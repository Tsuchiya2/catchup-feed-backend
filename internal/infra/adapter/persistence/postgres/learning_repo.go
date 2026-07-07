package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"catchup-feed/internal/learning"
	"catchup-feed/internal/repository"
)

// LearningRepo implements the Phase 3 learning-loop query layer
// (learning_items / review_logs, 設計書 §4/§5.2/§6) on PostgreSQL. All day
// parameters are bound as YYYY-MM-DD text with a ::date cast
// (learning.FormatDay) so no driver timezone conversion can move a
// broadcast day (§12-10).
type LearningRepo struct{ db *sql.DB }

func NewLearningRepo(db *sql.DB) repository.LearningRepository {
	return &LearningRepo{db: db}
}

// InsertItem creates a stage-0 item due on dueOn. Validation runs first:
// the kind⇔FK invariants mirror the DB CHECKs, and provider='ollama' for
// kind='book' exists ONLY here in the application layer (§12-4), so
// skipping validation would silently ship private book data provenance
// bugs to the DB.
func (r *LearningRepo) InsertItem(ctx context.Context, item learning.NewItem, dueOn time.Time) (int64, error) {
	if err := item.Validate(); err != nil {
		return 0, fmt.Errorf("InsertItem: %w", err)
	}
	const query = `
INSERT INTO learning_items (kind, article_id, book_id, concept, question, answer, provider, stage, due_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, 0, $8::date)
RETURNING id`
	var id int64
	err := r.db.QueryRowContext(ctx, query,
		item.Kind, item.ArticleID, item.BookID,
		item.Concept, item.Question, item.Answer, item.Provider,
		learning.FormatDay(dueOn),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("InsertItem: %w", err)
	}
	return id, nil
}

// HasArticleItemCreatedOn is the same-day rev re-run dedupe for item
// generation (§12-2) — see repository.LearningRepository. created_at is
// UTC (timestamptz on a UTC-運用 Postgres, §12-10), so the JST broadcast
// day is converted to its two UTC boundary instants in SQL:
// ($1::date)::timestamp is JST-wall-clock midnight, and AT TIME ZONE
// 'Asia/Tokyo' reinterprets it as a timestamptz. A naive UTC date
// comparison would misfile items created between 00:00 and 09:00 JST.
func (r *LearningRepo) HasArticleItemCreatedOn(ctx context.Context, day time.Time) (bool, error) {
	const query = `
SELECT EXISTS (
    SELECT 1 FROM learning_items
    WHERE kind = $1
      AND created_at >= ($2::date)::timestamp AT TIME ZONE 'Asia/Tokyo'
      AND created_at <  ($2::date + 1)::timestamp AT TIME ZONE 'Asia/Tokyo'
)`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, learning.KindArticle, learning.FormatDay(day)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("HasArticleItemCreatedOn: %w", err)
	}
	return exists, nil
}

