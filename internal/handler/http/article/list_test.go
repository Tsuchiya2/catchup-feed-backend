package article_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

/* ───────── モック実装 ───────── */

type stubArticleRepo struct {
	articles       []*entity.Article
	articlesWithSrc []repository.ArticleWithSource
	totalCount     int64
	listErr        error
	countErr       error
}

func (s *stubArticleRepo) List(_ context.Context) ([]*entity.Article, error) {
	return s.articles, s.listErr
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubArticleRepo) Get(_ context.Context, _ int64) (*entity.Article, error) {
	return nil, nil
}
func (s *stubArticleRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubArticleRepo) SearchWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubArticleRepo) Create(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubArticleRepo) Update(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubArticleRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubArticleRepo) ExistsByURL(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *stubArticleRepo) ExistsByURLBatch(_ context.Context, _ []string) (map[string]bool, error) {
	return nil, nil
}
func (s *stubArticleRepo) GetWithSource(_ context.Context, _ int64) (*entity.Article, string, error) {
	return nil, "", nil
}
func (s *stubArticleRepo) ListWithSource(_ context.Context) ([]repository.ArticleWithSource, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	var result []repository.ArticleWithSource
	for _, a := range s.articles {
		result = append(result, repository.ArticleWithSource{
			Article:    a,
			SourceName: "Test Source",
		})
	}
	return result, nil
}
func (s *stubArticleRepo) ListWithSourcePaginated(_ context.Context, offset, limit int) ([]repository.ArticleWithSource, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	// Return subset based on offset/limit
	if offset >= len(s.articlesWithSrc) {
		return []repository.ArticleWithSource{}, nil
	}
	end := offset + limit
	if end > len(s.articlesWithSrc) {
		end = len(s.articlesWithSrc)
	}
	return s.articlesWithSrc[offset:end], nil
}
func (s *stubArticleRepo) CountArticles(_ context.Context) (int64, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.totalCount, nil
}
func (s *stubArticleRepo) CountArticlesWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) (int64, error) {
	return 0, nil
}
func (s *stubArticleRepo) SearchWithFiltersPaginated(_ context.Context, _ []string, _ repository.ArticleSearchFilters, _, _ int) ([]repository.ArticleWithSource, error) {
	return nil, nil
}

/* ───────── テストケース ───────── */

