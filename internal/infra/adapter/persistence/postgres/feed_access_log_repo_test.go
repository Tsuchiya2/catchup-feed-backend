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
)

func TestFeedAccessLogRepo_Insert(t *testing.T) {
	episodeID := int64(12)
	userAgent := "Overcast/2026"

	tests := []struct {
		name string
		log  *entity.FeedAccessLog
	}{
		{
			name: "episode download",
			log:  &entity.FeedAccessLog{TokenID: 1, EpisodeID: &episodeID, UserAgent: &userAgent},
		},
		{
			name: "feed.xml fetch has NULL episode_id",
			log:  &entity.FeedAccessLog{TokenID: 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			now := time.Now()
			mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO feed_access_logs")).
				WithArgs(tt.log.TokenID, tt.log.EpisodeID, tt.log.UserAgent).
				WillReturnRows(sqlmock.NewRows([]string{"id", "accessed_at"}).AddRow(int64(5), now))

			repo := pg.NewFeedAccessLogRepo(db)
			require.NoError(t, repo.Insert(context.Background(), tt.log))
			assert.Equal(t, int64(5), tt.log.ID)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestFeedAccessLogRepo_ListRecent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{"id", "token_id", "episode_id", "user_agent", "accessed_at"}).
		AddRow(int64(2), int64(1), int64(12), "Overcast/2026", now).
		AddRow(int64(1), int64(1), nil, nil, now.Add(-time.Minute))

	mock.ExpectQuery(regexp.QuoteMeta("ORDER BY id DESC")).
		WithArgs(50).
		WillReturnRows(rows)

	repo := pg.NewFeedAccessLogRepo(db)
	got, err := repo.ListRecent(context.Background(), 50)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.NotNil(t, got[0].EpisodeID)
	assert.Equal(t, int64(12), *got[0].EpisodeID)
	assert.Nil(t, got[1].EpisodeID, "feed.xml fetch has no episode")
}
