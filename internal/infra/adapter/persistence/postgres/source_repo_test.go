package postgres_test

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/go-cmp/cmp"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/repository"
)

/* ──────────────────────────────── ヘルパ ──────────────────────────────── */

func row(src *entity.Source) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "feed_url",
		"last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(
		src.ID, src.Name, src.FeedURL,
		src.LastCrawledAt, src.Active,
		src.SourceType, nil,
	)
}

/* ──────────────────────────────── 1. Get ──────────────────────────────── */

func TestSourceRepo_Get(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	want := &entity.Source{
		ID: 1, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &[]time.Time{time.Now()}[0], Active: true,
		SourceType: "RSS",
	}

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id`)).
		WithArgs(int64(1)).
		WillReturnRows(row(want))

	repo := postgres.NewSourceRepo(db)
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("Get err=%v", err)
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(entity.Source{})); diff != "" {
		t.Fatalf("mismatch (-want +got):\n%s", diff)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 2. List ──────────────────────────────── */

func TestSourceRepo_List(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectQuery(`FROM sources`).
		WillReturnRows(row(&entity.Source{
			ID: 1, Name: "Qiita", FeedURL: "https://qiita.com/feed",
			LastCrawledAt: &now, Active: true,
			SourceType: "RSS",
		}))

	repo := postgres.NewSourceRepo(db)
	got, err := repo.List(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("List err=%v len=%d", err, len(got))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 3. Search ──────────────────────────────── */

func TestSourceRepo_Search(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(`FROM sources`).
		WithArgs("%go%").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "feed_url", "last_crawled_at", "active",
			"source_type", "scraper_config",
		})) // empty set OK

	repo := postgres.NewSourceRepo(db)
	if _, err := repo.Search(context.Background(), "go"); err != nil {
		t.Fatalf("Search err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 4. Create ──────────────────────────────── */

func TestSourceRepo_Create(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO sources`)).
		WithArgs("Qiita", "https://qiita.com/feed",
			&now, true, "RSS", []byte(nil)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	repo := postgres.NewSourceRepo(db)
	err := repo.Create(context.Background(), &entity.Source{
		Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &now, Active: true,
		SourceType: "RSS",
	})
	if err != nil {
		t.Fatalf("Create err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 5. Update ──────────────────────────────── */

func TestSourceRepo_Update(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec(`UPDATE sources`).
		WithArgs("Qiita", "https://qiita.com/feed",
			&now, true, "RSS", []byte(nil), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := postgres.NewSourceRepo(db)
	err := repo.Update(context.Background(), &entity.Source{
		ID: 1, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &now, Active: true,
		SourceType: "RSS",
	})
	if err != nil {
		t.Fatalf("Update err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 6. Delete ──────────────────────────────── */

func TestSourceRepo_Delete(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`DELETE FROM sources`).
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := postgres.NewSourceRepo(db)
	if err := repo.Delete(context.Background(), 1); err != nil {
		t.Fatalf("Delete err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 7. ListActive ──────────────────────────────── */

func TestSourceRepo_ListActive(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).
		AddRow(1, "Qiita", "https://qiita.com/feed", now, true, "RSS", nil).
		AddRow(2, "Zenn", "https://zenn.dev/feed", now, true, "RSS", nil)

	mock.ExpectQuery(`FROM sources`).
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive err=%v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("ListActive expected 2 sources, got %d", len(sources))
	}
	if !sources[0].Active || !sources[1].Active {
		t.Fatal("ListActive returned inactive sources")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRepo_ListActive_Empty(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	})

	mock.ExpectQuery(`FROM sources`).
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive err=%v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("ListActive expected 0 sources, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 8. TouchCrawledAt ──────────────────────────────── */

func TestSourceRepo_TouchCrawledAt(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec(`UPDATE sources SET last_crawled_at`).
		WithArgs(now, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := postgres.NewSourceRepo(db)
	err := repo.TouchCrawledAt(context.Background(), 1, now)
	if err != nil {
		t.Fatalf("TouchCrawledAt err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRepo_TouchCrawledAt_NonExistent(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec(`UPDATE sources SET last_crawled_at`).
		WithArgs(now, int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := postgres.NewSourceRepo(db)
	// TouchCrawledAt doesn't check rows affected, so it should succeed
	err := repo.TouchCrawledAt(context.Background(), 999, now)
	if err != nil {
		t.Fatalf("TouchCrawledAt err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 9. SearchWithFilters ──────────────────────────────── */

func TestSourceRepo_SearchWithFilters_SingleKeyword(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(1, "Go Blog", "https://go.dev/blog/feed", &now, true, "RSS", nil)

	mock.ExpectQuery(`FROM sources`).
		WithArgs("%Go%"). // EscapeILIKE wraps with %
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{"Go"}, repository.SourceSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Name != "Go Blog" {
		t.Fatalf("expected Go Blog, got %s", sources[0].Name)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRepo_SearchWithFilters_MultipleKeywords(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(1, "Go Blog", "https://go.dev/blog/feed", &now, true, "RSS", nil)

	// Multiple keywords with AND logic
	mock.ExpectQuery(`FROM sources`).
		WithArgs("%Go%", "%blog%").
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{"Go", "blog"}, repository.SourceSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRepo_SearchWithFilters_WithSourceTypeFilter(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(2, "Webflow Blog", "https://webflow.com/blog", &now, true, "Webflow", nil)

	sourceType := "Webflow"
	filters := repository.SourceSearchFilters{
		SourceType: &sourceType,
	}

	mock.ExpectQuery(`FROM sources`).
		WithArgs("%blog%", "Webflow").
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{"blog"}, filters)
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].SourceType != "Webflow" {
		t.Fatalf("expected Webflow, got %s", sources[0].SourceType)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRepo_SearchWithFilters_WithActiveFilter(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(1, "Active Blog", "https://active.com/feed", &now, true, "RSS", nil)

	active := true
	filters := repository.SourceSearchFilters{
		Active: &active,
	}

	mock.ExpectQuery(`FROM sources`).
		WithArgs("%blog%", true).
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{"blog"}, filters)
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if !sources[0].Active {
		t.Fatal("expected active source")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRepo_SearchWithFilters_WithAllFilters(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(1, "Go Blog RSS", "https://go.dev/feed", &now, true, "RSS", nil)

	sourceType := "RSS"
	active := true
	filters := repository.SourceSearchFilters{
		SourceType: &sourceType,
		Active:     &active,
	}

	mock.ExpectQuery(`FROM sources`).
		WithArgs("%Go%", "%blog%", "RSS", true).
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{"Go", "blog"}, filters)
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestSourceRepo_SearchWithFilters_EmptyKeywords is now deprecated.
// The feature now supports filter-only searches (empty keywords with no filters returns all sources).
// See new tests: TestSourceRepo_SearchWithFilters_EmptyKeywords_NoFilters, etc.
func TestSourceRepo_SearchWithFilters_EmptyKeywords(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Updated behavior: empty keywords now executes query and returns all sources
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}) // Empty result set for this test

	mock.ExpectQuery(`FROM sources`).
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{}, repository.SourceSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("expected 0 sources, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRepo_SearchWithFilters_SpecialCharacters(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(1, "100% Go", "https://go100.com/feed", &now, true, "RSS", nil)

	// EscapeILIKE should escape % as \%
	mock.ExpectQuery(`FROM sources`).
		WithArgs("%100\\%%"). // 100% -> %100\%%
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{"100%"}, repository.SourceSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRepo_SearchWithFilters_UnderscoreEscape(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(1, "my_blog", "https://myblog.com/feed", &now, true, "RSS", nil)

	// EscapeILIKE should escape _ as \_
	mock.ExpectQuery(`FROM sources`).
		WithArgs("%my\\_blog%"). // my_blog -> %my\_blog%
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{"my_blog"}, repository.SourceSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRepo_SearchWithFilters_BackslashEscape(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(1, "path\\file", "https://example.com/feed", &now, true, "RSS", nil)

	// EscapeILIKE should escape \ as \\
	mock.ExpectQuery(`FROM sources`).
		WithArgs("%path\\\\file%"). // path\file -> %path\\file%
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{"path\\file"}, repository.SourceSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 10. Error Cases ──────────────────────────────── */

func TestSourceRepo_Update_NoRowsAffected(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec(`UPDATE sources`).
		WithArgs("Qiita", "https://qiita.com/feed",
			&now, true, "RSS", []byte(nil), int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := postgres.NewSourceRepo(db)
	err := repo.Update(context.Background(), &entity.Source{
		ID: 999, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &now, Active: true,
		SourceType: "RSS",
	})
	if err == nil {
		t.Fatal("Update should fail when no rows affected")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRepo_Delete_NoRowsAffected(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`DELETE FROM sources`).
		WithArgs(int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := postgres.NewSourceRepo(db)
	err := repo.Delete(context.Background(), 999)
	if err == nil {
		t.Fatal("Delete should fail when no rows affected")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 11. Filter-Only Search Tests (TASK-005) ──────────────────────────────── */

// TestSourceRepo_SearchWithFilters_EmptyKeywords_NoFilters verifies empty keywords with no filters returns all sources
func TestSourceRepo_SearchWithFilters_EmptyKeywords_NoFilters(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).
		AddRow(1, "Tech Blog", "https://example.com/feed", &now, true, "RSS", nil).
		AddRow(2, "News Site", "https://news.example.com/feed", &now, false, "Webflow", nil)

	// No WHERE clause - returns all sources
	mock.ExpectQuery(`FROM sources`).
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{}, repository.SourceSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestSourceRepo_SearchWithFilters_EmptyKeywords_SourceTypeFilter verifies empty keywords with source_type filter
func TestSourceRepo_SearchWithFilters_EmptyKeywords_SourceTypeFilter(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(1, "RSS Blog", "https://example.com/feed", &now, true, "RSS", nil)

	sourceType := "RSS"
	filters := repository.SourceSearchFilters{
		SourceType: &sourceType,
	}

	// Only source_type filter in WHERE clause
	mock.ExpectQuery(`FROM sources`).
		WithArgs("RSS").
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{}, filters)
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].SourceType != "RSS" {
		t.Fatalf("expected RSS, got %s", sources[0].SourceType)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestSourceRepo_SearchWithFilters_EmptyKeywords_ActiveFilter verifies empty keywords with active filter
func TestSourceRepo_SearchWithFilters_EmptyKeywords_ActiveFilter(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).
		AddRow(1, "Active Blog", "https://example.com/feed", &now, true, "RSS", nil).
		AddRow(2, "Another Active", "https://example2.com/feed", &now, true, "Webflow", nil)

	active := true
	filters := repository.SourceSearchFilters{
		Active: &active,
	}

	// Only active filter in WHERE clause
	mock.ExpectQuery(`FROM sources`).
		WithArgs(true).
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{}, filters)
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	for _, src := range sources {
		if !src.Active {
			t.Fatal("expected all sources to be active")
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestSourceRepo_SearchWithFilters_EmptyKeywords_MultipleFilters verifies empty keywords with multiple filters
func TestSourceRepo_SearchWithFilters_EmptyKeywords_MultipleFilters(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}).AddRow(1, "Active RSS", "https://example.com/feed", &now, true, "RSS", nil)

	sourceType := "RSS"
	active := true
	filters := repository.SourceSearchFilters{
		SourceType: &sourceType,
		Active:     &active,
	}

	// Both filters in WHERE clause
	mock.ExpectQuery(`FROM sources`).
		WithArgs("RSS", true).
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{}, filters)
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].SourceType != "RSS" {
		t.Fatalf("expected RSS, got %s", sources[0].SourceType)
	}
	if !sources[0].Active {
		t.Fatal("expected source to be active")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestSourceRepo_SearchWithFilters_EmptyKeywords_EmptyResult verifies empty result returns empty slice (not nil)
func TestSourceRepo_SearchWithFilters_EmptyKeywords_EmptyResult(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
		"source_type", "scraper_config",
	}) // No rows

	sourceType := "NonExistent"
	filters := repository.SourceSearchFilters{
		SourceType: &sourceType,
	}

	mock.ExpectQuery(`FROM sources`).
		WithArgs("NonExistent").
		WillReturnRows(rows)

	repo := postgres.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{}, filters)
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if sources == nil {
		t.Fatal("expected empty slice, got nil")
	}
	if len(sources) != 0 {
		t.Fatalf("expected 0 sources, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

/* ──────────────────────────────── 12. Additional Error Cases ──────────────────────────────── */

func TestSourceRepo_Get_NotFound(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id`)).
		WithArgs(int64(999)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "feed_url", "last_crawled_at", "active",
			"source_type", "scraper_config",
		}))

	repo := postgres.NewSourceRepo(db)
	got, err := repo.Get(context.Background(), 999)
	if err != nil {
		t.Fatalf("Get should not return error for not found, err=%v", err)
	}
	if got != nil {
		t.Fatalf("Get should return nil for not found, got=%v", got)
	}
}

func TestSourceRepo_Get_DatabaseError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	dbError := errors.New("connection lost")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id`)).
		WithArgs(int64(1)).
		WillReturnError(dbError)

	repo := postgres.NewSourceRepo(db)
	got, err := repo.Get(context.Background(), 1)
	if err == nil {
		t.Fatal("Get should return error for database error")
	}
	if got != nil {
		t.Errorf("Get should return nil on error, got=%v", got)
	}
}

func TestSourceRepo_Get_WithScraperConfig(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	scraperConfigJSON := []byte(`{"item_selector":"article","url_prefix":"https://example.com"}`)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id`)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "feed_url", "last_crawled_at", "active",
			"source_type", "scraper_config",
		}).AddRow(1, "Test Source", "https://example.com/feed", &now, true, "Webflow", scraperConfigJSON))

	repo := postgres.NewSourceRepo(db)
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("Get err=%v", err)
	}
	if got == nil {
		t.Fatal("Get should return source")
	}
	if got.ScraperConfig == nil {
		t.Fatal("ScraperConfig should not be nil")
	}
	if got.ScraperConfig.ItemSelector != "article" {
		t.Errorf("ItemSelector = %q, want %q", got.ScraperConfig.ItemSelector, "article")
	}
}

func TestSourceRepo_Get_InvalidScraperConfigJSON(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	invalidJSON := []byte(`{"item_selector":invalid}`) // Invalid JSON

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id`)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "feed_url", "last_crawled_at", "active",
			"source_type", "scraper_config",
		}).AddRow(1, "Test", "https://example.com", &now, true, "RSS", invalidJSON))

	repo := postgres.NewSourceRepo(db)
	got, err := repo.Get(context.Background(), 1)
	if err == nil {
		t.Fatal("Get should return error for invalid JSON")
	}
	if got != nil {
		t.Errorf("Get should return nil on JSON unmarshal error, got=%v", got)
	}
}

func TestSourceRepo_List_ScanError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock invalid data type for ID
	mock.ExpectQuery(`FROM sources`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "feed_url", "last_crawled_at", "active",
			"source_type", "scraper_config",
		}).AddRow("invalid", "name", "url", nil, true, "RSS", nil))

	repo := postgres.NewSourceRepo(db)
	got, err := repo.List(context.Background())
	if err == nil {
		t.Fatal("List should return error for scan error")
	}
	if got != nil {
		t.Errorf("List should return nil on error, got=%v", got)
	}
}

func TestSourceRepo_ListActive_ScanError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock invalid data type
	mock.ExpectQuery(`FROM sources`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "feed_url", "last_crawled_at", "active",
			"source_type", "scraper_config",
		}).AddRow("invalid", "name", "url", nil, true, "RSS", nil))

	repo := postgres.NewSourceRepo(db)
	got, err := repo.ListActive(context.Background())
	if err == nil {
		t.Fatal("ListActive should return error for scan error")
	}
	if got != nil {
		t.Errorf("ListActive should return nil on error, got=%v", got)
	}
}

func TestSourceRepo_Search_ScanError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock invalid data type
	mock.ExpectQuery(`FROM sources`).
		WithArgs("%go%").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "feed_url", "last_crawled_at", "active",
			"source_type", "scraper_config",
		}).AddRow("invalid", "name", "url", nil, true, "RSS", nil))

	repo := postgres.NewSourceRepo(db)
	got, err := repo.Search(context.Background(), "go")
	if err == nil {
		t.Fatal("Search should return error for scan error")
	}
	if got != nil {
		t.Errorf("Search should return nil on error, got=%v", got)
	}
}

func TestSourceRepo_SearchWithFilters_ScanError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// Mock invalid data type
	mock.ExpectQuery(`FROM sources`).
		WithArgs("%go%").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "feed_url", "last_crawled_at", "active",
			"source_type", "scraper_config",
		}).AddRow("invalid", "name", "url", nil, true, "RSS", nil))

	repo := postgres.NewSourceRepo(db)
	got, err := repo.SearchWithFilters(context.Background(), []string{"go"}, repository.SourceSearchFilters{})
	if err == nil {
		t.Fatal("SearchWithFilters should return error for scan error")
	}
	if got != nil {
		t.Errorf("SearchWithFilters should return nil on error, got=%v", got)
	}
}

func TestSourceRepo_Create_DatabaseError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	dbError := errors.New("unique constraint violation")
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO sources`)).
		WithArgs("Qiita", "https://qiita.com/feed",
			&now, true, "RSS", []byte(nil)).
		WillReturnError(dbError)

	repo := postgres.NewSourceRepo(db)
	err := repo.Create(context.Background(), &entity.Source{
		Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &now, Active: true,
		SourceType: "RSS",
	})
	if err == nil {
		t.Fatal("Create should return error for database error")
	}
}

func TestSourceRepo_Create_WithScraperConfig(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	scraperConfig := &entity.ScraperConfig{
		ItemSelector: "article",
		URLPrefix:    "https://example.com",
	}
	expectedJSON := []byte(`{"item_selector":"article","url_prefix":"https://example.com"}`)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO sources`)).
		WithArgs("Webflow", "https://webflow.com/blog",
			&now, true, "Webflow", expectedJSON).
		WillReturnResult(sqlmock.NewResult(1, 1))

	repo := postgres.NewSourceRepo(db)
	err := repo.Create(context.Background(), &entity.Source{
		Name:           "Webflow",
		FeedURL:        "https://webflow.com/blog",
		LastCrawledAt:  &now,
		Active:         true,
		SourceType:     "Webflow",
		ScraperConfig:  scraperConfig,
	})
	if err != nil {
		t.Fatalf("Create err=%v", err)
	}
}

func TestSourceRepo_Update_DatabaseError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	dbError := errors.New("constraint violation")
	mock.ExpectExec(`UPDATE sources`).
		WithArgs("Qiita", "https://qiita.com/feed",
			&now, true, "RSS", []byte(nil), int64(1)).
		WillReturnError(dbError)

	repo := postgres.NewSourceRepo(db)
	err := repo.Update(context.Background(), &entity.Source{
		ID: 1, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &now, Active: true,
		SourceType: "RSS",
	})
	if err == nil {
		t.Fatal("Update should return error for database error")
	}
}

func TestSourceRepo_Delete_DatabaseError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	dbError := errors.New("foreign key constraint")
	mock.ExpectExec(`DELETE FROM sources`).
		WithArgs(int64(1)).
		WillReturnError(dbError)

	repo := postgres.NewSourceRepo(db)
	err := repo.Delete(context.Background(), 1)
	if err == nil {
		t.Fatal("Delete should return error for database error")
	}
}

func TestSourceRepo_TouchCrawledAt_DatabaseError(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	dbError := errors.New("connection lost")
	mock.ExpectExec(`UPDATE sources SET last_crawled_at`).
		WithArgs(now, int64(1)).
		WillReturnError(dbError)

	repo := postgres.NewSourceRepo(db)
	err := repo.TouchCrawledAt(context.Background(), 1, now)
	if err == nil {
		t.Fatal("TouchCrawledAt should return error for database error")
	}
}
