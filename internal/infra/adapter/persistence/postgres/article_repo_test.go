package postgres_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/go-cmp/cmp"

	"catchup-feed/internal/domain/entity"
	pg "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/repository"
)

/* ─────────────────────────── ヘルパ ─────────────────────────── */

func artRow(a *entity.Article) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "source_id", "title", "url",
		"summary", "published_at", "created_at",
	}).AddRow(
		a.ID, a.SourceID, a.Title, a.URL,
		a.Summary, a.PublishedAt, a.CreatedAt,
	)
}

/* ─────────────────────────── 1. Get ─────────────────────────── */

func TestArticleRepo_Get(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Date(2025, 7, 19, 0, 0, 0, 0, time.UTC)
	want := &entity.Article{
		ID: 1, SourceID: 2, Title: "Go 1.24 released",
		URL: "https://example.com", Summary: "sum",
		PublishedAt: now, CreatedAt: now,
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id")).
		WithArgs(int64(1)).
		WillReturnRows(artRow(want))

	repo := pg.NewArticleRepo(db)
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("Get err=%v", err)
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(entity.Article{})); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ─────────────────────────── 2. List ─────────────────────────── */

func TestArticleRepo_List(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectQuery("FROM articles").
		WillReturnRows(artRow(&entity.Article{
			ID: 1, SourceID: 2, Title: "x", URL: "y",
			Summary: "s", PublishedAt: now, CreatedAt: now,
		}))

	repo := pg.NewArticleRepo(db)
	got, err := repo.List(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("List err=%v len=%d", err, len(got))
	}
}

/* ─────────────────────────── 3. Search ─────────────────────────── */

func TestArticleRepo_Search(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("FROM articles").
		WithArgs("%go%").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at",
		})) // 空集合で OK

	repo := pg.NewArticleRepo(db)
	if _, err := repo.Search(context.Background(), "go"); err != nil {
		t.Fatalf("Search err=%v", err)
	}
}

/* ─────────────────────────── 4. Create ─────────────────────────── */

func TestArticleRepo_Create(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO articles")).
		WithArgs(int64(2), "title", "https://u",
			"summary", now, now).
		WillReturnResult(sqlmock.NewResult(1, 1))

	repo := pg.NewArticleRepo(db)
	err := repo.Create(context.Background(), &entity.Article{
		SourceID: 2, Title: "title", URL: "https://u",
		Summary: "summary", PublishedAt: now, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("Create err=%v", err)
	}
}

/* ─────────────────────────── 5. Update ─────────────────────────── */

func TestArticleRepo_Update(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()

	mock.ExpectExec("UPDATE articles").
		WithArgs(int64(2), "new", "https://u",
			"sum", now, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := pg.NewArticleRepo(db)
	err := repo.Update(context.Background(), &entity.Article{
		ID: 1, SourceID: 2, Title: "new", URL: "https://u",
		Summary: "sum", PublishedAt: now,
	})
	if err != nil {
		t.Fatalf("Update err=%v", err)
	}
}

/* ─────────────────────────── 6. Delete ─────────────────────────── */

func TestArticleRepo_Delete(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectExec("DELETE FROM articles").
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := pg.NewArticleRepo(db)
	if err := repo.Delete(context.Background(), 1); err != nil {
		t.Fatalf("Delete err=%v", err)
	}
}

/* ─────────────────────────── 7. ExistsByURL ─────────────────────────── */

func TestArticleRepo_ExistsByURL(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// PostgreSQLはSELECT EXISTSを使用し、常に1行返す（trueまたはfalse）
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS (SELECT 1 FROM articles WHERE url = $1)")).
		WithArgs("https://u").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	repo := pg.NewArticleRepo(db)
	ok, err := repo.ExistsByURL(context.Background(), "https://u")
	if err != nil || !ok {
		t.Fatalf("ExistsByURL err=%v ok=%v", err, ok)
	}
}

func TestArticleRepo_ExistsByURL_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// PostgreSQLはSELECT EXISTSを使用し、常に1行返す（falseの場合）
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS (SELECT 1 FROM articles WHERE url = $1)")).
		WithArgs("https://notfound").
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	repo := pg.NewArticleRepo(db)
	ok, err := repo.ExistsByURL(context.Background(), "https://notfound")
	if err != nil {
		t.Fatalf("ExistsByURL err=%v", err)
	}
	if ok {
		t.Fatalf("ExistsByURL want false, got true")
	}
}

/* ─────────────────────────── 8. ExistsByURLBatch ─────────────────────────── */

func TestArticleRepo_ExistsByURLBatch(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	urls := []string{
		"https://example.com/article1",
		"https://example.com/article2",
		"https://example.com/article3",
	}

	// article1とarticle3が存在する
	mock.ExpectQuery(regexp.QuoteMeta("SELECT url FROM articles WHERE url IN ($1, $2, $3)")).
		WithArgs("https://example.com/article1", "https://example.com/article2", "https://example.com/article3").
		WillReturnRows(sqlmock.NewRows([]string{"url"}).
			AddRow("https://example.com/article1").
			AddRow("https://example.com/article3"))

	repo := pg.NewArticleRepo(db)
	result, err := repo.ExistsByURLBatch(context.Background(), urls)
	if err != nil {
		t.Fatalf("ExistsByURLBatch err=%v", err)
	}

	// 結果を検証
	if len(result) != 2 {
		t.Fatalf("result length = %d, want 2", len(result))
	}
	if !result["https://example.com/article1"] {
		t.Errorf("article1 should exist")
	}
	if result["https://example.com/article2"] {
		t.Errorf("article2 should not exist")
	}
	if !result["https://example.com/article3"] {
		t.Errorf("article3 should exist")
	}
}

func TestArticleRepo_ExistsByURLBatch_Empty(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	repo := pg.NewArticleRepo(db)
	result, err := repo.ExistsByURLBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("ExistsByURLBatch err=%v", err)
	}

	// 空のURLリストは空の結果を返す
	if len(result) != 0 {
		t.Fatalf("result length = %d, want 0", len(result))
	}
}

