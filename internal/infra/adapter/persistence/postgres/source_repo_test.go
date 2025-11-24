package postgres_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/go-cmp/cmp"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/infra/adapter/persistence/postgres"
)

/* ──────────────────────────────── ヘルパ ──────────────────────────────── */

func row(src *entity.Source) *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "name", "feed_url",
		"last_crawled_at", "active",
	}).AddRow(
		src.ID, src.Name, src.FeedURL,
		src.LastCrawledAt, src.Active,
	)
}

/* ──────────────────────────────── 1. Get ──────────────────────────────── */

func TestSourceRepo_Get(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	want := &entity.Source{
		ID: 1, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &[]time.Time{time.Now()}[0], Active: true,
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
			now, true).
		WillReturnResult(sqlmock.NewResult(1, 1))

	repo := postgres.NewSourceRepo(db)
	err := repo.Create(context.Background(), &entity.Source{
		Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &now, Active: true,
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
			now, true, int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := postgres.NewSourceRepo(db)
	err := repo.Update(context.Background(), &entity.Source{
		ID: 1, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &now, Active: true,
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
	}).
		AddRow(1, "Qiita", "https://qiita.com/feed", now, true).
		AddRow(2, "Zenn", "https://zenn.dev/feed", now, true)

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

/* ──────────────────────────────── 9. Error Cases ──────────────────────────────── */

func TestSourceRepo_Update_NoRowsAffected(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	now := time.Now()
	mock.ExpectExec(`UPDATE sources`).
		WithArgs("Qiita", "https://qiita.com/feed",
			now, true, int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	repo := postgres.NewSourceRepo(db)
	err := repo.Update(context.Background(), &entity.Source{
		ID: 999, Name: "Qiita", FeedURL: "https://qiita.com/feed",
		LastCrawledAt: &now, Active: true,
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
