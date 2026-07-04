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
)

func TestSummaryRepo_Upsert(t *testing.T) {
	tests := []struct {
		name         string
		summary      *entity.Summary
		wantProvider string
	}{
		{
			name: "provider from the fallback chain is persisted",
			summary: &entity.Summary{
				ArticleID: 1, Body: "日本語要約", Provider: "gemini",
			},
			wantProvider: "gemini",
		},
		{
			name: "empty provider falls back to unknown (NOT NULL column)",
			summary: &entity.Summary{
				ArticleID: 2, Body: "要約",
			},
			wantProvider: entity.SummaryProviderUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			mock.ExpectExec(regexp.QuoteMeta("ON CONFLICT (article_id) DO UPDATE")).
				WithArgs(tt.summary.ArticleID, tt.summary.Body, tt.wantProvider).
				WillReturnResult(sqlmock.NewResult(0, 1))

			repo := pg.NewSummaryRepo(db)
			require.NoError(t, repo.Upsert(context.Background(), tt.summary))
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSummaryRepo_Upsert_DatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO summaries")).
		WillReturnError(errors.New("fk violation"))

	repo := pg.NewSummaryRepo(db)
	assert.Error(t, repo.Upsert(context.Background(), &entity.Summary{
		ArticleID: 999, Body: "b", Provider: "groq",
	}))
}

func TestSummaryRepo_GetByArticleID(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		rows *sqlmock.Rows
		want *entity.Summary
	}{
		{
			name: "found",
			rows: sqlmock.NewRows([]string{"article_id", "body", "provider", "created_at"}).
				AddRow(int64(1), "要約", "ollama", now),
			want: &entity.Summary{ArticleID: 1, Body: "要約", Provider: "ollama", CreatedAt: now},
		},
		{
			name: "not summarized yet returns nil, nil",
			rows: sqlmock.NewRows([]string{"article_id", "body", "provider", "created_at"}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			mock.ExpectQuery(regexp.QuoteMeta("FROM summaries")).
				WithArgs(int64(1)).
				WillReturnRows(tt.rows)

			repo := pg.NewSummaryRepo(db)
			got, err := repo.GetByArticleID(context.Background(), 1)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
