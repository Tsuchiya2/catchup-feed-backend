package postgres_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
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

// driverValue makes expected-arg tables readable (nil = SQL NULL).
type driverValue = driver.Value

/* ─────────────────────────── ヘルパ ─────────────────────────── */

// articleCols is the column list every article read returns (§4 articles +
// summaries.body join).
var articleCols = []string{
	"id", "source_id", "title", "url", "content",
	"summary", "published_at", "crawled_at",
}

func artRow(a *entity.Article) *sqlmock.Rows {
	return sqlmock.NewRows(articleCols).AddRow(
		a.ID, a.SourceID, a.Title, a.URL, a.Content,
		a.Summary, a.PublishedAt, a.CrawledAt,
	)
}

func newArticleRepo(t *testing.T) (repository.ArticleRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return pg.NewArticleRepo(db), mock, func() { _ = db.Close() }
}

/* ─────────────────────────── Get ─────────────────────────── */

func TestArticleRepo_Get(t *testing.T) {
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		rows    *sqlmock.Rows
		queryEr error
		want    *entity.Article
		wantErr bool
	}{
		{
			name: "found with content and summary",
			want: &entity.Article{
				ID: 1, SourceID: 2, Title: "Go 1.26 released",
				URL: "https://example.com", Content: "full text",
				Summary: "日本語要約", PublishedAt: now, CrawledAt: now,
			},
		},
		{
			name: "NULL published_at maps to zero time",
			rows: sqlmock.NewRows(articleCols).
				AddRow(int64(1), int64(2), "t", "https://u", "", "", nil, now),
			want: &entity.Article{
				ID: 1, SourceID: 2, Title: "t", URL: "https://u", CrawledAt: now,
			},
		},
		{
			name: "not found returns nil, nil",
			rows: sqlmock.NewRows(articleCols),
		},
		{
			name:    "database error",
			queryEr: errors.New("db down"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newArticleRepo(t)
			defer closeFn()

			exp := mock.ExpectQuery(regexp.QuoteMeta("LEFT JOIN summaries sm ON sm.article_id = a.id")).
				WithArgs(int64(1))
			switch {
			case tt.queryEr != nil:
				exp.WillReturnError(tt.queryEr)
			case tt.rows != nil:
				exp.WillReturnRows(tt.rows)
			default:
				exp.WillReturnRows(artRow(tt.want))
			}

			got, err := repo.Get(context.Background(), 1)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

/* ─────────────────────────── List / Search ─────────────────────────── */

func TestArticleRepo_List(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	now := time.Now()
	mock.ExpectQuery("FROM articles a").
		WillReturnRows(artRow(&entity.Article{
			ID: 1, SourceID: 2, Title: "x", URL: "y",
			Summary: "s", PublishedAt: now, CrawledAt: now,
		}))

	got, err := repo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "s", got[0].Summary, "summary joined from summaries.body")
}

func TestArticleRepo_List_ScanError(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	mock.ExpectQuery("FROM articles a").
		WillReturnRows(sqlmock.NewRows(articleCols).
			AddRow("not-an-int", int64(2), "t", "u", "", "", time.Now(), time.Now()))

	_, err := repo.List(context.Background())
	assert.Error(t, err)
}

func TestArticleRepo_Search(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	// Keyword search hits title and the joined summary body (sm.body).
	mock.ExpectQuery(regexp.QuoteMeta("sm.body ILIKE $1")).
		WithArgs("%go%").
		WillReturnRows(sqlmock.NewRows(articleCols))

	_, err := repo.Search(context.Background(), "go")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

/* ─────────────────────────── SearchWithFilters ─────────────────────────── */

func TestArticleRepo_SearchWithFilters(t *testing.T) {
	now := time.Now()
	sourceID := int64(7)

	tests := []struct {
		name      string
		keywords  []string
		filters   repository.ArticleSearchFilters
		wantQuery string
		wantArgs  []driverValue
		noQuery   bool
	}{
		{
			name:      "single keyword searches title and sm.body",
			keywords:  []string{"go"},
			wantQuery: `(a.title ILIKE $1 OR sm.body ILIKE $1)`,
			wantArgs:  []driverValue{"%go%"},
		},
		{
			name:      "source id filter",
			filters:   repository.ArticleSearchFilters{SourceID: &sourceID},
			wantQuery: `a.source_id = $1`,
			wantArgs:  []driverValue{sourceID},
		},
		{
			name:      "keyword and date range",
			keywords:  []string{"go"},
			filters:   repository.ArticleSearchFilters{From: &now},
			wantQuery: `a.published_at >= $2`,
			wantArgs:  []driverValue{"%go%", now},
		},
		{
			name:    "no keywords and no filters returns empty without querying",
			noQuery: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newArticleRepo(t)
			defer closeFn()

			if !tt.noQuery {
				args := make([]driver.Value, len(tt.wantArgs))
				copy(args, tt.wantArgs)
				mock.ExpectQuery(regexp.QuoteMeta(tt.wantQuery)).
					WithArgs(args...).
					WillReturnRows(sqlmock.NewRows(articleCols))
			}

			got, err := repo.SearchWithFilters(context.Background(), tt.keywords, tt.filters)
			require.NoError(t, err)
			assert.Empty(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestArticleRepo_CountArticlesWithFilters_JoinsSummaries(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	// COUNT must use the same summaries join because keywords search sm.body.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM articles a\nLEFT JOIN summaries sm ON sm.article_id = a.id")).
		WithArgs("%go%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(3)))

	got, err := repo.CountArticlesWithFilters(context.Background(), []string{"go"}, repository.ArticleSearchFilters{})
	require.NoError(t, err)
	assert.Equal(t, int64(3), got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestArticleRepo_CountArticlesWithFilters_NoCriteria(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	got, err := repo.CountArticlesWithFilters(context.Background(), nil, repository.ArticleSearchFilters{})
	require.NoError(t, err)
	assert.Zero(t, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

/* ─────────────────────────── Pagination ─────────────────────────── */

func TestArticleRepo_ListWithSourcePaginated(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	now := time.Now()
	rows := sqlmock.NewRows(append(articleCols, "source_name")).
		AddRow(int64(1), int64(2), "t", "https://u", "c", "s", now, now, "Go Blog")

	mock.ExpectQuery("LIMIT \\$1 OFFSET \\$2").
		WithArgs(10, 20).
		WillReturnRows(rows)

	got, err := repo.ListWithSourcePaginated(context.Background(), 20, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "Go Blog", got[0].SourceName)
	assert.Equal(t, "s", got[0].Article.Summary)
}

func TestArticleRepo_SearchWithFiltersPaginated_WithKeywords(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("LIMIT $2 OFFSET $3")).
		WithArgs("%go%", 10, 0).
		WillReturnRows(sqlmock.NewRows(append(articleCols, "source_name")))

	got, err := repo.SearchWithFiltersPaginated(context.Background(), []string{"go"}, repository.ArticleSearchFilters{}, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestArticleRepo_CountArticles(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM articles")).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(int64(42)))

	got, err := repo.CountArticles(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(42), got)
}

/* ─────────────────────────── Create ─────────────────────────── */

func TestArticleRepo_Create(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		article     *entity.Article
		wantContent driverValue // nil = SQL NULL
		wantPubAt   driverValue
	}{
		{
			name: "full article",
			article: &entity.Article{
				SourceID: 2, Title: "title", URL: "https://u",
				Content: "full text", PublishedAt: now, CrawledAt: now,
			},
			wantContent: "full text",
			wantPubAt:   now,
		},
		{
			name: "empty content and zero published_at stored as NULL",
			article: &entity.Article{
				SourceID: 2, Title: "title", URL: "https://u", CrawledAt: now,
			},
			wantContent: nil,
			wantPubAt:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newArticleRepo(t)
			defer closeFn()

			mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO articles")).
				WithArgs(int64(2), "title", "https://u",
					tt.wantContent, tt.wantPubAt, now).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(99)))

			err := repo.Create(context.Background(), tt.article)
			require.NoError(t, err)
			assert.Equal(t, int64(99), tt.article.ID,
				"Create must set the returned id (summaries FK depends on it)")
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestArticleRepo_CreateWithSummary(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO articles")).
		WithArgs(int64(2), "title", "https://u", "full text", now, now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(99)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO summaries")).
		WithArgs(int64(99), "日本語要約", "gemini").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	art := &entity.Article{
		SourceID: 2, Title: "title", URL: "https://u",
		Content: "full text", PublishedAt: now, CrawledAt: now,
	}
	sum := &entity.Summary{Body: "日本語要約", Provider: "gemini"}

	require.NoError(t, repo.CreateWithSummary(context.Background(), art, sum))
	assert.Equal(t, int64(99), art.ID)
	assert.Equal(t, int64(99), sum.ArticleID, "summary FK is taken from the new article id")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestArticleRepo_CreateWithSummary_SummaryErrorRollsBack pins the §8
// invariant: a summary insert failure must roll the article back, so no
// article row can exist without its summary (the URL stays unknown and the
// next hourly crawl retries).
func TestArticleRepo_CreateWithSummary_SummaryErrorRollsBack(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO articles")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(99)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO summaries")).
		WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()

	err := repo.CreateWithSummary(context.Background(),
		&entity.Article{SourceID: 2, Title: "t", URL: "https://u", CrawledAt: time.Now()},
		&entity.Summary{Body: "要約", Provider: "groq"},
	)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestArticleRepo_CreateWithSummary_DefaultsProvider(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO articles")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO summaries")).
		WithArgs(int64(1), "要約", entity.SummaryProviderUnknown).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.CreateWithSummary(context.Background(),
		&entity.Article{SourceID: 2, Title: "t", URL: "https://u", CrawledAt: now},
		&entity.Summary{Body: "要約"}, // provider unreported -> "unknown"
	))
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestArticleRepo_CreateWithTranscribeJob pins the Phase 2 §5 invariant:
// the content-less article and its transcribe job land in one transaction,
// and the payload carries {article_id, media_url, source_kind} — the
// contract the Mac transcribe worker reads.
func TestArticleRepo_CreateWithTranscribeJob(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	now := time.Now()
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO articles")).
		WithArgs(int64(2), "Ep 1", "https://example.com/ep1",
			nil, // content is stored as NULL until transcribed
			now, now).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO jobs")).
		WithArgs(entity.JobKindTranscribe,
			[]byte(`{"article_id":42,"media_url":"https://cdn.example.com/ep1.mp3","source_kind":"podcast"}`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	art := &entity.Article{
		SourceID: 2, Title: "Ep 1", URL: "https://example.com/ep1",
		PublishedAt: now, CrawledAt: now,
	}
	require.NoError(t, repo.CreateWithTranscribeJob(context.Background(),
		art, "https://cdn.example.com/ep1.mp3", entity.SourceKindPodcast))
	assert.Equal(t, int64(42), art.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestArticleRepo_CreateWithTranscribeJob_JobErrorRollsBack: a job insert
// failure must roll the article back — otherwise a content-less article
// would exist with no transcribe job to ever fill it (stuck row).
func TestArticleRepo_CreateWithTranscribeJob_JobErrorRollsBack(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO articles")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO jobs")).
		WillReturnError(errors.New("connection reset"))
	mock.ExpectRollback()

	err := repo.CreateWithTranscribeJob(context.Background(),
		&entity.Article{SourceID: 2, Title: "t", URL: "https://u", CrawledAt: time.Now()},
		"https://www.youtube.com/watch?v=abc", entity.SourceKindYouTube)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestArticleRepo_Create_DatabaseError(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO articles")).
		WillReturnError(errors.New("insert failed"))

	err := repo.Create(context.Background(), &entity.Article{
		SourceID: 1, Title: "t", URL: "https://u", CrawledAt: time.Now(),
	})
	assert.Error(t, err)
}

/* ─────────────────────────── Update / Delete ─────────────────────────── */

func TestArticleRepo_Update(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	now := time.Now()
	mock.ExpectExec("UPDATE articles").
		WithArgs(int64(2), "new", "https://u", "content", now, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Update(context.Background(), &entity.Article{
		ID: 1, SourceID: 2, Title: "new", URL: "https://u",
		Content: "content", PublishedAt: now,
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestArticleRepo_Update_NotFound(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	mock.ExpectExec("UPDATE articles").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Update(context.Background(), &entity.Article{
		ID: 99, SourceID: 2, Title: "t", URL: "https://u",
	})
	assert.Error(t, err)
}

func TestArticleRepo_Delete(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	// summaries.article_id REFERENCES articles: the summary row goes first,
	// both in one transaction.
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM summaries WHERE article_id = $1")).
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM articles WHERE id = $1")).
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.Delete(context.Background(), 1)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestArticleRepo_Delete_NotFound(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM summaries")).
		WithArgs(int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM articles")).
		WithArgs(int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err := repo.Delete(context.Background(), 99)
	assert.Error(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

/* ─────────────────────────── Exists ─────────────────────────── */

func TestArticleRepo_ExistsByURL(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	mock.ExpectQuery("SELECT EXISTS").
		WithArgs("https://u").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	got, err := repo.ExistsByURL(context.Background(), "https://u")
	require.NoError(t, err)
	assert.True(t, got)
}

func TestArticleRepo_ExistsByURLBatch(t *testing.T) {
	tests := []struct {
		name     string
		urls     []string
		existing []string
		want     map[string]bool
		noQuery  bool
	}{
		{
			name:     "subset exists",
			urls:     []string{"https://a", "https://b"},
			existing: []string{"https://a"},
			want:     map[string]bool{"https://a": true},
		},
		{
			name:    "empty input short-circuits",
			urls:    nil,
			want:    map[string]bool{},
			noQuery: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newArticleRepo(t)
			defer closeFn()

			if !tt.noQuery {
				rows := sqlmock.NewRows([]string{"url"})
				for _, u := range tt.existing {
					rows.AddRow(u)
				}
				args := make([]driver.Value, len(tt.urls))
				for i, u := range tt.urls {
					args[i] = u
				}
				mock.ExpectQuery("SELECT url FROM articles WHERE url IN").
					WithArgs(args...).
					WillReturnRows(rows)
			}

			got, err := repo.ExistsByURLBatch(context.Background(), tt.urls)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

/* ─────────────────────────── GetWithSource ─────────────────────────── */

func TestArticleRepo_GetWithSource(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	now := time.Now()
	rows := sqlmock.NewRows(append(articleCols, "source_name")).
		AddRow(int64(1), int64(2), "t", "https://u", "c", "s", now, now, "Go Blog")

	mock.ExpectQuery("INNER JOIN sources s ON a.source_id = s.id").
		WithArgs(int64(1)).
		WillReturnRows(rows)

	article, sourceName, err := repo.GetWithSource(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, article)
	assert.Equal(t, "Go Blog", sourceName)
	assert.Equal(t, "s", article.Summary)
}

func TestArticleRepo_GetWithSource_NotFound(t *testing.T) {
	repo, mock, closeFn := newArticleRepo(t)
	defer closeFn()

	mock.ExpectQuery("INNER JOIN sources s ON a.source_id = s.id").
		WithArgs(int64(9)).
		WillReturnError(sql.ErrNoRows)

	article, sourceName, err := repo.GetWithSource(context.Background(), 9)
	require.NoError(t, err)
	assert.Nil(t, article)
	assert.Empty(t, sourceName)
}

/* ─────────────────── ListUnsummarized (Phase 2 §5.2b) ─────────────────── */

func TestArticleRepo_ListUnsummarized(t *testing.T) {
	now := time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		rows    *sqlmock.Rows
		queryEr error
		wantLen int
		wantErr bool
	}{
		{
			name: "returns content-filled articles without summaries",
			rows: sqlmock.NewRows(articleCols).
				AddRow(int64(1), int64(2), "transcribed", "https://u1", "transcript text", "", now, now).
				AddRow(int64(3), int64(2), "another", "https://u2", "more text", "", nil, now),
			wantLen: 2,
		},
		{
			name:    "no candidates returns empty slice",
			rows:    sqlmock.NewRows(articleCols),
			wantLen: 0,
		},
		{
			name:    "database error",
			queryEr: errors.New("db down"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mock, closeFn := newArticleRepo(t)
			defer closeFn()

			// The WHERE clause is the §5.2b target definition: content
			// present AND no summaries row (via the shared LEFT JOIN).
			exp := mock.ExpectQuery(regexp.QuoteMeta(
				"WHERE a.content IS NOT NULL AND a.content <> ''\n  AND sm.article_id IS NULL\nORDER BY a.id\nLIMIT $1")).
				WithArgs(50)
			if tt.queryEr != nil {
				exp.WillReturnError(tt.queryEr)
			} else {
				exp.WillReturnRows(tt.rows)
			}

			got, err := repo.ListUnsummarized(context.Background(), 50)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