func TestArticleRepo_ExistsByURLBatch_AllNew(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	urls := []string{
		"https://example.com/new1",
		"https://example.com/new2",
	}

	// すべて存在しない（空の結果）
	mock.ExpectQuery(regexp.QuoteMeta("SELECT url FROM articles WHERE url IN ($1, $2)")).
		WithArgs("https://example.com/new1", "https://example.com/new2").
		WillReturnRows(sqlmock.NewRows([]string{"url"}))

	repo := pg.NewArticleRepo(db)
	result, err := repo.ExistsByURLBatch(context.Background(), urls)
	if err != nil {
		t.Fatalf("ExistsByURLBatch err=%v", err)
	}

	// すべて存在しないので、結果は空
	if len(result) != 0 {
		t.Fatalf("result length = %d, want 0", len(result))
	}
}

func TestArticleRepo_ExistsByURLBatch_AllExist(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	urls := []string{
		"https://example.com/article1",
		"https://example.com/article2",
	}

	// すべて存在する
	mock.ExpectQuery(regexp.QuoteMeta("SELECT url FROM articles WHERE url IN ($1, $2)")).
		WithArgs("https://example.com/article1", "https://example.com/article2").
		WillReturnRows(sqlmock.NewRows([]string{"url"}).
			AddRow("https://example.com/article1").
			AddRow("https://example.com/article2"))

	repo := pg.NewArticleRepo(db)
	result, err := repo.ExistsByURLBatch(context.Background(), urls)
	if err != nil {
		t.Fatalf("ExistsByURLBatch err=%v", err)
	}

	// すべて存在する
	if len(result) != 2 {
		t.Fatalf("result length = %d, want 2", len(result))
	}
	if !result["https://example.com/article1"] {
		t.Errorf("article1 should exist")
	}
	if !result["https://example.com/article2"] {
		t.Errorf("article2 should exist")
	}
}

/* ─────────────────────────── 9. GetWithSource ─────────────────────────── */

