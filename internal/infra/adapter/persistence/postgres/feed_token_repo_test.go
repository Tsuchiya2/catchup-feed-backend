package postgres_test

import (
	"context"
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

var feedTokenCols = []string{"id", "subscriber_id", "token_hash", "created_at", "revoked_at"}

func newFeedTokenRepo(t *testing.T) (repository.FeedTokenRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return pg.NewFeedTokenRepo(db), mock, func() { _ = db.Close() }
}

func TestFeedTokenRepo_Create(t *testing.T) {
	repo, mock, closeFn := newFeedTokenRepo(t)
	defer closeFn()

	_, hash, err := entity.GenerateFeedToken()
	require.NoError(t, err)

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO feed_tokens")).
		WithArgs(int64(2), hash).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(10), now))

	token := &entity.FeedToken{SubscriberID: 2, TokenHash: hash}
	require.NoError(t, repo.Create(context.Background(), token))
	assert.Equal(t, int64(10), token.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFeedTokenRepo_Get(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	revoked := now.Add(time.Hour)

	tests := []struct {
		name string
		rows *sqlmock.Rows
		want *entity.FeedToken
	}{
		{
			name: "revoked token is still returned by ID",
			rows: sqlmock.NewRows(feedTokenCols).
				AddRow(int64(4), int64(2), "hash", now, revoked),
			want: &entity.FeedToken{ID: 4, SubscriberID: 2, TokenHash: "hash", CreatedAt: now, RevokedAt: &revoked},
		},
		{
			name: "unknown id returns nil, nil",
			rows: sqlmock.NewRows(feedTokenCols),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newFeedTokenRepo(t)
			defer closeFn()

			mock.ExpectQuery(regexp.QuoteMeta("WHERE id = $1")).
				WithArgs(int64(4)).
				WillReturnRows(tt.rows)

			got, err := repo.Get(context.Background(), 4)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

// TestFeedTokenRepo_GetActiveByHash pins the §5.2 verification lookup:
// the SQL must require an unrevoked token AND an active subscriber, and the
// caller must not be able to distinguish revoked / unknown / deactivated.
func TestFeedTokenRepo_GetActiveByHash(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	hash := entity.HashFeedToken("some-plaintext-token")

	tests := []struct {
		name string
		rows *sqlmock.Rows
		want *entity.FeedToken
	}{
		{
			name: "valid token of active subscriber",
			rows: sqlmock.NewRows(feedTokenCols).
				AddRow(int64(1), int64(2), hash, now, nil),
			want: &entity.FeedToken{ID: 1, SubscriberID: 2, TokenHash: hash, CreatedAt: now},
		},
		{
			// revoked tokens / deactivated subscribers / unknown hashes are
			// all filtered by the WHERE clause: zero rows, nil result.
			name: "no matching row returns nil, nil",
			rows: sqlmock.NewRows(feedTokenCols),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newFeedTokenRepo(t)
			defer closeFn()

			mock.ExpectQuery(regexp.QuoteMeta(
				`WHERE t.token_hash = $1
  AND t.revoked_at IS NULL
  AND s.deactivated_at IS NULL`)).
				WithArgs(hash).
				WillReturnRows(tt.rows)

			got, err := repo.GetActiveByHash(context.Background(), hash)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestFeedTokenRepo_Revoke(t *testing.T) {
	repo, mock, closeFn := newFeedTokenRepo(t)
	defer closeFn()

	at := time.Now()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE feed_tokens SET revoked_at = $1")).
		WithArgs(at, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Revoke(context.Background(), 1, at))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestFeedTokenRepo_ListBySubscriber(t *testing.T) {
	repo, mock, closeFn := newFeedTokenRepo(t)
	defer closeFn()

	now := time.Now()
	revoked := now.Add(-time.Hour)
	rows := sqlmock.NewRows(feedTokenCols).
		AddRow(int64(2), int64(7), "hash-b", now, nil).
		AddRow(int64(1), int64(7), "hash-a", now.Add(-2*time.Hour), revoked)

	mock.ExpectQuery(regexp.QuoteMeta("WHERE subscriber_id = $1")).
		WithArgs(int64(7)).
		WillReturnRows(rows)

	got, err := repo.ListBySubscriber(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.False(t, got[0].IsRevoked())
	assert.True(t, got[1].IsRevoked())
}
