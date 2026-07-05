package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// JobRepo is the DB-backed queue between worker (Pi) and radio (Mac)
// (jobs table, §4 / C-4). Claiming uses a single UPDATE with
// SELECT ... FOR UPDATE SKIP LOCKED — deliberately not a worker framework.
type JobRepo struct{ db *sql.DB }

func NewJobRepo(db *sql.DB) repository.JobRepository {
	return &JobRepo{db: db}
}

// Enqueue inserts a pending job. A nil payload is stored as '{}' (the §4
// column default); a zero runAfter means "runnable now".
func (repo *JobRepo) Enqueue(ctx context.Context, kind string, payload json.RawMessage, runAfter time.Time) (int64, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if runAfter.IsZero() {
		runAfter = time.Now()
	}
	const query = `
INSERT INTO jobs (kind, payload, run_after)
VALUES ($1, $2, $3)
RETURNING id`
	var id int64
	err := repo.db.QueryRowContext(ctx, query, kind, []byte(payload), runAfter).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("Enqueue: %w", err)
	}
	return id, nil
}

// ClaimNext atomically claims the oldest runnable pending job: it marks the
// row running and increments attempts. FOR UPDATE SKIP LOCKED keeps
// concurrent consumers from double-claiming. Returns nil when nothing is
// runnable.
func (repo *JobRepo) ClaimNext(ctx context.Context, kinds ...string) (*entity.Job, error) {
	var (
		kindFilter string
		args       []any
	)
	if len(kinds) > 0 {
		placeholders := make([]string, len(kinds))
		for i, kind := range kinds {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args = append(args, kind)
		}
		kindFilter = fmt.Sprintf(" AND kind IN (%s)", strings.Join(placeholders, ", "))
	}

	// #nosec G201 -- kindFilter contains only generated placeholders ($1, $2, ...).
	query := fmt.Sprintf(`
UPDATE jobs SET
       status   = 'running',
       attempts = attempts + 1
WHERE id = (
    SELECT id FROM jobs
    WHERE status = 'pending' AND run_after <= now()%s
    ORDER BY run_after ASC, id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, kind, payload, status, attempts, last_error, run_after, created_at`, kindFilter)

	var (
		job     entity.Job
		payload []byte
	)
	err := repo.db.QueryRowContext(ctx, query, args...).Scan(
		&job.ID, &job.Kind, &payload, &job.Status, &job.Attempts,
		&job.LastError, &job.RunAfter, &job.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ClaimNext: %w", err)
	}
	job.Payload = json.RawMessage(payload)
	return &job, nil
}

// MarkDone finishes a claimed job successfully.
func (repo *JobRepo) MarkDone(ctx context.Context, id int64) error {
	const query = `UPDATE jobs SET status = 'done' WHERE id = $1`
	res, err := repo.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("MarkDone: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("MarkDone: no rows affected")
	}
	return nil
}

// RequeueRunning flips every running job back to pending (stale-job sweep,
// see repository.JobRepository). last_error records the sweep so the
// dashboard of a crash-looping job tells the story.
func (repo *JobRepo) RequeueRunning(ctx context.Context) (int64, error) {
	const query = `
UPDATE jobs SET
       status     = 'pending',
       last_error = 'requeued: claimed by a worker that did not finish (stale running sweep)'
WHERE status = 'running'`
	res, err := repo.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("RequeueRunning: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("RequeueRunning: %w", err)
	}
	return n, nil
}

// MarkFailed records the error. With retryAt set the job goes back to
// pending with run_after = retryAt (attempts stay incremented from the
// claim); with retryAt nil the job is failed terminally.
func (repo *JobRepo) MarkFailed(ctx context.Context, id int64, lastError string, retryAt *time.Time) error {
	var (
		query string
		args  []any
	)
	if retryAt != nil {
		query = `UPDATE jobs SET status = 'pending', last_error = $1, run_after = $2 WHERE id = $3`
		args = []any{lastError, *retryAt, id}
	} else {
		query = `UPDATE jobs SET status = 'failed', last_error = $1 WHERE id = $2`
		args = []any{lastError, id}
	}
	res, err := repo.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("MarkFailed: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("MarkFailed: no rows affected")
	}
	return nil
}
