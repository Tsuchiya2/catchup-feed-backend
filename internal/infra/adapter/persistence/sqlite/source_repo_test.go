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
	"catchup-feed/internal/repository"
)

// ─────────────────────────────────────────────
// ヘルパ：行生成
// ─────────────────────────────────────────────
func row(src *entity.Source) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "feed_url",
		"last_crawled_at", "active",
	}).AddRow(
		src.ID, src.Name, src.FeedURL,
		src.LastCrawledAt, src.Active,
	)
}

// ─────────────────────────────────────────────
// 1. Get
// ─────────────────────────────────────────────
func TestSourceRepo_Get(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	want := &entity.Source{ID: 1, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &[]time.Time{time.Now()}[0], Active: true}

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id")).
		WithArgs(int64(1)).
		WillReturnRows(row(want))

	repo := sqlite.NewSourceRepo(db)
	got, err := repo.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("Get err=%v", err)
	}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(entity.Source{})); diff != "" {
		t.Fatalf("Get mismatch (-want +got):\n%s", diff)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// ─────────────────────────────────────────────
// 2. List
// ─────────────────────────────────────────────
func TestSourceRepo_List(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	want := &entity.Source{ID: 1, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &[]time.Time{time.Now()}[0], Active: true}

	mock.ExpectQuery("SELECT").WillReturnRows(row(want))

	repo := sqlite.NewSourceRepo(db)
	got, err := repo.List(context.Background())
	if err != nil || len(got) != 1 {
		t.Fatalf("List err=%v len=%d", err, len(got))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// ─────────────────────────────────────────────
// 3. Search
// ─────────────────────────────────────────────
func TestSourceRepo_Search(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectQuery("FROM sources").
		WithArgs("%go%", "%go%").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "feed_url",
			"last_crawled_at", "active"})) // 空結果で十分

	repo := sqlite.NewSourceRepo(db)
	_, err := repo.Search(context.Background(), "go")
	if err != nil {
		t.Fatalf("Search err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// ─────────────────────────────────────────────
// 4. Create
// ─────────────────────────────────────────────
func TestSourceRepo_Create(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO sources")).
		WithArgs("Qiita", "https://qiita.com/feed", sqlmock.AnyArg(), true).
		WillReturnResult(sqlmock.NewResult(1, 1))

	repo := sqlite.NewSourceRepo(db)
	err := repo.Create(context.Background(), &entity.Source{
		Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &[]time.Time{time.Now()}[0], Active: true,
	})
	if err != nil {
		t.Fatalf("Create err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// ─────────────────────────────────────────────
// 5. Update
// ─────────────────────────────────────────────
func TestSourceRepo_Update(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE sources").
		WithArgs("Qiita", "https://qiita.com/feed",
								sqlmock.AnyArg(), true, 1).
		WillReturnResult(sqlmock.NewResult(0, 1)) // 1行更新

	repo := sqlite.NewSourceRepo(db)
	err := repo.Update(context.Background(), &entity.Source{
		ID: 1, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &[]time.Time{time.Now()}[0], Active: true,
	})
	if err != nil {
		t.Fatalf("Update err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// ─────────────────────────────────────────────
// 6. Delete
// ─────────────────────────────────────────────
func TestSourceRepo_Delete(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectExec("DELETE FROM sources").
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1)) // 1行削除

	repo := sqlite.NewSourceRepo(db)
	err := repo.Delete(context.Background(), 1)
	if err != nil {
		t.Fatalf("Delete err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// ─────────────────────────────────────────────
// 7. ListActive
// ─────────────────────────────────────────────
func TestSourceRepo_ListActive(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
	}).
		AddRow(1, "Qiita", "https://qiita.com/feed", now, true).
		AddRow(2, "Zenn", "https://zenn.dev/feed", now, true)

	mock.ExpectQuery("FROM sources").
		WillReturnRows(rows)

	repo := sqlite.NewSourceRepo(db)
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
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestSourceRepo_ListActive_Empty(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "last_crawled_at", "active",
	})

	mock.ExpectQuery("FROM sources").
		WillReturnRows(rows)

	repo := sqlite.NewSourceRepo(db)
	sources, err := repo.ListActive(context.Background())
	if err != nil {
		t.Fatalf("ListActive err=%v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("ListActive expected 0 sources, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// ─────────────────────────────────────────────
// 8. TouchCrawledAt
// ─────────────────────────────────────────────
func TestSourceRepo_TouchCrawledAt(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec("UPDATE sources SET last_crawled_at").
		WithArgs(now, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := sqlite.NewSourceRepo(db)
	err := repo.TouchCrawledAt(context.Background(), 1, now)
	if err != nil {
		t.Fatalf("TouchCrawledAt err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestSourceRepo_TouchCrawledAt_NonExistent(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec("UPDATE sources SET last_crawled_at").
		WithArgs(now, int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := sqlite.NewSourceRepo(db)
	// TouchCrawledAt doesn't check rows affected, so it should succeed
	err := repo.TouchCrawledAt(context.Background(), 999, now)
	if err != nil {
		t.Fatalf("TouchCrawledAt err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// ─────────────────────────────────────────────
// 9. Error Cases
// ─────────────────────────────────────────────
func TestSourceRepo_Update_NoRowsAffected(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec("UPDATE sources").
		WithArgs("Qiita", "https://qiita.com/feed",
			now, true, int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := sqlite.NewSourceRepo(db)
	err := repo.Update(context.Background(), &entity.Source{
		ID: 999, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &now, Active: true,
	})
	if err == nil {
		t.Fatal("Update should fail when no rows affected")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

func TestSourceRepo_Delete_NoRowsAffected(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	mock.ExpectExec("DELETE FROM sources").
		WithArgs(int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := sqlite.NewSourceRepo(db)
	err := repo.Delete(context.Background(), 999)
	if err == nil {
		t.Fatal("Delete should fail when no rows affected")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// ─────────────────────────────────────────────
// 10. Filter-Only Search Tests (TASK-006)
// ─────────────────────────────────────────────

// TestSourceRepo_SearchWithFilters_EmptyKeywords_NoFilters verifies empty keywords with no filters returns all sources
func TestSourceRepo_SearchWithFilters_EmptyKeywords_NoFilters(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "source_type", "last_crawled_at", "active",
	}).
		AddRow(1, "Tech Blog", "https://example.com/feed", "RSS", now, true).
		AddRow(2, "News Site", "https://news.example.com/feed", "Webflow", now, false)

	// No WHERE clause - returns all sources
	mock.ExpectQuery("FROM sources").
		WillReturnRows(rows)

	repo := sqlite.NewSourceRepo(db)
	sources, err := repo.SearchWithFilters(context.Background(), []string{}, repository.SourceSearchFilters{})
	if err != nil {
		t.Fatalf("SearchWithFilters err=%v", err)
	}
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// TestSourceRepo_SearchWithFilters_EmptyKeywords_SourceTypeFilter verifies empty keywords with source_type filter
func TestSourceRepo_SearchWithFilters_EmptyKeywords_SourceTypeFilter(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "source_type", "last_crawled_at", "active",
	}).AddRow(1, "RSS Blog", "https://example.com/feed", "RSS", now, true)

	sourceType := "RSS"
	filters := repository.SourceSearchFilters{
		SourceType: &sourceType,
	}

	// Only source_type filter in WHERE clause (SQLite uses ? placeholders)
	mock.ExpectQuery("FROM sources").
		WithArgs("RSS").
		WillReturnRows(rows)

	repo := sqlite.NewSourceRepo(db)
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
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// TestSourceRepo_SearchWithFilters_EmptyKeywords_ActiveFilter verifies empty keywords with active filter
func TestSourceRepo_SearchWithFilters_EmptyKeywords_ActiveFilter(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "source_type", "last_crawled_at", "active",
	}).
		AddRow(1, "Active Blog", "https://example.com/feed", "RSS", now, true).
		AddRow(2, "Another Active", "https://example2.com/feed", "Webflow", now, true)

	active := true
	filters := repository.SourceSearchFilters{
		Active: &active,
	}

	// Only active filter in WHERE clause (SQLite uses ? placeholders)
	mock.ExpectQuery("FROM sources").
		WithArgs(true).
		WillReturnRows(rows)

	repo := sqlite.NewSourceRepo(db)
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
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// TestSourceRepo_SearchWithFilters_EmptyKeywords_MultipleFilters verifies empty keywords with multiple filters
func TestSourceRepo_SearchWithFilters_EmptyKeywords_MultipleFilters(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "source_type", "last_crawled_at", "active",
	}).AddRow(1, "Active RSS", "https://example.com/feed", "RSS", now, true)

	sourceType := "RSS"
	active := true
	filters := repository.SourceSearchFilters{
		SourceType: &sourceType,
		Active:     &active,
	}

	// Both filters in WHERE clause (SQLite uses ? placeholders)
	mock.ExpectQuery("FROM sources").
		WithArgs("RSS", true).
		WillReturnRows(rows)

	repo := sqlite.NewSourceRepo(db)
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
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}

// TestSourceRepo_SearchWithFilters_EmptyKeywords_EmptyResult verifies empty result returns empty slice (not nil)
func TestSourceRepo_SearchWithFilters_EmptyKeywords_EmptyResult(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{
		"id", "name", "feed_url", "source_type", "last_crawled_at", "active",
	}) // No rows

	sourceType := "NonExistent"
	filters := repository.SourceSearchFilters{
		SourceType: &sourceType,
	}

	mock.ExpectQuery("FROM sources").
		WithArgs("NonExistent").
		WillReturnRows(rows)

	repo := sqlite.NewSourceRepo(db)
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
		t.Fatalf("ExpectationsWereMet: %v", err)
	}
}
