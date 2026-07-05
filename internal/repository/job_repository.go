package repository

import (
	"context"
	"encoding/json"
	"time"

	"catchup-feed/internal/domain/entity"
)

// JobRepository is the DB-backed queue between processes (jobs table, §4 /
// C-4: プロセス間連携は PostgreSQL のジョブテーブル経由のみ). Deliberately
// minimal — enqueue, claim, mark — not a worker framework.
type JobRepository interface {
	// Enqueue inserts a pending job. A nil payload is stored as '{}'.
	// runAfter schedules the earliest execution time (time.Time{} = now).
	Enqueue(ctx context.Context, kind string, payload json.RawMessage, runAfter time.Time) (int64, error)
	// ClaimNext atomically claims the oldest runnable pending job
	// (run_after <= now), marks it running, and increments attempts.
	// Uses SELECT ... FOR UPDATE SKIP LOCKED so concurrent consumers never
	// double-claim. kinds optionally restricts the job kinds considered.
	// Returns nil when no job is runnable.
	ClaimNext(ctx context.Context, kinds ...string) (*entity.Job, error)
	// MarkDone finishes a claimed job successfully.
	MarkDone(ctx context.Context, id int64) error
	// MarkFailed records the error. With retryAt set the job goes back to
	// pending with run_after = retryAt; with retryAt nil it is failed
	// terminally. Retry-count policy stays in the caller: it reads
	// Job.Attempts (incremented by ClaimNext) and decides.
	MarkFailed(ctx context.Context, id int64, lastError string, retryAt *time.Time) error
	// RequeueRunning flips every running job back to pending and returns
	// how many rows it touched. The worker is the queue's only consumer
	// (C-4: single worker on the Pi), so at consumer startup any 'running'
	// row is by definition an orphan of a crashed previous process — this
	// is the stale-job sweep. Attempts stay as incremented by the crashed
	// claim, so a repeatedly crashing job still hits the retry ceiling.
	RequeueRunning(ctx context.Context) (int64, error)
}