func TestArticleRepo_GetWithSource_Success(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Date(2025, 7, 19, 0, 0, 0, 0, time.UTC)
	want := &entity.Article{
		ID:          1,
		SourceID:    2,
		Title:       "Go 1.24 released",
		URL:         "https://example.com",
		Summary:     "sum",
		PublishedAt: now,
		CreatedAt:   now,
	}
	wantSourceName := "Tech News"

	mock.ExpectQuery(regexp.QuoteMeta("SELECT a.id, a.source_id, a.title, a.url, a.summary, a.published_at, a.created_at, s.name AS source_name")).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}).AddRow(
			want.ID, want.SourceID, want.Title, want.URL,
			want.Summary, want.PublishedAt, want.CreatedAt, wantSourceName,
		))

	repo := pg.NewArticleRepo(db)
	got, sourceName, err := repo.GetWithSource(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetWithSource err=%v", err)
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(entity.Article{})); diff != "" {
		t.Fatalf("article mismatch (-want +got):\n%s", diff)
	}
	if sourceName != wantSourceName {
		t.Errorf("sourceName = %q, want %q", sourceName, wantSourceName)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestArticleRepo_GetWithSource_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT a.id")).
		WithArgs(int64(999)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}))

	repo := pg.NewArticleRepo(db)
	got, sourceName, err := repo.GetWithSource(context.Background(), 999)
	if err != nil {
		t.Fatalf("GetWithSource should not return error for not found, err=%v", err)
	}
	if got != nil {
		t.Errorf("GetWithSource should return nil article for not found, got=%v", got)
	}
	if sourceName != "" {
		t.Errorf("GetWithSource should return empty source name for not found, got=%q", sourceName)
	}
}

func TestArticleRepo_GetWithSource_DatabaseError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	dbError := errors.New("connection lost")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT a.id")).
		WithArgs(int64(1)).
		WillReturnError(dbError)

	repo := pg.NewArticleRepo(db)
	got, sourceName, err := repo.GetWithSource(context.Background(), 1)
	if err == nil {
		t.Fatalf("GetWithSource should return error for database error")
	}
	if got != nil {
		t.Errorf("GetWithSource should return nil article on error, got=%v", got)
	}
	if sourceName != "" {
		t.Errorf("GetWithSource should return empty source name on error, got=%q", sourceName)
	}
}

func TestArticleRepo_GetWithSource_JoinWithSourceName(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	tests := []struct {
		name           string
		articleID      int64
		sourceName     string
		wantSourceName string
	}{
		{
			name:           "source with simple name",
			articleID:      1,
			sourceName:     "TechCrunch",
			wantSourceName: "TechCrunch",
		},
		{
			name:           "source with space in name",
			articleID:      2,
			sourceName:     "Hacker News",
			wantSourceName: "Hacker News",
		},
		{
			name:           "source with special characters",
			articleID:      3,
			sourceName:     "Dev.to - Community",
			wantSourceName: "Dev.to - Community",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock.ExpectQuery(regexp.QuoteMeta("SELECT a.id")).
				WithArgs(tt.articleID).
				WillReturnRows(sqlmock.NewRows([]string{
					"id", "source_id", "title", "url",
					"summary", "published_at", "created_at", "source_name",
				}).AddRow(
					tt.articleID, int64(10), "Test Title", "https://example.com",
					"Test Summary", now, now, tt.sourceName,
				))

			repo := pg.NewArticleRepo(db)
			_, sourceName, err := repo.GetWithSource(context.Background(), tt.articleID)
			if err != nil {
				t.Fatalf("GetWithSource err=%v", err)
			}
			if sourceName != tt.wantSourceName {
				t.Errorf("sourceName = %q, want %q", sourceName, tt.wantSourceName)
			}
		})
	}
}

/* ─────────────────────────── 10. SearchWithFilters ─────────────────────────── */

func TestArticleRepo_SearchWithFilters_SingleKeyword(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectQuery("FROM articles").
		WithArgs("%Go%").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at",
		}).AddRow(
			int64(1), int64(2), "Go 1.24 released", "https://example.com",
			"New Go version", now, now,
		))

	repo := pg.NewArticleRepo(db)
	result, err := repo.SearchWithFilters(context.Background(), []string{"Go"}, repository.ArticleSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(result) != 1 {
		t.Fatalf("SearchWithFilters len=%d, want 1", len(result))
	}
}

func TestArticleRepo_SearchWithFilters_MultipleKeywords(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectQuery("FROM articles").
		WithArgs("%Go%", "%release%").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at",
		}).AddRow(
			int64(1), int64(2), "Go 1.24 released", "https://example.com",
			"New Go version", now, now,
		))

	repo := pg.NewArticleRepo(db)
	result, err := repo.SearchWithFilters(context.Background(), []string{"Go", "release"}, repository.ArticleSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(result) != 1 {
		t.Fatalf("SearchWithFilters len=%d, want 1", len(result))
	}
}

