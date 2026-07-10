package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"catchup-feed/internal/learning"
	"catchup-feed/internal/repository"
)

// bookActivationLockKey is the pg_advisory_xact_lock key that serializes
// ActivateBook transactions. The "active は最大1冊" invariant is enforced in
// the application layer by design (§7.3, no DB constraint), and row locks
// alone cannot enforce it: under READ COMMITTED a row that BECAME active
// after a statement took its snapshot is invisible to that statement's
// re-check (EvalPlanQual re-evaluates only rows the snapshot already saw),
// so two concurrent activates could each demote the old active book and
// then both promote their own. The advisory lock makes the whole
// demote+promote critical section serial; the waiter's statements then run
// on fresh snapshots and see the winner's committed 'active' row.
//
// The value is an arbitrary application-chosen 64-bit key ("bookrevi" in
// ASCII), shared with nothing else in this codebase.
const bookActivationLockKey int64 = 0x626F6F6B72657669

// LearningAdminRepo implements the §8.1 admin query layer
// (repository.LearningAdminRepository) on PostgreSQL. Day parameters are
// bound as YYYY-MM-DD text with a ::date cast (learning.FormatDay), same
// as LearningRepo (§12-10).
type LearningAdminRepo struct{ db *sql.DB }

func NewLearningAdminRepo(db *sql.DB) repository.LearningAdminRepository {
	return &LearningAdminRepo{db: db}
}

// ListPendingReviews returns the grading queue, oldest asking first
// (§8.1). Both not-yet-set columns are in the WHERE so the set is exactly
// the logs GradeReview's claim can still win.
//
// The join to learning_items deliberately does NOT filter on retired_at: an
// ungraded log of a retired item (graduated or manually archived while its
// last asking sat unanswered) is intentionally still returned. This is by
// design — the queue only drains: grading such a log closes it (GradeReview
// keeps the item terminal, §6.1), and the 48h auto-resolve closes it
// otherwise (result='auto'). Either way the pending row disappears; hiding it
// here would instead leave it dangling forever (nothing else closes it).
func (r *LearningAdminRepo) ListPendingReviews(ctx context.Context) ([]repository.PendingReview, error) {
	const query = `
SELECT rl.id, rl.item_id, rl.asked_on, li.concept, li.question, li.answer
FROM review_logs rl
JOIN learning_items li ON li.id = rl.item_id
WHERE rl.result IS NULL AND rl.graded_at IS NULL
ORDER BY rl.asked_on ASC, rl.id ASC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ListPendingReviews: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var pending []repository.PendingReview
	for rows.Next() {
		var p repository.PendingReview
		if err := rows.Scan(&p.LogID, &p.ItemID, &p.AskedOn, &p.Concept, &p.Question, &p.Answer); err != nil {
			return nil, fmt.Errorf("ListPendingReviews: %w", err)
		}
		pending = append(pending, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListPendingReviews: %w", err)
	}
	return pending, nil
}

// GradeReview is the manual-grading transaction (§8.1 一発確定). See
// repository.LearningAdminRepository for the full contract; the §12-9
// competition with the radio batch's AutoResolve is decided entirely by
// the atomic claim UPDATE — whichever side sets the log first wins, the
// other matches zero rows. SELECT-then-UPDATE is exactly the race the
// design forbids.
func (r *LearningAdminRepo) GradeReview(ctx context.Context, logID int64, result string, gradedOn time.Time, ladder []int) (repository.GradeOutcome, error) {
	// Defense in depth: the usecase validates too, but 'auto' (or worse)
	// slipping into a manual grade would corrupt the tracker's
	// self-graded/auto-drained distinction (§6.1).
	switch result {
	case learning.ResultGood, learning.ResultFuzzy, learning.ResultForgot:
	default:
		return repository.GradeOutcome{}, fmt.Errorf("GradeReview: invalid manual result %q", result)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return repository.GradeOutcome{}, fmt.Errorf("GradeReview: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Step 1: atomic claim (§12-9). 未採点 (both columns unset) のみ勝てる。
	var itemID int64
	err = tx.QueryRowContext(ctx, `
UPDATE review_logs SET result = $2, graded_at = now()
WHERE id = $1 AND result IS NULL AND graded_at IS NULL
RETURNING item_id`, logID, result).Scan(&itemID)
	if errors.Is(err, sql.ErrNoRows) {
		// Absent vs already resolved: only now is a read needed, and it is
		// race-free — a log id never un-exists and a resolved log never
		// un-resolves (一発確定).
		var exists bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM review_logs WHERE id = $1)`, logID).Scan(&exists); err != nil {
			return repository.GradeOutcome{}, fmt.Errorf("GradeReview: log %d lookup: %w", logID, err)
		}
		if !exists {
			return repository.GradeOutcome{}, repository.ErrReviewLogNotFound
		}
		return repository.GradeOutcome{}, repository.ErrReviewLogGraded
	}
	if err != nil {
		return repository.GradeOutcome{}, fmt.Errorf("GradeReview: claim log %d: %w", logID, err)
	}

	// Step 2: lock the item and apply the shared transition (§6.1).
	var stage int
	var dueOn time.Time
	var retiredAt *time.Time
	err = tx.QueryRowContext(ctx, `
SELECT stage, due_on, retired_at FROM learning_items WHERE id = $1 FOR UPDATE`,
		itemID).Scan(&stage, &dueOn, &retiredAt)
	if err != nil {
		return repository.GradeOutcome{}, fmt.Errorf("GradeReview: lock item %d: %w", itemID, err)
	}

	if retiredAt != nil {
		// Terminal (卒業 or 手動アーカイブ) — same stance as AutoResolve:
		// the log closes, the state stays. 起こり得るのは retire 後に
		// 残った未採点ログの採点のみ。
		if err := tx.Commit(); err != nil {
			return repository.GradeOutcome{}, fmt.Errorf("GradeReview: commit: %w", err)
		}
		return repository.GradeOutcome{ItemID: itemID, Stage: stage, DueOn: dueOn, Retired: true}, nil
	}

	next, err := learning.Transition(stage, result, gradedOn, ladder)
	if err != nil {
		return repository.GradeOutcome{}, fmt.Errorf("GradeReview: transition item %d: %w", itemID, err)
	}
	_, err = tx.ExecContext(ctx, `
UPDATE learning_items
SET stage = $2, due_on = $3::date,
    retired_at = CASE WHEN $4 THEN now() ELSE retired_at END
WHERE id = $1`,
		itemID, next.Stage, learning.FormatDay(next.DueOn), next.Retired)
	if err != nil {
		return repository.GradeOutcome{}, fmt.Errorf("GradeReview: advance item %d: %w", itemID, err)
	}
	if err := tx.Commit(); err != nil {
		return repository.GradeOutcome{}, fmt.Errorf("GradeReview: commit: %w", err)
	}
	return repository.GradeOutcome{ItemID: itemID, Stage: next.Stage, DueOn: next.DueOn, Retired: next.Retired}, nil
}

