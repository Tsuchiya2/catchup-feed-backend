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

func newBookAdminRepo(t *testing.T) (repository.BookAdminRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return pg.NewBookAdminRepo(db), mock, func() { _ = db.Close() }
}

/* ─────────────────────────── ListBooks ─────────────────────────── */

func TestBookAdminRepo_ListBooks(t *testing.T) {
	repo, mock, cleanup := newBookAdminRepo(t)
	defer cleanup()

	imported := time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT b.id, b.title, b.file_path, b.imported_at,")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "file_path", "imported_at", "chunk_count"}).
			AddRow(int64(1), "実用 Go 言語", "/data/books/go.pdf", imported, 42).
			AddRow(int64(2), "CLI の本", "/Users/mac/books/cli.pdf", imported, 7))

	got, err := repo.ListBooks(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, repository.BookRecord{
		ID: 1, Title: "実用 Go 言語", FilePath: "/data/books/go.pdf", ImportedAt: imported, ChunkCount: 42,
	}, got[0])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestBookAdminRepo_ListBooks_Empty(t *testing.T) {
	repo, mock, cleanup := newBookAdminRepo(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT b.id, b.title, b.file_path, b.imported_at,")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "file_path", "imported_at", "chunk_count"}))

	got, err := repo.ListBooks(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got)
}

/* ──────────────────────── LatestIngestStates ──────────────────────── */

func TestBookAdminRepo_LatestIngestStates(t *testing.T) {
	repo, mock, cleanup := newBookAdminRepo(t)
	defer cleanup()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT ON (payload->>'file_path')")).
		WithArgs("book_ingest").
		WillReturnRows(sqlmock.NewRows([]string{"file_path", "status", "title"}).
			AddRow("/data/books/a.pdf", "pending", "A").
			AddRow("/data/books/b.pdf", "failed", ""))

	got, err := repo.LatestIngestStates(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]repository.IngestJobState{
		"/data/books/a.pdf": {Status: "pending", Title: "A"},
		"/data/books/b.pdf": {Status: "failed", Title: ""},
	}, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

/* ───────────────────────── HasPendingIngest ───────────────────────── */

func TestBookAdminRepo_HasPendingIngest(t *testing.T) {
	tests := []struct {
		name   string
		exists bool
	}{
		{"pending job exists", true},
		{"no pending job", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, cleanup := newBookAdminRepo(t)
			defer cleanup()

			mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS (")).
				WithArgs("book_ingest", "pending", "/data/books/a.pdf").
				WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(tt.exists))

			got, err := repo.HasPendingIngest(context.Background(), "/data/books/a.pdf")
			require.NoError(t, err)
			assert.Equal(t, tt.exists, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

/* ──────────────────────── CancelPendingIngest ──────────────────────── */

func TestBookAdminRepo_CancelPendingIngest(t *testing.T) {
	repo, mock, cleanup := newBookAdminRepo(t)
	defer cleanup()

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM jobs")).
		WithArgs("book_ingest", "pending", "/data/books/a.pdf").
		WillReturnResult(sqlmock.NewResult(0, 2))

	n, err := repo.CancelPendingIngest(context.Background(), "/data/books/a.pdf")
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	assert.NoError(t, mock.ExpectationsWereMet())
}

/* ─────────────────────── DeleteBookByFilePath ─────────────────────── */

func TestBookAdminRepo_DeleteBookByFilePath(t *testing.T) {
	tests := []struct {
		name        string
		booksRows   int64
		wantDeleted bool
	}{
		{"book existed", 1, true},
		{"no book row", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, cleanup := newBookAdminRepo(t)
			defer cleanup()

			const path = "/data/books/a.pdf"
			mock.ExpectBegin()
			mock.ExpectExec(regexp.QuoteMeta("DELETE FROM review_logs")).
				WithArgs(path).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(regexp.QuoteMeta("DELETE FROM learning_items")).
				WithArgs(path).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(regexp.QuoteMeta("DELETE FROM book_chunks")).
				WithArgs(path).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec(regexp.QuoteMeta("DELETE FROM books")).
				WithArgs(path).WillReturnResult(sqlmock.NewResult(0, tt.booksRows))
			mock.ExpectCommit()

			deleted, err := repo.DeleteBookByFilePath(context.Background(), path)
			require.NoError(t, err)
			assert.Equal(t, tt.wantDeleted, deleted)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestBookAdminRepo_DeleteBookByFilePath_RollsBackOnError(t *testing.T) {
	repo, mock, cleanup := newBookAdminRepo(t)
	defer cleanup()

	const path = "/data/books/a.pdf"
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM review_logs")).
		WithArgs(path).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM learning_items")).
		WithArgs(path).WillReturnError(errors.New("boom"))
	mock.ExpectRollback()

	_, err := repo.DeleteBookByFilePath(context.Background(), path)
	require.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
