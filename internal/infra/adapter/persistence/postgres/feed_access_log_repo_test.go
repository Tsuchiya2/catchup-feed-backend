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

var feedAccessRecordCols = []string{
	"id", "token_id", "episode_id", "user_agent", "accessed_at",
	"subscriber_id", "name",
}

func TestFeedAccessLogRepo_ListRecords(t *testing.T) {
	now := time.Now()
	subscriberID := int64(7)

	tests := []struct {
		name         string
		subscriberID *int64
		rows         *sqlmock.Rows
		wantLen      int
	}{
		{
			name:         "unfiltered timeline joins subscriber",
			subscriberID: nil,
			rows: sqlmock.NewRows(feedAccessRecordCols).
				AddRow(int64(2), int64(1), int64(12), "Overcast/2026", now, int64(7), "友人A").
				AddRow(int64(1), int64(3), nil, nil, now.Add(-time.Minute), int64(8), "友人B"),
			wantLen: 2,
		},
		{
			name:         "filtered by subscriber",
			subscriberID: &subscriberID,
			rows: sqlmock.NewRows(feedAccessRecordCols).
				AddRow(int64(2), int64(1), int64(12), "Overcast/2026", now, int64(7), "友人A"),
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			expect := mock.ExpectQuery(regexp.QuoteMeta("ORDER BY l.id DESC"))
			if tt.subscriberID == nil {
				expect.WithArgs(nil, 50).WillReturnRows(tt.rows)
			} else {
				expect.WithArgs(*tt.subscriberID, 50).WillReturnRows(tt.rows)
			}

			repo := pg.NewFeedAccessLogRepo(db)
			got, err := repo.ListRecords(context.Background(), tt.subscriberID, 50)
			require.NoError(t, err)
			require.Len(t, got, tt.wantLen)
			assert.Equal(t, int64(7), got[0].SubscriberID)
			assert.Equal(t, "友人A", got[0].SubscriberName)
			require.NotNil(t, got[0].EpisodeID)
			assert.Equal(t, int64(12), *got[0].EpisodeID)
			if tt.wantLen == 2 {
				assert.Nil(t, got[1].EpisodeID, "feed.xml fetch has no episode")
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestFeedAccessLogRepo_SummarizeBySubscriber(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now()
	since7 := now.AddDate(0, 0, -7)
	since30 := now.AddDate(0, 0, -30)
	lastAccess := now.Add(-48 * time.Hour)

	rows := sqlmock.NewRows([]string{
		"id", "name", "active", "last_accessed_at", "count_7d", "count_30d",
	}).
		AddRow(int64(1), "友人A", true, lastAccess, int64(5), int64(20)).
		AddRow(int64(2), "友人B(未アクセス)", true, nil, int64(0), int64(0)).
		AddRow(int64(3), "友人C(解除済み)", false, nil, int64(0), int64(0))

	mock.ExpectQuery(regexp.QuoteMeta("LEFT JOIN feed_tokens")).
		WithArgs(since7, since30).
		WillReturnRows(rows)

	repo := pg.NewFeedAccessLogRepo(db)
	got, err := repo.SummarizeBySubscriber(context.Background(), since7, since30)
	require.NoError(t, err)
	require.Len(t, got, 3, "subscribers without tokens or accesses still appear")

	assert.Equal(t, int64(1), got[0].SubscriberID)
	assert.True(t, got[0].Active)
	require.NotNil(t, got[0].LastAccessedAt)
	assert.WithinDuration(t, lastAccess, *got[0].LastAccessedAt, time.Second)
	assert.Equal(t, int64(5), got[0].Count7d)
	assert.Equal(t, int64(20), got[0].Count30d)

	assert.Nil(t, got[1].LastAccessedAt, "never-accessed friend has nil last access")
	assert.False(t, got[2].Active)
	assert.NoError(t, mock.ExpectationsWereMet())
}
