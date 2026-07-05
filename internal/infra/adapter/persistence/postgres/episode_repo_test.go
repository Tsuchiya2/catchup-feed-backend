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
	// Zero PublishedAt is sent as NULL and COALESCEd to the DB's now().
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO episodes")).
		WithArgs(entity.FeedKindPublic, episode.Title, episode.ShowNotes,
			episode.AudioPath, episode.AudioBytes, episode.DurationSec, nil).
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

// TestEpisodeRepo_Create_ExplicitPublishedAt pins the selection-window fix:
// the radio batch stores its article-selection timestamp as published_at,
// so the next run's cursor starts where this run's SELECT ran — summaries
// the worker created during the batch are picked up next time instead of
// being lost in the SELECT-to-INSERT window.
func TestEpisodeRepo_Create_ExplicitPublishedAt(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	selectedAt := time.Date(2026, 7, 5, 4, 30, 0, 0, time.UTC)
	episode := &entity.Episode{
		FeedKind: entity.FeedKindPublic, Title: "pulse 2026-07-05", ShowNotes: "n",
		AudioPath: "/data/episodes/2026-07-05.mp3", AudioBytes: 1, DurationSec: 1,
		PublishedAt: selectedAt,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO episodes")).
		WithArgs(episode.FeedKind, episode.Title, episode.ShowNotes,
			episode.AudioPath, episode.AudioBytes, episode.DurationSec, selectedAt).
		WillReturnRows(sqlmock.NewRows([]string{"id", "published_at"}).AddRow(int64(13), selectedAt))
	mock.ExpectCommit()

	require.NoError(t, repo.Create(context.Background(), episode, nil))
	assert.Equal(t, int64(13), episode.ID)
	assert.Equal(t, selectedAt, episode.PublishedAt)
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

func TestEpisodeRepo_ListRecent_AllKinds(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	now := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta("FROM episodes")).
		WithArgs(30).
		WillReturnRows(sqlmock.NewRows(episodeCols).
			AddRow(int64(3), entity.FeedKindPrivate, "private newest", "n", "/p3", int64(9), 60, now).
			AddRow(int64(2), entity.FeedKindPublic, "public older", "n", "/p2", int64(9), 60, now.Add(-24*time.Hour)))

	got, err := repo.ListRecent(context.Background(), 30)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, entity.FeedKindPrivate, got[0].FeedKind)
	assert.Equal(t, entity.FeedKindPublic, got[1].FeedKind)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEpisodeRepo_CountByKindSince(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	startOfDay := time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE feed_kind = $1 AND published_at >= $2")).
		WithArgs(entity.FeedKindPublic, startOfDay).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	count, err := repo.CountByKindSince(context.Background(), entity.FeedKindPublic, startOfDay)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.NoError(t, mock.ExpectationsWereMet())
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

/* ───────────── media retention (D-4) ───────────── */

func TestEpisodeRepo_ListWithAudioBefore(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	cutoff := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)
	published := time.Date(2026, 5, 1, 4, 30, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE published_at < $1 AND audio_path <> ''")).
		WithArgs(cutoff, 500).
		WillReturnRows(sqlmock.NewRows(episodeCols).
			AddRow(int64(1), entity.FeedKindPublic, "pulse 2026-05-01", "notes",
				"/data/episodes/2026-05-01.mp3", int64(7_000_000), 900, published))

	episodes, err := repo.ListWithAudioBefore(context.Background(), cutoff, 500)
	require.NoError(t, err)
	require.Len(t, episodes, 1)
	assert.Equal(t, "/data/episodes/2026-05-01.mp3", episodes[0].AudioPath)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestEpisodeRepo_ClearAudio(t *testing.T) {
	tests := []struct {
		name     string
		affected int64
		wantErr  bool
	}{
		{name: "clears the file reference", affected: 1},
		{name: "missing episode is an error", affected: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newEpisodeRepo(t)
			defer closeFn()

			mock.ExpectExec(regexp.QuoteMeta("UPDATE episodes SET audio_path = '', audio_bytes = 0 WHERE id = $1")).
				WithArgs(int64(7)).
				WillReturnResult(sqlmock.NewResult(0, tt.affected))

			err := repo.ClearAudio(context.Background(), 7)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestEpisodeRepo_ListAudioPaths(t *testing.T) {
	repo, mock, closeFn := newEpisodeRepo(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT audio_path FROM episodes WHERE audio_path <> ''")).
		WillReturnRows(sqlmock.NewRows([]string{"audio_path"}).
			AddRow("/data/episodes/2026-07-04.mp3").
			AddRow("/data/episodes/2026-07-05.mp3"))

	paths, err := repo.ListAudioPaths(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"/data/episodes/2026-07-04.mp3", "/data/episodes/2026-07-05.mp3"}, paths)
	assert.NoError(t, mock.ExpectationsWereMet())
}