func TestListHandler_Success(t *testing.T) {
	now := time.Now()
	articlesWithSrc := []repository.ArticleWithSource{
		{
			Article: &entity.Article{
				ID:          1,
				SourceID:    10,
				Title:       "Test Article 1",
				URL:         "https://example.com/article1",
				Summary:     "Summary 1",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Test Source",
		},
		{
			Article: &entity.Article{
				ID:          2,
				SourceID:    10,
				Title:       "Test Article 2",
				URL:         "https://example.com/article2",
				Summary:     "Summary 2",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Test Source",
		},
	}

	stub := &stubArticleRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      2,
	}

	handler := article.ListHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
		Logger:        slog.Default(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// レスポンスのパース (pagination.Response format)
	var result pagination.Response[article.DTO]
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// 結果の検証
	if len(result.Data) != 2 {
		t.Fatalf("result.Data length = %d, want 2", len(result.Data))
	}
	if result.Data[0].ID != 1 {
		t.Errorf("result.Data[0].ID = %d, want 1", result.Data[0].ID)
	}
	if result.Data[0].Title != "Test Article 1" {
		t.Errorf("result.Data[0].Title = %q, want %q", result.Data[0].Title, "Test Article 1")
	}
	if result.Data[1].ID != 2 {
		t.Errorf("result.Data[1].ID = %d, want 2", result.Data[1].ID)
	}
}

func TestListHandler_EmptyList(t *testing.T) {
	stub := &stubArticleRepo{
		articlesWithSrc: []repository.ArticleWithSource{},
		totalCount:      0,
	}

	handler := article.ListHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
		Logger:        slog.Default(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// レスポンスのパース
	var result pagination.Response[article.DTO]
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// 空のリストが返ることを確認
	if len(result.Data) != 0 {
		t.Fatalf("result.Data length = %d, want 0", len(result.Data))
	}
}

func TestListHandler_Error(t *testing.T) {
	stub := &stubArticleRepo{
		countErr: errors.New("database error"),
	}

	handler := article.ListHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
		Logger:        slog.Default(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// エラー時は500を返す
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

/* ───────── Pagination Tests ───────── */

func TestListHandler_Pagination_ValidParams(t *testing.T) {
	t.Parallel()

	now := time.Now()
	// Create 100 test articles to support pagination
	articlesWithSrc := make([]repository.ArticleWithSource, 100)
	for i := 0; i < 100; i++ {
		articlesWithSrc[i] = repository.ArticleWithSource{
			Article: &entity.Article{
				ID:          int64(i + 1),
				SourceID:    10,
				Title:       "Article",
				URL:         "https://example.com/",
				Summary:     "Summary",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Test Source",
		}
	}

	stub := &stubArticleRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      150,
	}

	handler := article.ListHandler{
		Svc: artUC.Service{Repo: stub},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
		Logger: slog.Default(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles?page=2&limit=20", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// Parse response
	var result pagination.Response[article.DTO]
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify pagination metadata
	if result.Pagination.Page != 2 {
		t.Errorf("Pagination.Page = %d, want 2", result.Pagination.Page)
	}
	if result.Pagination.Limit != 20 {
		t.Errorf("Pagination.Limit = %d, want 20", result.Pagination.Limit)
	}
	if result.Pagination.Total != 150 {
		t.Errorf("Pagination.Total = %d, want 150", result.Pagination.Total)
	}
	if result.Pagination.TotalPages != 8 {
		t.Errorf("Pagination.TotalPages = %d, want 8", result.Pagination.TotalPages)
	}

	// Verify data (page 2 with limit 20 should return 20 items from our 100-item dataset)
	if len(result.Data) != 20 {
		t.Errorf("Data length = %d, want 20", len(result.Data))
	}
}

func TestListHandler_Pagination_DefaultParams(t *testing.T) {
	t.Parallel()

	stub := &stubArticleRepo{
		articlesWithSrc: []repository.ArticleWithSource{},
		totalCount:      0,
	}

	handler := article.ListHandler{
		Svc: artUC.Service{Repo: stub},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
		Logger: slog.Default(),
	}

	// No query parameters - should use defaults
	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result pagination.Response[article.DTO]
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify defaults are applied
	if result.Pagination.Page != 1 {
		t.Errorf("Pagination.Page = %d, want 1 (default)", result.Pagination.Page)
	}
	if result.Pagination.Limit != 20 {
		t.Errorf("Pagination.Limit = %d, want 20 (default)", result.Pagination.Limit)
	}
}

func TestListHandler_Pagination_InvalidPage(t *testing.T) {
	t.Parallel()

	handler := article.ListHandler{
		Svc: artUC.Service{},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
		Logger: slog.Default(),
	}

	tests := []struct {
		name  string
		query string
	}{
		{"negative page", "page=-1"},
		{"zero page", "page=0"},
		{"non-integer page", "page=abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/articles?"+tt.query, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestListHandler_Pagination_InvalidLimit(t *testing.T) {
	t.Parallel()

	handler := article.ListHandler{
		Svc: artUC.Service{},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
		Logger: slog.Default(),
	}

	tests := []struct {
		name  string
		query string
	}{
		{"negative limit", "limit=-10"},
		{"zero limit", "limit=0"},
		{"exceeds max", "limit=101"},
		{"non-integer limit", "limit=xyz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/articles?"+tt.query, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestListHandler_Pagination_EmptyResults(t *testing.T) {
	t.Parallel()

	stub := &stubArticleRepo{
		articlesWithSrc: []repository.ArticleWithSource{},
		totalCount:      150,
	}

	handler := article.ListHandler{
		Svc: artUC.Service{Repo: stub},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
		Logger: slog.Default(),
	}

	// Request a page beyond available data
	req := httptest.NewRequest(http.MethodGet, "/articles?page=100", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result pagination.Response[article.DTO]
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return empty data with valid metadata
	if len(result.Data) != 0 {
		t.Errorf("Data length = %d, want 0", len(result.Data))
	}
	if result.Pagination.Total != 150 {
		t.Errorf("Pagination.Total = %d, want 150", result.Pagination.Total)
	}
}

func TestListHandler_Pagination_ServiceError(t *testing.T) {
	t.Parallel()

	stub := &stubArticleRepo{
		countErr: errors.New("count error"),
	}

	handler := article.ListHandler{
		Svc: artUC.Service{Repo: stub},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
		Logger: slog.Default(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles?page=1&limit=20", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}
