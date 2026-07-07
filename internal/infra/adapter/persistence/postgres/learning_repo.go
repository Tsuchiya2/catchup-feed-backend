package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
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

// ListDue implements the §6.3 selection: active items due on or before the
// broadcast day, oldest first, capped at the quiz slots. Read-only by
// contract — asking must not move due_on (§12-2).
func (r *LearningRepo) ListDue(ctx context.Context, day time.Time, limit int) ([]learning.Item, error) {
	const query = `
SELECT id, kind, article_id, book_id, concept, question, answer, provider,
       stage, due_on, retired_at, created_at
FROM learning_items
WHERE retired_at IS NULL AND due_on <= $1::date
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
	seen := map[int64]bool{}
	var itemIDs []int64
	for rows.Next() {
		var itemID int64
		if err := rows.Scan(&itemID); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("AutoResolve: scan: %w", err)
		}
		resolved++
		// Dedupe: an item re-asked daily while ungraded owns several stale
		// logs, but they are one unanswered question — one auto advance.
		if !seen[itemID] {
			seen[itemID] = true
			itemIDs = append(itemIDs, itemID)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("AutoResolve: claim logs: %w", err)
	}
	_ = rows.Close()

	// Deterministic lock order across concurrent graders (deadlock 回避).
	sort.Slice(itemIDs, func(i, j int) bool { return itemIDs[i] < itemIDs[j] })

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
// the AutoResolve transaction. A retired item (graduated or manually
// archived while its log sat ungraded) is skipped: its log is still closed
// but its terminal state stays untouched.
func (r *LearningRepo) advanceItem(ctx context.Context, tx *sql.Tx, itemID int64, resolveDay time.Time, ladder []int) error {
	var stage int
	err := tx.QueryRowContext(ctx,
		`SELECT stage FROM learning_items WHERE id = $1 AND retired_at IS NULL FOR UPDATE`,
		itemID).Scan(&stage)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // already retired — log closed, state untouched
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