func TestArticleRepo_SearchWithFilters_WithSourceID(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	sourceID := int64(2)
	mock.ExpectQuery("FROM articles").
		WithArgs("%Go%", sourceID).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at",
		}).AddRow(
			int64(1), sourceID, "Go 1.24 released", "https://example.com",
			"New Go version", now, now,
		))

	repo := pg.NewArticleRepo(db)
	filters := repository.ArticleSearchFilters{SourceID: &sourceID}
	result, err := repo.SearchWithFilters(context.Background(), []string{"Go"}, filters)
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(result) != 1 {
		t.Fatalf("SearchWithFilters len=%d, want 1", len(result))
	}
}

func TestArticleRepo_SearchWithFilters_WithDateRange(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	from := now.AddDate(0, 0, -7)
	to := now
	mock.ExpectQuery("FROM articles").
		WithArgs("%Go%", from, to).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at",
		}).AddRow(
			int64(1), int64(2), "Go 1.24 released", "https://example.com",
			"New Go version", now, now,
		))

	repo := pg.NewArticleRepo(db)
	filters := repository.ArticleSearchFilters{From: &from, To: &to}
	result, err := repo.SearchWithFilters(context.Background(), []string{"Go"}, filters)
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(result) != 1 {
		t.Fatalf("SearchWithFilters len=%d, want 1", len(result))
	}
}

func TestArticleRepo_SearchWithFilters_AllFilters(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	sourceID := int64(2)
	from := now.AddDate(0, 0, -7)
	to := now
	mock.ExpectQuery("FROM articles").
		WithArgs("%Go%", "%release%", sourceID, from, to).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at",
		}).AddRow(
			int64(1), sourceID, "Go 1.24 released", "https://example.com",
			"New Go version", now, now,
		))

	repo := pg.NewArticleRepo(db)
	filters := repository.ArticleSearchFilters{
		SourceID: &sourceID,
		From:     &from,
		To:       &to,
	}
	result, err := repo.SearchWithFilters(context.Background(), []string{"Go", "release"}, filters)
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(result) != 1 {
		t.Fatalf("SearchWithFilters len=%d, want 1", len(result))
	}
}

func TestArticleRepo_SearchWithFilters_EmptyKeywords(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	repo := pg.NewArticleRepo(db)
	result, err := repo.SearchWithFilters(context.Background(), []string{}, repository.ArticleSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(result) != 0 {
		t.Fatalf("SearchWithFilters len=%d, want 0", len(result))
	}
}

func TestArticleRepo_SearchWithFilters_SpecialCharacters(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	// Special characters: %, _, \
	// These should be escaped by search.EscapeILIKE
	mock.ExpectQuery("FROM articles").
		WithArgs("%100\\%%", "%my\\_var%", "%path\\\\file%").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at",
		}).AddRow(
			int64(1), int64(2), "100% complete", "https://example.com",
			"my_var in path\\file", now, now,
		))

	repo := pg.NewArticleRepo(db)
	result, err := repo.SearchWithFilters(context.Background(), []string{"100%", "my_var", "path\\file"}, repository.ArticleSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(result) != 1 {
		t.Fatalf("SearchWithFilters len=%d, want 1", len(result))
	}
}

/* ──────────────────────────── ListWithSourcePaginated ──────────────────────────── */

