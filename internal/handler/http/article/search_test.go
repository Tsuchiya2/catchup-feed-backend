package article_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/article"
	"catchup-feed/internal/repository"
	artUC "catchup-feed/internal/usecase/article"
)

type stubSearchRepo struct {
	articles  []*entity.Article
	searchErr error
}

func (s *stubSearchRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return s.articles, s.searchErr
}

func (s *stubSearchRepo) SearchWithFilters(_ context.Context, keywords []string, filters repository.ArticleSearchFilters) ([]*entity.Article, error) {
	return s.articles, s.searchErr
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubSearchRepo) List(_ context.Context) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubSearchRepo) Get(_ context.Context, _ int64) (*entity.Article, error) {
	return nil, nil
}
func (s *stubSearchRepo) Create(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubSearchRepo) Update(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubSearchRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubSearchRepo) ExistsByURL(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *stubSearchRepo) ExistsByURLBatch(_ context.Context, _ []string) (map[string]bool, error) {
	return nil, nil
}
func (s *stubSearchRepo) GetWithSource(_ context.Context, _ int64) (*entity.Article, string, error) {
	return nil, "", nil
}
func (s *stubSearchRepo) ListWithSource(_ context.Context) ([]repository.ArticleWithSource, error) {
	return nil, nil
}
func (s *stubSearchRepo) ListWithSourcePaginated(_ context.Context, _, _ int) ([]repository.ArticleWithSource, error) {
	return nil, nil
}
func (s *stubSearchRepo) CountArticles(_ context.Context) (int64, error) {
	return 0, nil
}

func TestSearchHandler_Success(t *testing.T) {
	now := time.Now()
	stub := &stubSearchRepo{
		articles: []*entity.Article{
			{
				ID:          1,
				SourceID:    10,
				Title:       "Go Programming",
				URL:         "https://example.com/go",
				Summary:     "Learn Go",
				PublishedAt: now,
				CreatedAt:   now,
			},
			{
				ID:          2,
				SourceID:    10,
				Title:       "Advanced Go Patterns",
				URL:         "https://example.com/go-advanced",
				Summary:     "Advanced topics",
				PublishedAt: now,
				CreatedAt:   now,
			},
		},
	}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=go", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// レスポンスのパース
	var result []article.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("result length = %d, want 2", len(result))
	}
	if result[0].Title != "Go Programming" {
		t.Errorf("result[0].Title = %q, want %q", result[0].Title, "Go Programming")
	}
}

func TestSearchHandler_EmptyResult(t *testing.T) {
	stub := &stubSearchRepo{
		articles: []*entity.Article{},
	}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=nonexistent", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// レスポンスのパース
	var result []article.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 0 {
		t.Fatalf("result length = %d, want 0", len(result))
	}
}

func TestSearchHandler_MissingKeyword(t *testing.T) {
	stub := &stubSearchRepo{}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	// keywordパラメータなし
	req := httptest.NewRequest(http.MethodGet, "/articles/search", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSearchHandler_EmptyKeyword(t *testing.T) {
	stub := &stubSearchRepo{}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	// keywordが空文字列
	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSearchHandler_SearchError(t *testing.T) {
	stub := &stubSearchRepo{
		searchErr: errors.New("database error"),
	}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

/* ───────── Multi-keyword and filters tests ───────── */

func TestSearchHandler_MultiKeyword(t *testing.T) {
	now := time.Now()
	stub := &stubSearchRepo{
		articles: []*entity.Article{
			{
				ID:          1,
				SourceID:    10,
				Title:       "Go Programming Tutorial",
				URL:         "https://example.com/go",
				Summary:     "Learn Go programming",
				PublishedAt: now,
				CreatedAt:   now,
			},
		},
	}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=Go+Programming", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result []article.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
}

func TestSearchHandler_WithSourceIDFilter(t *testing.T) {
	now := time.Now()
	stub := &stubSearchRepo{
		articles: []*entity.Article{
			{
				ID:          1,
				SourceID:    10,
				Title:       "Test Article",
				URL:         "https://example.com/test",
				Summary:     "Test summary",
				PublishedAt: now,
				CreatedAt:   now,
			},
		},
	}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=test&source_id=10", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result []article.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
}

func TestSearchHandler_WithDateRangeFilter(t *testing.T) {
	now := time.Now()
	stub := &stubSearchRepo{
		articles: []*entity.Article{
			{
				ID:          1,
				SourceID:    10,
				Title:       "Test Article",
				URL:         "https://example.com/test",
				Summary:     "Test summary",
				PublishedAt: now,
				CreatedAt:   now,
			},
		},
	}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	from := "2024-01-01"
	to := "2024-12-31"
	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=test&from="+from+"&to="+to, nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestSearchHandler_InvalidSourceID(t *testing.T) {
	stub := &stubSearchRepo{}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	tests := []struct {
		name      string
		sourceID  string
		wantError string
	}{
		{
			name:      "non-integer source_id",
			sourceID:  "abc",
			wantError: "invalid source_id: must be a valid integer",
		},
		{
			name:      "negative source_id",
			sourceID:  "-1",
			wantError: "invalid source_id: must be positive",
		},
		{
			name:      "zero source_id",
			sourceID:  "0",
			wantError: "invalid source_id: must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=test&source_id="+tt.sourceID, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestSearchHandler_InvalidDateFormat(t *testing.T) {
	stub := &stubSearchRepo{}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	tests := []struct {
		name     string
		queryStr string
	}{
		{
			name:     "invalid from date",
			queryStr: "?keyword=test&from=invalid-date",
		},
		{
			name:     "invalid to date",
			queryStr: "?keyword=test&to=not-a-date",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/articles/search"+tt.queryStr, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestSearchHandler_InvalidDateRange(t *testing.T) {
	stub := &stubSearchRepo{}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	// from > to
	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=test&from=2024-12-31&to=2024-01-01", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSearchHandler_TooManyKeywords(t *testing.T) {
	stub := &stubSearchRepo{}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	// 11個のキーワード（最大10個を超える）
	// URLエンコードが必要
	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=k1+k2+k3+k4+k5+k6+k7+k8+k9+k10+k11", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSearchHandler_AllFilters(t *testing.T) {
	now := time.Now()
	stub := &stubSearchRepo{
		articles: []*entity.Article{
			{
				ID:          1,
				SourceID:    10,
				Title:       "Go Programming",
				URL:         "https://example.com/go",
				Summary:     "Learn Go",
				PublishedAt: now,
				CreatedAt:   now,
			},
		},
	}
	handler := article.SearchHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=Go&source_id=10&from=2024-01-01&to=2024-12-31", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result []article.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
}
