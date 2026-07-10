package repository

import (
	"context"
	"time"
)

// ActiveReviewBook is the single review_status='active' book the radio batch
// book-reviews today (§7.3, D-20: 同時に最大1冊). Cursor is the number of
// chunks already reviewed = the next 0-based book_chunks.position to read
// (書籍取り込みは position を 0 起点で連番付与する — catchup-feed-ai
// pulse_books/db.py の enumerate 準拠). TotalChunks is the review's end: the
// book is finished when Cursor reaches it.
type ActiveReviewBook struct {
	ID          int64
	Title       string
	Cursor      int
	TotalChunks int
}

// BookReviewChunk is one source excerpt fed to the local model (§7.3). It is
// private book text (C-12) and only ever reaches Ollama.
type BookReviewChunk struct {
	Position int
	Content  string
}

// BookReviewRepository is the §7.3 book_review persistence used by the radio
// batch (books.review_cursor/review_status + book_chunks). It is deliberately
// separate from LearningRepository: this side owns the book cursor, the
// learning side owns the quiz item (which rides on Learning.InsertItem with
// provider='ollama' pinned). Day parameters are JST broadcast days
// (learning.BroadcastDay), bound as text with a ::date cast (§12-10).
type BookReviewRepository interface {
	// ActiveBook returns the one review_status='active' book (§7.3, D-20's
	// max-1 invariant is enforced by the admin API's ActivateBook). ok=false
	// with no error means no active book — book_review is skipped for the day
	// (縮退と同じ扱い, §7.3). If two rows were ever active (should be
	// impossible), the lowest id is returned deterministically.
	ActiveBook(ctx context.Context) (ActiveReviewBook, bool, error)

	// NextChunks returns up to limit book_chunks of the book at position >=
	// cursor, in position order (§7.3: position 順に続きから一定量). An empty
	// result means the cursor is at or past the end (読了間近); the caller
	// marks the book finished.
	NextChunks(ctx context.Context, bookID int64, cursor, limit int) ([]BookReviewChunk, error)

	// HasBookReviewOn reports whether a book_review segment already sits in a
	// private episode published on the given JST broadcast day. This is the
	// same-day rev re-run guard (§12-2 の冪等流儀 applied to §7.3): a segment
	// is committed as part of the private episode, so rev2 sees rev1's
	// book_review and skips regeneration — no double cursor advance, no double
	// book-quiz insert. It keys off the committed segment (not the book quiz
	// item), so it holds even when the quiz degraded to nil (§5.3).
	HasBookReviewOn(ctx context.Context, day time.Time) (bool, error)

	// AdvanceCursor moves review_cursor to newCursor and, when finished is
	// true, sets review_status='finished' (§7.3: 末尾到達で finished、次巻は
	// activate 待ち). The UPDATE is guarded on the current cursor and the
	// active status (WHERE id=$ AND review_cursor=$from AND
	// review_status='active') so it is idempotent and race-safe: a same-day
	// rev whose cursor already advanced, or a book paused/swapped from the
	// dashboard mid-run, matches zero rows and is a no-op. Called only after
	// the private episode carrying the book_review is committed (§7.3: 生成
	// 失敗で先にカーソルだけ進む事故を防ぐ).
	AdvanceCursor(ctx context.Context, bookID int64, fromCursor, newCursor int, finished bool) error
}