// ListDue implements the §6.3 selection: active items due on or before the
// broadcast day, oldest first, capped at the quiz slots. Read-only by
// contract — asking must not move due_on (§12-2).
//
// Items with an ungraded log from a PREVIOUS day are excluded (§6.3,
// 2026-07-07 親裁定): re-reading a question that is still awaiting its
// verdict contradicts D-17 (未採点=good 前進) and double-spends the S
// slots. The exclusion is limited to asked_on < day so that a same-day
// rev re-run — whose own logs carry asked_on = day — still selects the
// identical items (§4 設計メモの冪等). The pending log is closed by the
// 48h auto-resolve, after which the item reappears at its post-transition
// due date.
//
// limit must be >= 1 (出題枠 S; PostgreSQL rejects a negative LIMIT and
// LIMIT 0 selects nothing). Callers pass Config.Slots, which LoadConfig
// guarantees positive.
func (r *LearningRepo) ListDue(ctx context.Context, day time.Time, limit int) ([]learning.Item, error) {
	const query = `
SELECT id, kind, article_id, book_id, concept, question, answer, provider,
       stage, due_on, retired_at, created_at
FROM learning_items
WHERE retired_at IS NULL AND due_on <= $1::date
  AND NOT EXISTS (
      SELECT 1 FROM review_logs rl
      WHERE rl.item_id = learning_items.id
        AND rl.result IS NULL
        AND rl.asked_on < $1::date
  )
ORDER BY due_on ASC, id ASC
LIMIT $2`
	rows, err := r.db.QueryContext(ctx, query, learning.FormatDay(day), limit)
	if err != nil {
		return nil, fmt.Errorf("ListDue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []learning.Item
	for rows.Next() {
		var item learning.Item
		if err := rows.Scan(
			&item.ID, &item.Kind, &item.ArticleID, &item.BookID,
			&item.Concept, &item.Question, &item.Answer, &item.Provider,
			&item.Stage, &item.DueOn, &item.RetiredAt, &item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("ListDue: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListDue: %w", err)
	}
	return items, nil
}

// RecordAsked writes the day's asking records. ON CONFLICT DO NOTHING on
// UNIQUE (item_id, asked_on) is the whole idempotency story for same-day
// rev re-runs (§4 設計メモ): the second rev inserts nothing, and the
// episode_id of the first rev is kept as the day's trace. A handful of
// rows per morning — a per-item loop in one transaction is right-sized.
func (r *LearningRepo) RecordAsked(ctx context.Context, itemIDs []int64, episodeID int64, askedOn time.Time) error {
	if len(itemIDs) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("RecordAsked: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const query = `
INSERT INTO review_logs (item_id, episode_id, asked_on)
VALUES ($1, $2, $3::date)
ON CONFLICT (item_id, asked_on) DO NOTHING`
	day := learning.FormatDay(askedOn)
	for _, itemID := range itemIDs {
		if _, err := tx.ExecContext(ctx, query, itemID, episodeID, day); err != nil {
			return fmt.Errorf("RecordAsked: item %d: %w", itemID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("RecordAsked: commit: %w", err)
	}
	return nil
}

// AutoResolve closes every ungraded log asked on or before cutoffDay with
// result='auto' and advances each affected item once (D-17). See
// repository.LearningRepository for the full contract.
//
// The claim is a single UPDATE whose WHERE carries the not-yet-set checks
// (result IS NULL AND graded_at IS NULL) — the same atomic-claim style as
// the jobs SKIP LOCKED consumer (§12-9). A manual grade running
// concurrently either wins the row first (this UPDATE skips it) or blocks
// on the row lock and then matches nothing. SELECT-then-UPDATE is exactly
// the race the design forbids.
func (r *LearningRepo) AutoResolve(ctx context.Context, cutoffDay, resolveDay time.Time, ladder []int) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("AutoResolve: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const claim = `
UPDATE review_logs SET result = $1
WHERE result IS NULL AND graded_at IS NULL AND asked_on <= $2::date
RETURNING item_id`
	rows, err := tx.QueryContext(ctx, claim, learning.ResultAuto, learning.FormatDay(cutoffDay))
	if err != nil {
		return 0, fmt.Errorf("AutoResolve: claim logs: %w", err)
	}
	resolved := 0
	var itemIDs []int64
	for rows.Next() {
		var itemID int64
		if err := rows.Scan(&itemID); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("AutoResolve: scan: %w", err)
		}
		resolved++
		itemIDs = append(itemIDs, itemID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("AutoResolve: claim logs: %w", err)
	}
	_ = rows.Close()

	// Deterministic lock order across concurrent graders (deadlock 回避).
	// Compact is a query-saving optimization only: the once-per-item
	// guarantee lives in advanceItem's due guard, which also covers logs
	// of the same item claimed by DIFFERENT runs (§6.3) — an in-memory
	// dedupe cannot see those.
	slices.Sort(itemIDs)
	itemIDs = slices.Compact(itemIDs)

	for _, itemID := range itemIDs {
		if err := r.advanceItem(ctx, tx, itemID, resolveDay, ladder); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("AutoResolve: commit: %w", err)
	}
	return resolved, nil
}

// advanceItem applies one ResultAuto transition to an active item inside
// the AutoResolve transaction. The claim SELECT carries the §6.3 guards,
// so an ineligible item is skipped (its log is still closed, its state
// stays untouched):
//
//   - retired_at IS NULL: graduated or manually archived items are
//     terminal.
//   - due_on <= resolveDay: the item must still be due. A transition
//     driven by ANOTHER log — an earlier run's auto-resolve, or a manual
//     grade — has already pushed due_on past the resolve day; advancing
//     again here would double-step the ladder (e.g. [1,7,30] degrading to
//     [1,1,30] for never-graded items). This guard is what makes the
//     advance once-per-item across runs, not just within one run.
func (r *LearningRepo) advanceItem(ctx context.Context, tx *sql.Tx, itemID int64, resolveDay time.Time, ladder []int) error {
	var stage int
	err := tx.QueryRowContext(ctx,
		`SELECT stage FROM learning_items
		 WHERE id = $1 AND retired_at IS NULL AND due_on <= $2::date
		 FOR UPDATE`,
		itemID, learning.FormatDay(resolveDay)).Scan(&stage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // retired or already advanced past resolveDay — log closed, state untouched
	}
	if err != nil {
		return fmt.Errorf("AutoResolve: lock item %d: %w", itemID, err)
	}

	next, err := learning.Transition(stage, learning.ResultAuto, resolveDay, ladder)
	if err != nil {
		return fmt.Errorf("AutoResolve: transition item %d: %w", itemID, err)
	}
	// graded_at stays NULL on the logs (§4); retirement timestamps the
	// graduation itself.
	_, err = tx.ExecContext(ctx, `
UPDATE learning_items
SET stage = $2, due_on = $3::date,
    retired_at = CASE WHEN $4 THEN now() ELSE retired_at END
WHERE id = $1`,
		itemID, next.Stage, learning.FormatDay(next.DueOn), next.Retired)
	if err != nil {
		return fmt.Errorf("AutoResolve: advance item %d: %w", itemID, err)
	}
	return nil
}

// CountOverdueActive counts active items strictly past due (§5.2
// backpressure input). due_on = day is the day's normal workload and does
// not count as backlog.
func (r *LearningRepo) CountOverdueActive(ctx context.Context, day time.Time) (int, error) {
	const query = `
SELECT count(*) FROM learning_items
WHERE retired_at IS NULL AND due_on < $1::date`
	var n int
	if err := r.db.QueryRowContext(ctx, query, learning.FormatDay(day)).Scan(&n); err != nil {
		return 0, fmt.Errorf("CountOverdueActive: %w", err)
	}
	return n, nil
}

// WeeklyReviewMaterial gathers the §7.4 review material — see
// repository.LearningRepository for the full contract. Three read-only
// queries; the segment runs once a week, so the extra round trips are
// immaterial. All in-window filters compare a timestamptz column against the
// JST day boundary of fromDay: ($1::date)::timestamp AT TIME ZONE 'Asia/Tokyo'
// is JST-wall-clock midnight reinterpreted as a timestamptz, the same
// technique HasArticleItemCreatedOn uses so a naive UTC comparison cannot
// misfile rows created between 00:00 and 09:00 JST (§12-10).
func (r *LearningRepo) WeeklyReviewMaterial(ctx context.Context, fromDay time.Time, ladderLen int) (learning.WeeklyReview, error) {
	from := learning.FormatDay(fromDay)
	var m learning.WeeklyReview

	const conceptsQuery = `
SELECT concept FROM learning_items
WHERE created_at >= ($1::date)::timestamp AT TIME ZONE 'Asia/Tokyo'
ORDER BY created_at ASC, id ASC`
	rows, err := r.db.QueryContext(ctx, conceptsQuery, from)
	if err != nil {
		return learning.WeeklyReview{}, fmt.Errorf("WeeklyReviewMaterial: concepts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var concept string
		if err := rows.Scan(&concept); err != nil {
			return learning.WeeklyReview{}, fmt.Errorf("WeeklyReviewMaterial: scan concept: %w", err)
		}
		m.Concepts = append(m.Concepts, concept)
	}
	if err := rows.Err(); err != nil {
		return learning.WeeklyReview{}, fmt.Errorf("WeeklyReviewMaterial: concepts: %w", err)
	}

	// Ladder completion only (§7.4「学びの成果」): Transition sets stage to
	// len(ladder) exactly when it retires an item, and a manual RetireItem
	// never touches stage, so stage >= ladderLen selects graduations and
	// excludes manual archives.
	const gradQuery = `
SELECT count(*) FROM learning_items
WHERE retired_at >= ($1::date)::timestamp AT TIME ZONE 'Asia/Tokyo'
  AND stage >= $2`
	if err := r.db.QueryRowContext(ctx, gradQuery, from, ladderLen).Scan(&m.GraduatedCount); err != nil {
		return learning.WeeklyReview{}, fmt.Errorf("WeeklyReviewMaterial: graduated: %w", err)
	}

	// One item pulled back by a 'forgot' grade in-window (most recent). A
	// forgot always has graded_at set (manual grade), so the window filter is
	// on graded_at.
	const reintroQuery = `
SELECT li.concept
FROM review_logs rl
JOIN learning_items li ON li.id = rl.item_id
WHERE rl.result = $1
  AND rl.graded_at >= ($2::date)::timestamp AT TIME ZONE 'Asia/Tokyo'
ORDER BY rl.graded_at DESC, rl.id DESC
LIMIT 1`
	err = r.db.QueryRowContext(ctx, reintroQuery, learning.ResultForgot, from).Scan(&m.Reintroduced)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return learning.WeeklyReview{}, fmt.Errorf("WeeklyReviewMaterial: reintroduced: %w", err)
	}
	return m, nil
}
