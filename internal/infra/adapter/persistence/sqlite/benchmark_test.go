package sqlite_test

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"catchup-feed/internal/infra/adapter/persistence/sqlite"
)

// BenchmarkExistsByURLBatch_SmallBatch は少数URLの性能を測定
func BenchmarkExistsByURLBatch_SmallBatch(b *testing.B) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	urls := []string{
		"https://example.com/1",
		"https://example.com/2",
		"https://example.com/3",
		"https://example.com/4",
		"https://example.com/5",
	}

	// モックの設定
	mock.ExpectQuery("SELECT url FROM articles WHERE url IN").
		WillReturnRows(sqlmock.NewRows([]string{"url"}).
			AddRow("https://example.com/1").
			AddRow("https://example.com/3"))

	repo := sqlite.NewArticleRepo(db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = repo.ExistsByURLBatch(context.Background(), urls)
		// モックをリセット
		mock.ExpectQuery("SELECT url FROM articles WHERE url IN").
			WillReturnRows(sqlmock.NewRows([]string{"url"}).
				AddRow("https://example.com/1").
				AddRow("https://example.com/3"))
	}
}

// BenchmarkExistsByURLBatch_MediumBatch は中程度のURL数の性能を測定
func BenchmarkExistsByURLBatch_MediumBatch(b *testing.B) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// 50個のURL
	urls := make([]string, 50)
	for i := 0; i < 50; i++ {
		urls[i] = "https://example.com/article"
	}

	mock.ExpectQuery("SELECT url FROM articles WHERE url IN").
		WillReturnRows(sqlmock.NewRows([]string{"url"}))

	repo := sqlite.NewArticleRepo(db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = repo.ExistsByURLBatch(context.Background(), urls)
		mock.ExpectQuery("SELECT url FROM articles WHERE url IN").
			WillReturnRows(sqlmock.NewRows([]string{"url"}))
	}
}

// BenchmarkExistsByURLBatch_LargeBatch は大量URLの性能を測定
func BenchmarkExistsByURLBatch_LargeBatch(b *testing.B) {
	db, mock, _ := sqlmock.New()
	defer func() { _ = db.Close() }()

	// 500個のURL
	urls := make([]string, 500)
	for i := 0; i < 500; i++ {
		urls[i] = "https://example.com/article"
	}

	mock.ExpectQuery("SELECT url FROM articles WHERE url IN").
		WillReturnRows(sqlmock.NewRows([]string{"url"}))

	repo := sqlite.NewArticleRepo(db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = repo.ExistsByURLBatch(context.Background(), urls)
		mock.ExpectQuery("SELECT url FROM articles WHERE url IN").
			WillReturnRows(sqlmock.NewRows([]string{"url"}))
	}
}
