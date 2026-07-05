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

	pg "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/repository"
)

var radioArticleCols = []string{
	"id", "title", "url", "category", "name", "body", "published_at",
}

func newRadioArticleRepo(t *testing.T) (repository.RadioArticleRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return pg.NewRadioArticleRepo(db), mock, func() { _ = db.Close() }
}

func TestRadioArticleRepo_ListSummarizedSince(t *testing.T) {
	repo, mock, closeFn := newRadioArticleRepo(t)
	defer closeFn()

	since := time.Date(2026, 7, 4, 4, 30, 0, 0, time.UTC)
	published := time.Date(2026, 7, 4, 18, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("WHERE sm.created_at > $1")).
		WithArgs(since, 200).
		WillReturnRows(sqlmock.NewRows(radioArticleCols).
			AddRow(int64(10), "Go 1.26", "https://example.com/go", "golang", "Go Blog", "要約本文", published))

	got, err := repo.ListSummarizedSince(context.Background(), since, 200)
	require.NoError(t, err)
	require.Len(t, got, 1)

	assert.Equal(t, int64(10), got[0].ID)
	assert.Equal(t, "Go 1.26", got[0].Title)
	assert.Equal(t, "https://example.com/go", got[0].URL)
	assert.Equal(t, "golang", got[0].Category)
	assert.Equal(t, "Go Blog", got[0].SourceName)
	assert.Equal(t, "要約本文", got[0].Summary)
	assert.Equal(t, published, got[0].PublishedAt)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestRadioArticleRepo_ListSummarizedSince_ExcludesBroadcastArticles pins
// the structural exclusion of already-broadcast articles (NOT EXISTS on
// segments). This is what makes the deliberate cursor overlap safe and
// keeps a manual -since re-run — even one pointed far into the past — from
// double-airing articles that already have a segment row (§6-6 冪等性).
func TestRadioArticleRepo_ListSummarizedSince_ExcludesBroadcastArticles(t *testing.T) {
	repo, mock, closeFn := newRadioArticleRepo(t)
	defer closeFn()

	farPast := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) // -since で過去に戻した想定
	exclusion := regexp.QuoteMeta("NOT EXISTS (SELECT 1 FROM segments sg WHERE sg.article_id = a.id)")

	// The expectation only matches when the SQL contains the NOT EXISTS
	// clause; the DB answers with zero rows for already-broadcast articles.
	mock.ExpectQuery(exclusion).
		WithArgs(farPast, 200).
		WillReturnRows(sqlmock.NewRows(radioArticleCols))

	got, err := repo.ListSummarizedSince(context.Background(), farPast, 200)
	require.NoError(t, err)
	assert.Empty(t, got, "articles with segment rows must never be re-selected")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRadioArticleRepo_ListSummarizedSince_Empty(t *testing.T) {
	repo, mock, closeFn := newRadioArticleRepo(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("WHERE sm.created_at > $1")).
		WithArgs(sqlmock.AnyArg(), 200).
		WillReturnRows(sqlmock.NewRows(radioArticleCols))

	got, err := repo.ListSummarizedSince(context.Background(), time.Time{}, 200)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestRadioArticleRepo_ListSummarizedSince_QueryError(t *testing.T) {
	repo, mock, closeFn := newRadioArticleRepo(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("WHERE sm.created_at > $1")).
		WillReturnError(errors.New("connection refused"))

	_, err := repo.ListSummarizedSince(context.Background(), time.Time{}, 200)
	assert.Error(t, err)
}
