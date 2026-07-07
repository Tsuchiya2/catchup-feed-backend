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

	// ListDue selects the quiz candidates for a broadcast day (§6.3):
	// active items (retired_at IS NULL) with due_on <= day, oldest first
	// (due_on ASC, id ASC), up to limit (出題枠 S). It reads only — see the
	// interface comment.
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
	// affected item takes ONE ResultAuto transition (learning.Transition)
	// dated resolveDay. Multiple stale logs of the same item collapse into
	// a single advance — they are the same unanswered question re-asked
	// while ungraded, not evidence of repeated recall.
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