// listItemsQuery builds the tracker query (§8.1). The two variants differ
// only in fixed literals — nothing user-controlled is interpolated.
func listItemsQuery(retired bool) string {
	where, order := "IS NULL", "li.due_on ASC, li.id ASC"
	if retired {
		where, order = "IS NOT NULL", "li.retired_at DESC, li.id DESC"
	}
	return fmt.Sprintf(`
SELECT li.id, li.kind, li.article_id, li.book_id, li.concept, li.question, li.answer,
       li.provider, li.stage, li.due_on, li.retired_at, li.created_at,
       h.times_asked, h.last_result, h.last_asked_on
FROM learning_items li
LEFT JOIN LATERAL (
    SELECT count(*)::int AS times_asked,
           max(rl.asked_on) AS last_asked_on,
           (SELECT result FROM review_logs
            WHERE item_id = li.id
            ORDER BY asked_on DESC, id DESC LIMIT 1) AS last_result
    FROM review_logs rl WHERE rl.item_id = li.id
) h ON true
WHERE li.retired_at %s
ORDER BY %s`, where, order)
}

func (r *LearningAdminRepo) ListItems(ctx context.Context, retired bool) ([]repository.LearningItemSummary, error) {
	rows, err := r.db.QueryContext(ctx, listItemsQuery(retired))
	if err != nil {
		return nil, fmt.Errorf("ListItems: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []repository.LearningItemSummary
	for rows.Next() {
		var s repository.LearningItemSummary
		if err := rows.Scan(
			&s.ID, &s.Kind, &s.ArticleID, &s.BookID, &s.Concept, &s.Question, &s.Answer,
			&s.Provider, &s.Stage, &s.DueOn, &s.RetiredAt, &s.CreatedAt,
			&s.TimesAsked, &s.LastResult, &s.LastAskedOn,
		); err != nil {
			return nil, fmt.Errorf("ListItems: %w", err)
		}
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListItems: %w", err)
	}
	return items, nil
}

// RetireItem archives the item (retired_at セットのみ, §8.1). The claim
// UPDATE only wins on active items; a zero-row result falls back to
// reading the existing retired_at (冪等 200) or reporting absence.
func (r *LearningAdminRepo) RetireItem(ctx context.Context, itemID int64) (time.Time, error) {
	var retiredAt time.Time
	err := r.db.QueryRowContext(ctx, `
UPDATE learning_items SET retired_at = now()
WHERE id = $1 AND retired_at IS NULL
RETURNING retired_at`, itemID).Scan(&retiredAt)
	if err == nil {
		return retiredAt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, fmt.Errorf("RetireItem: %w", err)
	}
	var existing *time.Time
	err = r.db.QueryRowContext(ctx,
		`SELECT retired_at FROM learning_items WHERE id = $1`, itemID).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, repository.ErrLearningItemNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("RetireItem: %w", err)
	}
	if existing == nil {
		// Claim lost but the item is active again? Impossible — retired_at
		// is never cleared. Report it rather than loop.
		return time.Time{}, fmt.Errorf("RetireItem: item %d: claim matched no row yet retired_at is NULL", itemID)
	}
	return *existing, nil
}

// bookColumns is the shared SELECT of a ReviewBook row: §7.3 state plus
// the chunk total (進捗率の分母).
const bookColumns = `
SELECT b.id, b.title, b.review_status, b.review_cursor,
       (SELECT count(*)::int FROM book_chunks c WHERE c.book_id = b.id) AS total_chunks
FROM books b`

func scanBook(row *sql.Row) (repository.ReviewBook, error) {
	var b repository.ReviewBook
	err := row.Scan(&b.ID, &b.Title, &b.ReviewStatus, &b.ReviewCursor, &b.TotalChunks)
	return b, err
}

func (r *LearningAdminRepo) ListBooks(ctx context.Context) ([]repository.ReviewBook, error) {
	rows, err := r.db.QueryContext(ctx, bookColumns+` ORDER BY b.id ASC`)
	if err != nil {
		return nil, fmt.Errorf("ListBooks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var books []repository.ReviewBook
	for rows.Next() {
		var b repository.ReviewBook
		if err := rows.Scan(&b.ID, &b.Title, &b.ReviewStatus, &b.ReviewCursor, &b.TotalChunks); err != nil {
			return nil, fmt.Errorf("ListBooks: %w", err)
		}
		books = append(books, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListBooks: %w", err)
	}
	return books, nil
}

// ActivateBook is the D-20 swap as one transaction. See the interface
// contract and bookActivationLockKey for why the advisory lock (and not
// row locks) carries the "active は最大1冊" invariant.
func (r *LearningAdminRepo) ActivateBook(ctx context.Context, bookID int64) (repository.ReviewBook, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return repository.ReviewBook{}, fmt.Errorf("ActivateBook: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, bookActivationLockKey); err != nil {
		return repository.ReviewBook{}, fmt.Errorf("ActivateBook: lock: %w", err)
	}

	// Promote to active. finished→active means 再読(親裁定 2026-07-07):
	// reset the cursor to 0 so the book starts over — otherwise the
	// end-of-book cursor would make radio flip it straight back to
	// 'finished'. idle→active is 一時停止からの再開: keep the cursor. The
	// CASE keys the reset off the CURRENT status inside the same statement,
	// so no extra read is needed.
	res, err := tx.ExecContext(ctx, `
UPDATE books
SET review_status = 'active',
    review_cursor = CASE WHEN review_status = 'finished' THEN 0 ELSE review_cursor END
WHERE id = $1`, bookID)
	if err != nil {
		return repository.ReviewBook{}, fmt.Errorf("ActivateBook: promote book %d: %w", bookID, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return repository.ReviewBook{}, fmt.Errorf("ActivateBook: promote book %d: %w", bookID, err)
	} else if n == 0 {
		return repository.ReviewBook{}, repository.ErrBookNotFound
	}

	// Demote the previous active (if any). Runs after the advisory lock,
	// so a concurrently committed activate is visible on this statement's
	// fresh snapshot.
	if _, err := tx.ExecContext(ctx,
		`UPDATE books SET review_status = 'idle' WHERE review_status = 'active' AND id <> $1`, bookID); err != nil {
		return repository.ReviewBook{}, fmt.Errorf("ActivateBook: demote previous active: %w", err)
	}

	book, err := scanBook(tx.QueryRowContext(ctx, bookColumns+` WHERE b.id = $1`, bookID))
	if err != nil {
		return repository.ReviewBook{}, fmt.Errorf("ActivateBook: reread book %d: %w", bookID, err)
	}
	if err := tx.Commit(); err != nil {
		return repository.ReviewBook{}, fmt.Errorf("ActivateBook: commit: %w", err)
	}
	return book, nil
}

// DeactivateBook pauses an active book (D-20: status→idle、カーソル保持).
// Only 'active' transitions; 'idle' is already the target (冪等) and
// 'finished' keeps its 読了 marker (interface contract). No advisory lock:
// this only ever reduces the number of active books, so it cannot break
// the max-1 invariant against a concurrent activate.
func (r *LearningAdminRepo) DeactivateBook(ctx context.Context, bookID int64) (repository.ReviewBook, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return repository.ReviewBook{}, fmt.Errorf("DeactivateBook: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`UPDATE books SET review_status = 'idle' WHERE id = $1 AND review_status = 'active'`, bookID); err != nil {
		return repository.ReviewBook{}, fmt.Errorf("DeactivateBook: book %d: %w", bookID, err)
	}
	book, err := scanBook(tx.QueryRowContext(ctx, bookColumns+` WHERE b.id = $1`, bookID))
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ReviewBook{}, repository.ErrBookNotFound
	}
	if err != nil {
		return repository.ReviewBook{}, fmt.Errorf("DeactivateBook: reread book %d: %w", bookID, err)
	}
	if err := tx.Commit(); err != nil {
		return repository.ReviewBook{}, fmt.Errorf("DeactivateBook: commit: %w", err)
	}
	return book, nil
}
