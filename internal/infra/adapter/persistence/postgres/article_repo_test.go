package postgres_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/go-cmp/cmp"

	"catchup-feed/internal/domain/entity"
	pg "catchup-feed/internal/infra/adapter/persistence/postgres"
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
	mock.ExpectQuery(regexp.QuoteMeta("SELECT url FROM articles WHERE url = ANY($1)")).
		WithArgs(sqlmock.AnyArg()).
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
	mock.ExpectQuery(regexp.QuoteMeta("SELECT url FROM articles WHERE url = ANY($1)")).
		WithArgs(sqlmock.AnyArg()).
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
	mock.ExpectQuery(regexp.QuoteMeta("SELECT url FROM articles WHERE url = ANY($1)")).
		WithArgs(sqlmock.AnyArg()).
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
