package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	pg "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/repository"
)

var jobCols = []string{
	"id", "kind", "payload", "status", "attempts", "last_error", "run_after", "created_at",
}

func newJobRepo(t *testing.T) (repository.JobRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return pg.NewJobRepo(db), mock, func() { _ = db.Close() }
}

/* ─────────────────────────── Enqueue ─────────────────────────── */

func TestJobRepo_Enqueue(t *testing.T) {
	runAfter := time.Date(2026, 7, 4, 4, 30, 0, 0, time.UTC)

	tests := []struct {
		name        string
		kind        string
		payload     json.RawMessage
		runAfter    time.Time
		wantPayload []byte
	}{
		{
			name:        "with payload and schedule",
			kind:        entity.JobKindNotifyEpisode,
			payload:     json.RawMessage(`{"episode_id":12}`),
			runAfter:    runAfter,
			wantPayload: []byte(`{"episode_id":12}`),
		},
		{
			name:        "nil payload defaults to empty object",
			kind:        entity.JobKindRegenerateFeed,
			wantPayload: []byte(`{}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newJobRepo(t)
			defer closeFn()

			mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO jobs")).
				WithArgs(tt.kind, tt.wantPayload, sqlmock.AnyArg()).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))

			id, err := repo.Enqueue(context.Background(), tt.kind, tt.payload, tt.runAfter)
			require.NoError(t, err)
			assert.Equal(t, int64(7), id)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

/* ─────────────────────────── ClaimNext ─────────────────────────── */

func TestJobRepo_ClaimNext(t *testing.T) {
	now := time.Date(2026, 7, 4, 5, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		kinds     []string
		rows      *sqlmock.Rows
		queryErr  error
		wantQuery string
		wantArgs  []any
		wantNil   bool
		wantErr   bool
	}{
		{
			name: "claims the oldest runnable job and increments attempts",
			rows: sqlmock.NewRows(jobCols).AddRow(
				int64(3), entity.JobKindRegenerateFeed, []byte(`{}`),
				entity.JobStatusRunning, 1, nil, now, now,
			),
			wantQuery: "FOR UPDATE SKIP LOCKED",
		},
		{
			name:  "kind filter uses placeholders",
			kinds: []string{entity.JobKindNotifyEpisode, entity.JobKindRegenerateFeed},
			rows: sqlmock.NewRows(jobCols).AddRow(
				int64(4), entity.JobKindNotifyEpisode, []byte(`{"episode_id":1}`),
				entity.JobStatusRunning, 2, "previous failure", now, now,
			),
			wantQuery: `kind IN ($1, $2)`,
			wantArgs:  []any{entity.JobKindNotifyEpisode, entity.JobKindRegenerateFeed},
		},
		{
			name:      "no runnable job returns nil, nil",
			rows:      sqlmock.NewRows(jobCols),
			wantQuery: "FOR UPDATE SKIP LOCKED",
			wantNil:   true,
		},
		{
			name:      "database error",
			queryErr:  errors.New("db down"),
			wantQuery: "FOR UPDATE SKIP LOCKED",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newJobRepo(t)
			defer closeFn()

			exp := mock.ExpectQuery(regexp.QuoteMeta(tt.wantQuery))
			if len(tt.wantArgs) > 0 {
				args := make([]driverValue, len(tt.wantArgs))
				for i, a := range tt.wantArgs {
					args[i] = a
				}
				exp.WithArgs(args...)
			}
			if tt.queryErr != nil {
				exp.WillReturnError(tt.queryErr)
			} else {
				exp.WillReturnRows(tt.rows)
			}

			job, err := repo.ClaimNext(context.Background(), tt.kinds...)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, job)
				return
			}
			require.NotNil(t, job)
			assert.Equal(t, entity.JobStatusRunning, job.Status)
			assert.GreaterOrEqual(t, job.Attempts, 1, "ClaimNext increments attempts")
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

/* ─────────────────────────── MarkDone / MarkFailed ─────────────────────────── */

func TestJobRepo_MarkDone(t *testing.T) {
	repo, mock, closeFn := newJobRepo(t)
	defer closeFn()

	mock.ExpectExec(regexp.QuoteMeta("UPDATE jobs SET status = 'done' WHERE id = $1")).
		WithArgs(int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.MarkDone(context.Background(), 3))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestJobRepo_MarkDone_NotFound(t *testing.T) {
	repo, mock, closeFn := newJobRepo(t)
	defer closeFn()

	mock.ExpectExec("UPDATE jobs").
		WillReturnResult(sqlmock.NewResult(0, 0))

	assert.Error(t, repo.MarkDone(context.Background(), 99))
}

func TestJobRepo_MarkFailed(t *testing.T) {
	retryAt := time.Date(2026, 7, 4, 6, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		retryAt   *time.Time
		wantQuery string
		wantArgs  []driverValue
	}{
		{
			name:      "terminal failure",
			wantQuery: `UPDATE jobs SET status = 'failed', last_error = $1 WHERE id = $2`,
			wantArgs:  []driverValue{"voicevox unreachable", int64(3)},
		},
		{
			name:      "retry: back to pending with new run_after",
			retryAt:   &retryAt,
			wantQuery: `UPDATE jobs SET status = 'pending', last_error = $1, run_after = $2 WHERE id = $3`,
			wantArgs:  []driverValue{"voicevox unreachable", retryAt, int64(3)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newJobRepo(t)
			defer closeFn()

			mock.ExpectExec(regexp.QuoteMeta(tt.wantQuery)).
				WithArgs(tt.wantArgs...).
				WillReturnResult(sqlmock.NewResult(0, 1))

			require.NoError(t, repo.MarkFailed(context.Background(), 3, "voicevox unreachable", tt.retryAt))
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

/* ─────────────────────── RequeueRunning ─────────────────────── */

func TestJobRepo_RequeueRunning(t *testing.T) {
	tests := []struct {
		name string
		rows int64
	}{
		{name: "requeues orphaned running jobs", rows: 2},
		{name: "no running jobs is a no-op", rows: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newJobRepo(t)
			defer closeFn()

			mock.ExpectExec(regexp.QuoteMeta("WHERE status = 'running'")).
				WillReturnResult(sqlmock.NewResult(0, tt.rows))

			n, err := repo.RequeueRunning(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.rows, n)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestJobRepo_RequeueRunning_Error(t *testing.T) {
	repo, mock, closeFn := newJobRepo(t)
	defer closeFn()

	mock.ExpectExec(regexp.QuoteMeta("WHERE status = 'running'")).
		WillReturnError(errors.New("connection refused"))

	_, err := repo.RequeueRunning(context.Background())
	assert.ErrorContains(t, err, "RequeueRunning")
}
