package repository

import (
	"context"
	"time"

	"catchup-feed/internal/learning"
)

// LearningRepository is the query layer of the Phase 3 learning loop
// (learning_items / review_logs, 設計書 §4/§5.2/§6). Days (dueOn, askedOn,
// day, cutoffDay, resolveDay) are JST broadcast days produced by
// learning.BroadcastDay — never raw time.Now() values (§12-10).
//
// The idempotency contract (§4 設計メモ / §12-2) is load-bearing: asking
// never mutates learning_items. State transitions happen only on grading
// (server API, 後続タスク) or auto-resolve (AutoResolve). A same-day rev
// re-run of the radio batch therefore re-selects the same items (ListDue is
// stable) and RecordAsked collapses on UNIQUE (item_id, asked_on).
type LearningRepository interface {
	// InsertItem creates a freshly generated item at stage 0 with the
	// given due day (normally learning.FirstDueDay — §5.1: 初回想起は翌日).
	// It validates the item first (learning.NewItem.Validate), which
	// enforces provider='ollama' for kind='book' in the application layer
	// (§12-4).
	InsertItem(ctx context.Context, item learning.NewItem, dueOn time.Time) (int64, error)

	// HasArticleItemCreatedOn reports whether any kind='article' item was
	// created on the given JST broadcast day. learning_items has no UNIQUE
	// key for generation, so this existence check is the radio batch's
	// same-day rev re-run dedupe (§12-2): rev2 sees rev1's items and skips
	// generation entirely, keeping the daily quota M per DAY, not per rev.
	// The check compares created_at (timestamptz) against the JST day
	// boundaries; due_on cannot serve as the discriminator because a
	// same-day 'forgot' grade also leaves an old item at stage 0 with
	// due_on = 翌日.
	HasArticleItemCreatedOn(ctx context.Context, day time.Time) (bool, error)

	// ListDue selects the quiz candidates for a broadcast day (§6.3):
	// active items (retired_at IS NULL) with due_on <= day, oldest first
	// (due_on ASC, id ASC), up to limit (出題枠 S, must be >= 1). It reads
	// only — see the interface comment.
	//
	// Items with an ungraded log from a PREVIOUS day (result IS NULL,
	// asked_on < day) are excluded (§6.3, 2026-07-07 親裁定): they are
	// awaiting their verdict — manual grade or 48h auto-resolve — and
	// re-asking them would contradict D-17 and double-spend the S slots
	// against the §6.2 saturation arithmetic. Same-day logs do NOT
	// exclude, so a same-day rev re-run still selects the identical items.
	// Once auto-resolve closes the pending log, the item reappears at its
	// post-transition due date.
	ListDue(ctx context.Context, day time.Time, limit int) ([]learning.Item, error)

	// RecordAsked inserts one review log (result NULL = 未採点) per item
	// for the broadcast day. ON CONFLICT (item_id, asked_on) DO NOTHING
	// makes the same-day rev re-run a no-op: the log row — including the
	// episode_id of the day's FIRST rev — stays as it is (§9: 出題記録は
	// 増えない).
	RecordAsked(ctx context.Context, itemIDs []int64, episodeID int64, askedOn time.Time) error

	// AutoResolve applies the D-17 auto-advance: every ungraded log
	// (result IS NULL AND graded_at IS NULL) asked on or before cutoffDay
	// is closed with result='auto' (graded_at stays NULL, §4), and each
	// eligible item takes ONE ResultAuto transition (learning.Transition)
	// dated resolveDay.
	//
	// "One" holds across runs, not just within a run (§6.3): the item-side
	// transition is guarded by due_on <= resolveDay, so an item whose
	// due_on was already pushed into the future by another log's
	// resolution — an earlier run's auto-advance or a manual grade — only
	// has its remaining stale logs closed, never a second advance.
	// Multiple stale logs of one item are the same unanswered question
	// re-asked while ungraded, not evidence of repeated recall; without
	// the guard a never-graded item would double-step (ladder [1,7,30]
	// degrading to [1,1,30]).
	//
	// The log claim is a single atomic UPDATE with the not-yet-set checks
	// in its WHERE (§12-9, jobs の SKIP LOCKED と同じ流儀): a concurrent
	// manual grade and this auto-resolve can each win a given log exactly
	// once, never both. Items already retired keep their state; their
	// stale logs are still closed.
	//
	// Callers derive the days from one clock reading:
	//   cutoffDay  = learning.BroadcastDay(now.Add(-cfg.AutoResolveAfter))
	//   resolveDay = learning.BroadcastDay(now)
	// Returns the number of logs resolved.
	AutoResolve(ctx context.Context, cutoffDay, resolveDay time.Time, ladder []int) (int, error)

	// CountOverdueActive returns the number of active items strictly past
	// their due day (due_on < day) — the §5.2 backpressure input. Items
	// due exactly on `day` are the day's normal workload, not backlog.
	// The caller compares against Config.BackpressureThreshold and stops
	// NEW item generation only; asking always continues (§9).
	CountOverdueActive(ctx context.Context, day time.Time) (int, error)
}