func TestArticleRepo_ListWithSourcePaginated(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()

	// Mock data: 2 articles (PostgreSQL uses $1, $2 placeholders)
	mock.ExpectQuery("SELECT.*FROM articles.*INNER JOIN sources.*LIMIT.*OFFSET").
		WithArgs(2, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}).
			AddRow(1, 10, "Article 1", "https://example.com/1", "Summary 1", now, now, "Test Source").
			AddRow(2, 10, "Article 2", "https://example.com/2", "Summary 2", now, now, "Test Source"))

	repo := pg.NewArticleRepo(db)
	result, err := repo.ListWithSourcePaginated(context.Background(), 0, 2)
	if err != nil {
		t.Fatalf("ListWithSourcePaginated err=%v", err)
	}

	if len(result) != 2 {
		t.Fatalf("ListWithSourcePaginated result length = %d, want 2", len(result))
	}

	if result[0].Article.ID != 1 {
		t.Errorf("result[0].Article.ID = %d, want 1", result[0].Article.ID)
	}
	if result[0].SourceName != "Test Source" {
		t.Errorf("result[0].SourceName = %q, want %q", result[0].SourceName, "Test Source")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestArticleRepo_ListWithSourcePaginated_SecondPage(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()

	// Mock second page (offset=20, limit=20)
	mock.ExpectQuery("SELECT.*FROM articles.*INNER JOIN sources.*LIMIT.*OFFSET").
		WithArgs(20, 20).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}).
			AddRow(21, 10, "Article 21", "https://example.com/21", "Summary 21", now, now, "Test Source").
			AddRow(22, 10, "Article 22", "https://example.com/22", "Summary 22", now, now, "Test Source"))

	repo := pg.NewArticleRepo(db)
	result, err := repo.ListWithSourcePaginated(context.Background(), 20, 20)
	if err != nil {
		t.Fatalf("ListWithSourcePaginated err=%v", err)
	}

	if len(result) != 2 {
		t.Fatalf("ListWithSourcePaginated result length = %d, want 2", len(result))
	}

	if result[0].Article.ID != 21 {
		t.Errorf("result[0].Article.ID = %d, want 21", result[0].Article.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestArticleRepo_ListWithSourcePaginated_EmptyResult(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock empty result (page beyond available data)
	mock.ExpectQuery("SELECT.*FROM articles.*INNER JOIN sources.*LIMIT.*OFFSET").
		WithArgs(20, 1000).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}))

	repo := pg.NewArticleRepo(db)
	result, err := repo.ListWithSourcePaginated(context.Background(), 1000, 20)
	if err != nil {
		t.Fatalf("ListWithSourcePaginated err=%v", err)
	}

	if len(result) != 0 {
		t.Fatalf("ListWithSourcePaginated result length = %d, want 0", len(result))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestArticleRepo_ListWithSourcePaginated_LargeOffset(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock large offset
	mock.ExpectQuery("SELECT.*FROM articles.*INNER JOIN sources.*LIMIT.*OFFSET").
		WithArgs(10, 9900).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}))

	repo := pg.NewArticleRepo(db)
	result, err := repo.ListWithSourcePaginated(context.Background(), 9900, 10)
	if err != nil {
		t.Fatalf("ListWithSourcePaginated err=%v", err)
	}

	if len(result) != 0 {
		t.Fatalf("ListWithSourcePaginated result length = %d, want 0", len(result))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────── CountArticles ──────────────────────────── */

func TestArticleRepo_CountArticles(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock count result
	mock.ExpectQuery("SELECT COUNT.*FROM articles").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(150))

	repo := pg.NewArticleRepo(db)
	count, err := repo.CountArticles(context.Background())
	if err != nil {
		t.Fatalf("CountArticles err=%v", err)
	}

	if count != 150 {
		t.Fatalf("CountArticles count = %d, want 150", count)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestArticleRepo_CountArticles_Zero(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock zero count
	mock.ExpectQuery("SELECT COUNT.*FROM articles").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	repo := pg.NewArticleRepo(db)
	count, err := repo.CountArticles(context.Background())
	if err != nil {
		t.Fatalf("CountArticles err=%v", err)
	}

	if count != 0 {
		t.Fatalf("CountArticles count = %d, want 0", count)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────── Error Cases ──────────────────────────── */

func TestArticleRepo_Get_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id")).
		WithArgs(int64(999)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at",
		}))

	repo := pg.NewArticleRepo(db)
	got, err := repo.Get(context.Background(), 999)
	if err != nil {
		t.Fatalf("Get should not return error for not found, err=%v", err)
	}
	if got != nil {
		t.Fatalf("Get should return nil for not found, got=%v", got)
	}
}

func TestArticleRepo_Get_DatabaseError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	dbError := errors.New("database connection lost")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id")).
		WithArgs(int64(1)).
		WillReturnError(dbError)

	repo := pg.NewArticleRepo(db)
	got, err := repo.Get(context.Background(), 1)
	if err == nil {
		t.Fatal("Get should return error for database error")
	}
	if got != nil {
		t.Errorf("Get should return nil on error, got=%v", got)
	}
}

