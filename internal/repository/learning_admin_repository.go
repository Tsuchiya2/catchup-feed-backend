package repository

import (
	"context"
	"errors"
	"time"

	"catchup-feed/internal/learning"
)

// Sentinel errors of the learning admin query layer (§8.1). The usecase
// maps them onto its user-facing errors; they never reach a client
// directly.
var (
	// ErrReviewLogNotFound: the review log id does not exist (HTTP 404 素材).
	ErrReviewLogNotFound = errors.New("review log not found")
	// ErrReviewLogGraded: the log exists but is already resolved — manual
	// grade OR the 48h auto-resolve (result='auto'), the two are one case
	// by design (§8.1 一発確定, HTTP 409 素材).
	ErrReviewLogGraded = errors.New("review log already graded")
	// ErrLearningItemNotFound: the learning item id does not exist.
	ErrLearningItemNotFound = errors.New("learning item not found")
	// ErrBookNotFound: the book id does not exist.
	ErrBookNotFound = errors.New("book not found")
)

// PendingReview is one row of the grading screen (§8.1 GET
// /learning/reviews/pending): an ungraded asking joined with its item's
// quiz content. AskedOn is a JST broadcast day (§12-10).
type PendingReview struct {
	LogID    int64
	ItemID   int64
	AskedOn  time.Time
	Concept  string
	Question string
	Answer   string
}

// GradeOutcome is the item state after a grade was applied (§6.1) — the
// material for the grading screen's optimistic update. When the item was
// already retired (terminal state, the log still closes) it carries the
// item's unchanged state with Retired=true.
type GradeOutcome struct {
	ItemID  int64
	Stage   int
	DueOn   time.Time
	Retired bool
}

// LearningItemSummary is one tracker row (§8.1 GET /learning/items): the
// item plus a minimal asking-history summary (出題回数・直近結果程度 —
// 過剰な集計はしない). LastResult is NULL when the item was never asked or
// its latest asking is still ungraded.
type LearningItemSummary struct {
	learning.Item
	TimesAsked  int
	LastResult  *string
	LastAskedOn *time.Time
}

// ReviewBook is one row of the book management screen (§8.1 GET
// /learning/books, D-20): identity, §7.3 progress state and the total
// chunk count (進捗率 = ReviewCursor / TotalChunks の素材).
type ReviewBook struct {
	ID           int64
	Title        string
	ReviewStatus string
	ReviewCursor int
	TotalChunks  int
}

// LearningAdminRepository is the server-side admin query layer of the
// Phase 3 learning loop (§8.1, JWT の内側). It complements
// LearningRepository (the radio-batch side): grading here and auto-resolve
// there are the only two writers of learning_items state, and both apply
// learning.Transition — never a private copy of the ladder rules.
type LearningAdminRepository interface {
	// ListPendingReviews returns every ungraded asking (result IS NULL AND
	// graded_at IS NULL), oldest first (asked_on ASC, id ASC), with the
	// item's concept/question/answer for the grading card. An empty result
	// is the happy path (§2 Out: 罪悪感 UI 禁止 — no counts, no overdue
	// aggregates anywhere else).
	ListPendingReviews(ctx context.Context) ([]PendingReview, error)

	// GradeReview applies one manual grade (§8.1 一発確定) in a single
	// transaction:
	//
	//  1. Claim the log with an atomic UPDATE whose WHERE carries the
	//     not-yet-set checks (result IS NULL AND graded_at IS NULL, §12-9
	//     — jobs の SKIP LOCKED と同じ流儀). Zero rows means the log is
	//     either absent (ErrReviewLogNotFound) or already resolved by a
	//     manual grade, the 48h auto-resolve, or a concurrent grade — all
	//     one ErrReviewLogGraded (409): a grade is never overwritten
	//     (forgot の逆適用は情報喪失で不可能).
	//  2. Lock the item (FOR UPDATE), apply learning.Transition with
	//     gradedOn as the due 起点 (§6.1: 採点日), and write
	//     stage/due_on/retired_at. A retired item is terminal (手動
	//     アーカイブ・卒業済み): its log still closes, its state stays
	//     untouched — the same stance as AutoResolve.
	//
	// result must be one of good/fuzzy/forgot; ResultAuto is the radio
	// batch's word and is rejected here. gradedOn is a learning.BroadcastDay
	// value, ladder the D-18 ladder (Config.Ladder).
	GradeReview(ctx context.Context, logID int64, result string, gradedOn time.Time, ladder []int) (GradeOutcome, error)

	// ListItems returns the tracker rows: retired=false → active items
	// (retired_at IS NULL) ordered by due_on ASC, id ASC; retired=true →
	// archived items ordered by retired_at DESC, id DESC (newest
	// graduation first).
	ListItems(ctx context.Context, retired bool) ([]LearningItemSummary, error)

	// RetireItem archives an item manually (§8.1: 「もう追わなくていい」,
	// retired_at セットのみ). Idempotent: re-retiring returns the original
	// retired_at with no update. ErrLearningItemNotFound when the id does
	// not exist.
	RetireItem(ctx context.Context, itemID int64) (time.Time, error)

	// ListBooks returns all ingested books with their §7.3 review state
	// and total chunk count, ordered by id ASC.
	ListBooks(ctx context.Context) ([]ReviewBook, error)

	// ActivateBook sets the book to review_status='active' and demotes any
	// other active book to 'idle' in the same transaction — the D-20 swap
	// as one operation. The "active は最大1冊" invariant lives in the
	// application layer by design (§7.3, no DB CHECK), so concurrent
	// activations are serialized with a transaction-scoped advisory lock:
	// plain row locking cannot close the race where two transactions each
	// demote the OLD active row and then both promote their own (a row
	// that turns active after a READ COMMITTED statement took its snapshot
	// is invisible to that statement's re-check). Activating a 'finished'
	// book is allowed (再読); the cursor is left where it was — resetting
	// it is not this endpoint's business. Idempotent for the already
	// active book. ErrBookNotFound when the id does not exist.
	ActivateBook(ctx context.Context, bookID int64) (ReviewBook, error)

	// DeactivateBook pauses an active book: review_status active→idle,
	// cursor kept (D-20 一時停止). Idempotent — and deliberately a no-op
	// for 'idle' and 'finished' books ('finished' is already inactive;
	// silently downgrading it would erase the 読了 marker. Re-reading goes
	// through ActivateBook). ErrBookNotFound when the id does not exist.
	DeactivateBook(ctx context.Context, bookID int64) (ReviewBook, error)
}
