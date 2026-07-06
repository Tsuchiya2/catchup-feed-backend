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
//
// Deferral (持ち越し) is a legal queue operation, distinct from failure:
// a consumer that has claimed a job but NOT yet started processing it may
// hand the job back with `status='pending', attempts = attempts - 1`,
// rolling back the increment its own claim made. A deferral does not
// consume the retry ceiling — it is scheduling, not an error (e.g. the Mac
// transcribe worker returns jobs that do not fit the remaining nightly
// transcription budget to the next night, D-14 / Phase 2 §5.2). The
// rollback is permitted only while the job is untouched (no side effects
// from processing); once processing has started, a failure must go through
// MarkFailed as usual and consume an attempt. This deferral is currently
// performed only by the Python transcribe worker; the Go consumer never
// defers.
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
	// RequeueRunning flips running jobs of the given kinds back to pending
	// and returns how many rows it touched (the stale-job sweep, run at
	// consumer startup). The jobs table has multiple consumers — the Pi
	// worker (cmd/worker) and the Mac transcribe worker (Python, Phase 2) —
	// so each consumer MUST sweep only the kinds it is registered to
	// handle and never touch another consumer's 'running' rows: a row of
	// my own kinds that is 'running' when I start can only be the orphan
	// of my crashed predecessor, while a 'running' row of a foreign kind
	// is very likely mid-execution on the other host. Sweeping it would
	// cause double execution and double attempts-counting. Calling with no
	// kinds sweeps nothing (never all). Attempts stay as incremented by
	// the crashed claim, so a repeatedly crashing job still hits the retry
	// ceiling.
	RequeueRunning(ctx context.Context, kinds ...string) (int64, error)
}
