package sqlite_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/go-cmp/cmp"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/infra/adapter/persistence/sqlite"
)

/* ────────────────────────────  ヘルパ  ──────────────────────────── */

func artRow(a *entity.Article) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "source_id", "title", "url",
		"summary", "published_at", "created_at",
	}).AddRow(
		a.ID, a.SourceID, a.Title, a.URL,
		a.Summary, a.PublishedAt, a.CreatedAt,
	)
}

/* ──────────────────────────── 1. Get ──────────────────────────── */

func TestArticleRepo_Get(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Date(2025, 7, 19, 0, 0, 0, 0, time.UTC)
	want := &entity.Article{
		ID: 1, SourceID: 2, Title: "Go 1.22 released",
		URL:     "https://example.com",
		Summary: "summary", PublishedAt: now, CreatedAt: now,
	}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT")).
		WithArgs(int64(1)).
		WillReturnRows(artRow(want))

	repo := sqlite.NewArticleRepo(db)
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("Get err=%v", err)
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(entity.Article{})); diff != "" {
		t.Fatalf("Get mismatch (-want +got):\n%s", diff)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────── 2. List ──────────────────────────── */

func TestArticleRepo_List(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectQuery("SELECT.*FROM articles").
		WillReturnRows(artRow(&entity.Article{
			ID: 1, SourceID: 2, Title: "x", URL: "y",
			Summary: "s", PublishedAt: now, CreatedAt: now,
		}))

	repo := sqlite.NewArticleRepo(db)
	arts, err := repo.List(context.Background())
	if err != nil || len(arts) != 1 {
		t.Fatalf("List err=%v len=%d", err, len(arts))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────── 3. Search ──────────────────────────── */

func TestArticleRepo_Search(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("FROM articles").
		WithArgs("%go%", "%go%").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at",
		})) // 空集合で十分

	repo := sqlite.NewArticleRepo(db)
	_, err := repo.Search(context.Background(), "go")
	if err != nil {
		t.Fatalf("Search err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────── 4. Create ──────────────────────────── */

func TestArticleRepo_Create(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO articles")).
		WithArgs(int64(2), "title", "https://u", "summary",
			now, now).
		WillReturnResult(sqlmock.NewResult(1, 1))

	repo := sqlite.NewArticleRepo(db)
	err := repo.Create(context.Background(), &entity.Article{
		SourceID: 2, Title: "title", URL: "https://u",
		Summary: "summary", PublishedAt: now, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("Create err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────── 5. Update ──────────────────────────── */

func TestArticleRepo_Update(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()

	mock.ExpectExec("UPDATE articles").
		WithArgs(int64(2), "new", "https://u", "sum", now, 1).
		WillReturnResult(sqlmock.NewResult(0, 1)) // 1 行更新

	repo := sqlite.NewArticleRepo(db)
	err := repo.Update(context.Background(), &entity.Article{
		ID: 1, SourceID: 2, Title: "new", URL: "https://u",
		Summary: "sum", PublishedAt: now,
	})
	if err != nil {
		t.Fatalf("Update err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────── 6. Delete ──────────────────────────── */

func TestArticleRepo_Delete(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectExec("DELETE FROM articles").
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := sqlite.NewArticleRepo(db)
	err := repo.Delete(context.Background(), 1)
	if err != nil {
		t.Fatalf("Delete err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────── 7. ExistsByURL ──────────────────────────── */

func TestArticleRepo_ExistsByURL(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// 存在する場合
	mock.ExpectQuery("SELECT 1 FROM articles").
		WithArgs("https://example.com").
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))

	repo := sqlite.NewArticleRepo(db)
	ok, err := repo.ExistsByURL(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("ExistsByURL err=%v", err)
	}
	if !ok {
		t.Fatalf("ExistsByURL want true, got false")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestArticleRepo_ExistsByURL_NotFound(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// 存在しない場合
	mock.ExpectQuery("SELECT 1 FROM articles").
		WithArgs("https://example.com/notfound").
		WillReturnRows(sqlmock.NewRows([]string{"1"}))

	repo := sqlite.NewArticleRepo(db)
	ok, err := repo.ExistsByURL(context.Background(), "https://example.com/notfound")
	if err != nil {
		t.Fatalf("ExistsByURL err=%v", err)
	}
	if ok {
		t.Fatalf("ExistsByURL want false, got true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────── 8. ExistsByURLBatch ──────────────────────────── */

func TestArticleRepo_ExistsByURLBatch(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	urls := []string{
		"https://example.com/article1",
		"https://example.com/article2",
		"https://example.com/article3",
	}

	// article1とarticle3が存在する
	mock.ExpectQuery(regexp.QuoteMeta("SELECT url FROM articles WHERE url IN (?,?,?)")).
		WithArgs("https://example.com/article1", "https://example.com/article2", "https://example.com/article3").
		WillReturnRows(sqlmock.NewRows([]string{"url"}).
			AddRow("https://example.com/article1").
			AddRow("https://example.com/article3"))

	repo := sqlite.NewArticleRepo(db)
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

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestArticleRepo_ExistsByURLBatch_Empty(t *testing.T) {
	t.Parallel()

	db, _, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	repo := sqlite.NewArticleRepo(db)
	result, err := repo.ExistsByURLBatch(context.Background(), []string{})
	if err != nil {
		t.Fatalf("ExistsByURLBatch err=%v", err)
	}

	// 空のURLリストは空の結果を返す
	if len(result) != 0 {
		t.Fatalf("result length = %d, want 0", len(result))
	}
}

func TestArticleRepo_ExistsByURLBatch_TooManyURLs(t *testing.T) {
	t.Parallel()

	db, _, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// SQLiteの上限999を超えるURLリスト
	urls := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		urls[i] = "https://example.com/article"
	}

	repo := sqlite.NewArticleRepo(db)
	_, err := repo.ExistsByURLBatch(context.Background(), urls)
	if err == nil {
		t.Fatal("ExistsByURLBatch should return error for > 999 URLs")
	}

	// エラーメッセージを検証
	expectedMsg := "ExistsByURLBatch: too many URLs (1000 > 999)"
	if err.Error() != expectedMsg {
		t.Errorf("error message = %q, want %q", err.Error(), expectedMsg)
	}
}

/* ──────────────────────────── 9. ListWithSourcePaginated ──────────────────────────── */

func TestArticleRepo_ListWithSourcePaginated(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()

	// Mock data: 2 articles
	mock.ExpectQuery("SELECT.*FROM articles.*INNER JOIN sources.*LIMIT.*OFFSET").
		WithArgs(2, 0).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "source_id", "title", "url",
			"summary", "published_at", "created_at", "source_name",
		}).
			AddRow(1, 10, "Article 1", "https://example.com/1", "Summary 1", now, now, "Test Source").
			AddRow(2, 10, "Article 2", "https://example.com/2", "Summary 2", now, now, "Test Source"))

	repo := sqlite.NewArticleRepo(db)
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

	repo := sqlite.NewArticleRepo(db)
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

	repo := sqlite.NewArticleRepo(db)
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

	repo := sqlite.NewArticleRepo(db)
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

/* ──────────────────────────── 10. CountArticles ──────────────────────────── */

func TestArticleRepo_CountArticles(t *testing.T) {
	t.Parallel()

	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock count result
	mock.ExpectQuery("SELECT COUNT.*FROM articles").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(150))

	repo := sqlite.NewArticleRepo(db)
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

	repo := sqlite.NewArticleRepo(db)
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
