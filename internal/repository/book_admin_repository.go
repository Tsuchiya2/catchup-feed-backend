package repository

import (
	"context"
	"time"
)

// BookRecord is one ingested book (books row) with its chunk count, as
// shown on the dashboard book list (D-25). FilePath is the identity key:
// the canonical absolute PDF path recorded at ingest (dashboard uploads
// record the Pi-server path under BOOKS_DIR; CLI ingests record the
// Mac-resolved path — the two never collide).
type BookRecord struct {
	ID         int64
	Title      string
	FilePath   string
	ImportedAt time.Time
	ChunkCount int
}

// IngestJobState is the latest kind='book_ingest' job for one file_path,
// used to derive the dashboard ingest status (D-25: 待機/処理中/完了/失敗
// は jobs の状態から導出、専用カラムは作らない).
type IngestJobState struct {
	Status string // jobs.status: pending | running | done | failed
	Title  string // payload title (display fallback before the books row exists)
}

// BookAdminRepository is the dashboard-side persistence of the book PDF
// management API (D-25): the books/book_chunks read model plus the few
// book_ingest job queries the upload/delete lifecycle needs. It is
// deliberately separate from JobRepository (which stays a generic queue —
// enqueue/claim/mark only) and from BookReviewRepository (radio-side,
// Phase 3 §7.3).
type BookAdminRepository interface {
	// ListBooks returns every books row with its chunk count, id order.
	ListBooks(ctx context.Context) ([]BookRecord, error)

	// LatestIngestStates returns, per payload file_path, the state of the
	// most recent (highest id) kind='book_ingest' job. Jobs without a
	// file_path payload key are ignored.
	LatestIngestStates(ctx context.Context) (map[string]IngestJobState, error)

	// UpdatePendingIngestTitle rewrites the payload title of pending
	// kind='book_ingest' jobs for the file_path and returns how many rows
	// it touched. Zero means no pending job exists — the caller enqueues a
	// new one (upload idempotency, D-25: 既存 pending ジョブがあれば重複
	// 投入しない; the title rewrite keeps a re-upload's new title from
	// going stale on the deduped job). Only the payload is updated —
	// status/attempts semantics stay owned by internal/jobs.
	UpdatePendingIngestTitle(ctx context.Context, filePath, title string) (int64, error)

	// CancelPendingIngest deletes pending (never running) book_ingest jobs
	// for the file_path and returns how many rows it removed. Deleting a
	// pending row is a legal queue operation — it does not touch the
	// status/attempts semantics owned by internal/jobs.
	CancelPendingIngest(ctx context.Context, filePath string) (int64, error)

	// DeleteBookByFilePath removes the books row(s) for the file_path and
	// everything referencing them — book_chunks, and the Phase 3 learning
	// rows (learning_items kind='book' + their review_logs), since a quiz
	// item for a deleted book is unanswerable. All in one transaction.
	// Returns whether any books row existed.
	DeleteBookByFilePath(ctx context.Context, filePath string) (bool, error)
}
