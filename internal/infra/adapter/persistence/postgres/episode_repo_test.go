package postgres_test

import (
	"context"
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

var episodeCols = []string{
	"id", "feed_kind", "title", "show_notes", "audio_path",
	"audio_bytes", "duration_sec", "published_at",
}

func newEpisodeRepo(t *testing.T) (repository.EpisodeRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return pg.NewEpisodeRepo(db), mock, func() { _ = db.Close() }
}

func TestEpisodeRepo_Create_WithSegments(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	now := time.Date(2026, 7, 4, 4, 30, 0, 0, time.UTC)
	articleID := int64(42)

	episode := &entity.Episode{
		FeedKind:    entity.FeedKindPublic,
		Title:       "pulse 2026-07-04",
		ShowNotes:   "- https://example.com/article",
		AudioPath:   "/data/episodes/2026-07-04.mp3",
		AudioBytes:  7_200_000,
		DurationSec: 900,
	}
	segments := []*entity.Segment{
		{Position: 1, Kind: entity.SegmentKindIntro, Script: "おはようございます"},
		{Position: 2, Kind: entity.SegmentKindNews, ArticleID: &articleID, Script: "今日のニュース"},
		{Position: 3, Kind: entity.SegmentKindOutro, Script: "また明日"},
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO episodes")).
		WithArgs(entity.FeedKindPublic, episode.Title, episode.ShowNotes,
			episode.AudioPath, episode.AudioBytes, episode.DurationSec).
		WillReturnRows(sqlmock.NewRows([]string{"id", "published_at"}).AddRow(int64(12), now))
	for i, seg := range segments {
		mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO segments")).
			WithArgs(int64(12), seg.Position, seg.Kind, seg.ArticleID, seg.Script).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(100 + i)))
	}
	mock.ExpectCommit()

	require.NoError(t, repo.Create(context.Background(), episode, segments))
	assert.Equal(t, int64(12), episode.ID)
	assert.Equal(t, now, episode.PublishedAt)
	for i, seg := range segments {
		assert.Equal(t, int64(12), seg.EpisodeID)
		assert.Equal(t, int64(100+i), seg.ID)
	}
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEpisodeRepo_Create_SegmentErrorRollsBack(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO episodes")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "published_at"}).AddRow(int64(12), time.Now()))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO segments")).
		WillReturnError(errors.New("unique violation: (episode_id, position)"))
	mock.ExpectRollback()

	err := repo.Create(context.Background(),
		&entity.Episode{FeedKind: entity.FeedKindPrivate, Title: "t", ShowNotes: "n", AudioPath: "/p", AudioBytes: 1, DurationSec: 1},
		[]*entity.Segment{{Position: 1, Kind: entity.SegmentKindIntro, Script: "s"}},
	)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEpisodeRepo_Get(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("FROM episodes")).
		WithArgs(int64(12)).
		WillReturnRows(sqlmock.NewRows(episodeCols).
			AddRow(int64(12), entity.FeedKindPublic, "t", "notes", "/p", int64(9), 60, now))

	got, err := repo.Get(context.Background(), 12)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, entity.FeedKindPublic, got.FeedKind)
	assert.Equal(t, int64(9), got.AudioBytes)
}

func TestEpisodeRepo_Get_NotFound(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM episodes")).
		WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows(episodeCols))

	got, err := repo.Get(context.Background(), 99)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestEpisodeRepo_ListByKind(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("WHERE feed_kind = $1")).
		WithArgs(entity.FeedKindPublic, 30).
		WillReturnRows(sqlmock.NewRows(episodeCols).
			AddRow(int64(2), entity.FeedKindPublic, "newest", "n", "/p2", int64(9), 60, now).
			AddRow(int64(1), entity.FeedKindPublic, "older", "n", "/p1", int64(9), 60, now.Add(-24*time.Hour)))

	got, err := repo.ListByKind(context.Background(), entity.FeedKindPublic, 30)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "newest", got[0].Title)
}

func TestEpisodeRepo_ListSegments(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	articleID := int64(42)
	mock.ExpectQuery(regexp.QuoteMeta("FROM segments")).
		WithArgs(int64(12)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "episode_id", "position", "kind", "article_id", "script"}).
			AddRow(int64(1), int64(12), 1, entity.SegmentKindIntro, nil, "intro script").
			AddRow(int64(2), int64(12), 2, entity.SegmentKindNews, articleID, "news script"))

	got, err := repo.ListSegments(context.Background(), 12)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Nil(t, got[0].ArticleID)
	require.NotNil(t, got[1].ArticleID)
	assert.Equal(t, articleID, *got[1].ArticleID)
}