func TestArticleRepo_Update_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec("UPDATE articles").
		WithArgs(int64(2), "new", "https://u",
			"sum", now, int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := pg.NewArticleRepo(db)
	err := repo.Update(context.Background(), &entity.Article{
		ID: 999, SourceID: 2, Title: "new", URL: "https://u",
		Summary: "sum", PublishedAt: now,
	})
	if err == nil {
		t.Fatal("Update should fail when no rows affected")
	}
	if !strings.Contains(err.Error(), "no rows affected") {
		t.Fatalf("Update error should mention 'no rows affected', got: %v", err)
	}
}

func TestArticleRepo_Delete_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectExec("DELETE FROM articles").
		WithArgs(int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := pg.NewArticleRepo(db)
	err := repo.Delete(context.Background(), 999)
	if err == nil {
		t.Fatal("Delete should fail when no rows affected")
	}
	if !strings.Contains(err.Error(), "no rows affected") {
		t.Fatalf("Delete error should mention 'no rows affected', got: %v", err)
	}
}

func TestArticleRepo_List_ScanError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock invalid data type for ID (string instead of int64)
	mock.ExpectQuery("FROM articles").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at",
		}).AddRow("invalid", 2, "title", "url", "summary", time.Now(), time.Now()))

	repo := pg.NewArticleRepo(db)
	got, err := repo.List(context.Background())
	if err == nil {
		t.Fatal("List should return error for scan error")
	}
	if got != nil {
		t.Errorf("List should return nil on scan error, got=%v", got)
	}
}

func TestArticleRepo_ListWithSource_ScanError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock invalid data type
	mock.ExpectQuery("FROM articles").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}).AddRow("invalid", 2, "title", "url", "summary", time.Now(), time.Now(), "source"))

	repo := pg.NewArticleRepo(db)
	got, err := repo.ListWithSource(context.Background())
	if err == nil {
		t.Fatal("ListWithSource should return error for scan error")
	}
	if got != nil {
		t.Errorf("ListWithSource should return nil on scan error, got=%v", got)
	}
}

func TestArticleRepo_ListWithSourcePaginated_ScanError(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock invalid data type
	mock.ExpectQuery("SELECT.*FROM articles.*INNER JOIN sources.*LIMIT.*OFFSET").
		WithArgs(10, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}).AddRow("invalid", 2, "title", "url", "summary", time.Now(), time.Now(), "source"))

	repo := pg.NewArticleRepo(db)
	got, err := repo.ListWithSourcePaginated(context.Background(), 0, 10)
	if err == nil {
		t.Fatal("ListWithSourcePaginated should return error for scan error")
	}
	if got != nil {
		t.Errorf("ListWithSourcePaginated should return nil on scan error, got=%v", got)
	}
}

func TestArticleRepo_CountArticles_DatabaseError(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	dbError := errors.New("connection lost")
	mock.ExpectQuery("SELECT COUNT.*FROM articles").
		WillReturnError(dbError)

	repo := pg.NewArticleRepo(db)
	count, err := repo.CountArticles(context.Background())
	if err == nil {
		t.Fatal("CountArticles should return error for database error")
	}
	if count != 0 {
		t.Errorf("CountArticles should return 0 on error, got=%d", count)
	}
}

func TestArticleRepo_Create_DatabaseError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	dbError := errors.New("unique constraint violation")
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO articles")).
		WithArgs(int64(2), "title", "https://u",
			"summary", now, now).
		WillReturnError(dbError)

	repo := pg.NewArticleRepo(db)
	err := repo.Create(context.Background(), &entity.Article{
		SourceID: 2, Title: "title", URL: "https://u",
		Summary: "summary", PublishedAt: now, CreatedAt: now,
	})
	if err == nil {
		t.Fatal("Create should return error for database error")
	}
}

func TestArticleRepo_ExistsByURL_DatabaseError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	dbError := errors.New("connection lost")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS")).
		WithArgs("https://u").
		WillReturnError(dbError)

	repo := pg.NewArticleRepo(db)
	ok, err := repo.ExistsByURL(context.Background(), "https://u")
	if err == nil {
		t.Fatal("ExistsByURL should return error for database error")
	}
	if ok {
		t.Errorf("ExistsByURL should return false on error, got=%v", ok)
	}
}

