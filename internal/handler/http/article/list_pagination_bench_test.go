package article_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"catchup-feed/internal/common/pagination"
	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/article"
	"catchup-feed/internal/repository"
	artUC "catchup-feed/internal/usecase/article"
)

// Benchmark pagination with various page sizes
func BenchmarkListHandler_Pagination_Page1_Limit20(b *testing.B) {
	benchmarkPaginationHandler(b, 1, 20, 1000)
}

func BenchmarkListHandler_Pagination_Page10_Limit20(b *testing.B) {
	benchmarkPaginationHandler(b, 10, 20, 1000)
}

func BenchmarkListHandler_Pagination_Page50_Limit20(b *testing.B) {
	benchmarkPaginationHandler(b, 50, 20, 1000)
}

func BenchmarkListHandler_Pagination_Page1_Limit50(b *testing.B) {
	benchmarkPaginationHandler(b, 1, 50, 1000)
}

func BenchmarkListHandler_Pagination_Page1_Limit100(b *testing.B) {
	benchmarkPaginationHandler(b, 1, 100, 1000)
}

func benchmarkPaginationHandler(b *testing.B, page, limit int, totalArticles int) {
	// Prepare test data
	now := time.Now()
	articlesWithSrc := make([]repository.ArticleWithSource, totalArticles)
	for i := 0; i < totalArticles; i++ {
		articlesWithSrc[i] = repository.ArticleWithSource{
			Article: &entity.Article{
				ID:          int64(i + 1),
				SourceID:    10,
				Title:       "Test Article",
				URL:         "https://example.com/article",
				Summary:     "Test Summary",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Test Source",
		}
	}

	stub := &stubArticleRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      int64(totalArticles),
	}

	handler := article.ListHandler{
		Svc: artUC.Service{Repo: stub},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/articles?page="+string(rune(page))+"&limit="+string(rune(limit)), nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

// Benchmark service layer pagination
func BenchmarkService_ListWithSourcePaginated(b *testing.B) {
	now := time.Now()
	totalArticles := 10000
	articlesWithSrc := make([]repository.ArticleWithSource, totalArticles)
	for i := 0; i < totalArticles; i++ {
		articlesWithSrc[i] = repository.ArticleWithSource{
			Article: &entity.Article{
				ID:          int64(i + 1),
				SourceID:    10,
				Title:       "Test Article",
				URL:         "https://example.com/article",
				Summary:     "Test Summary",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Test Source",
		}
	}

	mock := &stubArticleRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      int64(totalArticles),
	}

	svc := artUC.Service{Repo: mock}
	params := pagination.Params{
		Page:  1,
		Limit: 20,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.ListWithSourcePaginated(context.Background(), params)
	}
}
