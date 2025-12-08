package article_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/article"
	"catchup-feed/internal/repository"
	artUC "catchup-feed/internal/usecase/article"
)

type benchListRepo struct{}

func (b *benchListRepo) List(_ context.Context) ([]*entity.Article, error) {
	// 100件の記事を返すシミュレーション
	articles := make([]*entity.Article, 100)
	now := time.Now()
	for i := 0; i < 100; i++ {
		articles[i] = &entity.Article{
			ID:          int64(i + 1),
			SourceID:    1,
			Title:       "Benchmark Article Title",
			URL:         "https://example.com/article",
			Summary:     "This is a test summary for benchmark",
			PublishedAt: now,
			CreatedAt:   now,
		}
	}
	return articles, nil
}

// 以下は未使用だが、インターフェース満たすために実装
func (b *benchListRepo) Get(_ context.Context, _ int64) (*entity.Article, error) {
	return nil, nil
}
func (b *benchListRepo) Create(_ context.Context, _ *entity.Article) error {
	return nil
}
func (b *benchListRepo) Update(_ context.Context, _ *entity.Article) error {
	return nil
}
func (b *benchListRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (b *benchListRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return nil, nil
}
func (b *benchListRepo) SearchWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) ([]*entity.Article, error) {
	return nil, nil
}
func (b *benchListRepo) ExistsByURL(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (b *benchListRepo) ExistsByURLBatch(_ context.Context, _ []string) (map[string]bool, error) {
	return nil, nil
}
func (b *benchListRepo) GetWithSource(_ context.Context, _ int64) (*entity.Article, string, error) {
	return nil, "", nil
}
func (b *benchListRepo) ListWithSource(_ context.Context) ([]repository.ArticleWithSource, error) {
	// 100件の記事を返すシミュレーション
	result := make([]repository.ArticleWithSource, 100)
	now := time.Now()
	for i := 0; i < 100; i++ {
		result[i] = repository.ArticleWithSource{
			Article: &entity.Article{
				ID:          int64(i + 1),
				SourceID:    1,
				Title:       "Benchmark Article Title",
				URL:         "https://example.com/article",
				Summary:     "This is a test summary for benchmark",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Benchmark Source",
		}
	}
	return result, nil
}
func (b *benchListRepo) ListWithSourcePaginated(_ context.Context, offset, limit int) ([]repository.ArticleWithSource, error) {
	// 100件の記事から指定された範囲を返すシミュレーション
	result := make([]repository.ArticleWithSource, 0, limit)
	now := time.Now()
	for i := offset; i < offset+limit && i < 100; i++ {
		result = append(result, repository.ArticleWithSource{
			Article: &entity.Article{
				ID:          int64(i + 1),
				SourceID:    1,
				Title:       "Benchmark Article Title",
				URL:         "https://example.com/article",
				Summary:     "This is a test summary for benchmark",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Benchmark Source",
		})
	}
	return result, nil
}
func (b *benchListRepo) CountArticles(_ context.Context) (int64, error) {
	return 100, nil
}
func (b *benchListRepo) CountArticlesWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) (int64, error) {
	return 0, nil
}
func (b *benchListRepo) SearchWithFiltersPaginated(_ context.Context, _ []string, _ repository.ArticleSearchFilters, _, _ int) ([]repository.ArticleWithSource, error) {
	return nil, nil
}

// BenchmarkListHandler_100Articles は100件の記事一覧取得の性能を測定
func BenchmarkListHandler_100Articles(b *testing.B) {
	repo := &benchListRepo{}
	handler := article.ListHandler{Svc: artUC.Service{Repo: repo}}

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

// BenchmarkListHandler_Parallel は並行リクエストの性能を測定
func BenchmarkListHandler_Parallel(b *testing.B) {
	repo := &benchListRepo{}
	handler := article.ListHandler{Svc: artUC.Service{Repo: repo}}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodGet, "/articles", nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
		}
	})
}