func TestArticleRepo_ExistsByURLBatch_DatabaseError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	urls := []string{"https://example.com/1"}

	// Mock database error
	dbError := errors.New("connection lost")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT url FROM articles WHERE url IN ($1)")).
		WithArgs("https://example.com/1").
		WillReturnError(dbError)

	repo := pg.NewArticleRepo(db)
	result, err := repo.ExistsByURLBatch(context.Background(), urls)
	if err == nil {
		t.Fatal("ExistsByURLBatch should return error for database error")
	}
	if result != nil {
		t.Errorf("ExistsByURLBatch should return nil on error, got=%v", result)
	}
}

/* ──────────────────────────── CountArticlesWithFilters Tests ──────────────────────────── */

func TestArticleRepo_CountArticlesWithFilters_NoFilters(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	repo := pg.NewArticleRepo(db)
	count, err := repo.CountArticlesWithFilters(context.Background(), []string{}, repository.ArticleSearchFilters{})
	if err != nil {
		t.Fatalf("CountArticlesWithFilters err=%v", err)
	}
	if count != 0 {
		t.Fatalf("CountArticlesWithFilters count = %d, want 0", count)
	}
}

func TestArticleRepo_CountArticlesWithFilters_WithKeywords(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("SELECT COUNT.*FROM articles").
		WithArgs("%Go%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	repo := pg.NewArticleRepo(db)
	count, err := repo.CountArticlesWithFilters(context.Background(), []string{"Go"}, repository.ArticleSearchFilters{})
	if err != nil {
		t.Fatalf("CountArticlesWithFilters err=%v", err)
	}
	if count != 42 {
		t.Fatalf("CountArticlesWithFilters count = %d, want 42", count)
	}
}

func TestArticleRepo_CountArticlesWithFilters_WithSourceID(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	sourceID := int64(2)
	mock.ExpectQuery("SELECT COUNT.*FROM articles").
		WithArgs("%Go%", sourceID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(10))

	repo := pg.NewArticleRepo(db)
	filters := repository.ArticleSearchFilters{SourceID: &sourceID}
	count, err := repo.CountArticlesWithFilters(context.Background(), []string{"Go"}, filters)
	if err != nil {
		t.Fatalf("CountArticlesWithFilters err=%v", err)
	}
	if count != 10 {
		t.Fatalf("CountArticlesWithFilters count = %d, want 10", count)
	}
}

/* ──────────────────────────── SearchWithFiltersPaginated Tests ──────────────────────────── */

func TestArticleRepo_SearchWithFiltersPaginated_NoFilters(t *testing.T) {
	db, _, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	repo := pg.NewArticleRepo(db)
	result, err := repo.SearchWithFiltersPaginated(context.Background(), []string{}, repository.ArticleSearchFilters{}, 0, 10)
	if err != nil {
		t.Fatalf("SearchWithFiltersPaginated err=%v", err)
	}
	if len(result) != 0 {
		t.Fatalf("SearchWithFiltersPaginated len=%d, want 0", len(result))
	}
}

func TestArticleRepo_SearchWithFiltersPaginated_WithKeywords(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectQuery("FROM articles").
		WithArgs("%Go%", 10, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}).AddRow(
			int64(1), int64(2), "Go 1.24", "https://example.com",
			"New version", now, now, "Tech News",
		))

	repo := pg.NewArticleRepo(db)
	result, err := repo.SearchWithFiltersPaginated(context.Background(), []string{"Go"}, repository.ArticleSearchFilters{}, 0, 10)
	if err != nil {
		t.Fatalf("SearchWithFiltersPaginated err=%v", err)
	}
	if len(result) != 1 {
		t.Fatalf("SearchWithFiltersPaginated len=%d, want 1", len(result))
	}
	if result[0].SourceName != "Tech News" {
		t.Errorf("SourceName = %q, want %q", result[0].SourceName, "Tech News")
	}
}

func TestArticleRepo_SearchWithFiltersPaginated_ScanError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock invalid data type
	mock.ExpectQuery("FROM articles").
		WithArgs("%Go%", 10, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}).AddRow("invalid", 2, "title", "url", "summary", time.Now(), time.Now(), "source"))

	repo := pg.NewArticleRepo(db)
	result, err := repo.SearchWithFiltersPaginated(context.Background(), []string{"Go"}, repository.ArticleSearchFilters{}, 0, 10)
	if err == nil {
		t.Fatal("SearchWithFiltersPaginated should return error for scan error")
	}
	if result != nil {
		t.Errorf("SearchWithFiltersPaginated should return nil on error, got=%v", result)
	}
}
