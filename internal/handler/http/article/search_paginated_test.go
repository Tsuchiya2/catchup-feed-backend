package article_test

import (
	"context"
	"encoding/json"
	"errors"
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

type stubSearchPaginatedRepo struct {
	articlesWithSrc []repository.ArticleWithSource
	totalCount      int64
	searchErr       error
	countErr        error
}

func (s *stubSearchPaginatedRepo) List(_ context.Context) ([]*entity.Article, error) {
	return nil, nil
}

func (s *stubSearchPaginatedRepo) Get(_ context.Context, _ int64) (*entity.Article, error) {
	return nil, nil
}

func (s *stubSearchPaginatedRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return nil, nil
}

func (s *stubSearchPaginatedRepo) SearchWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) ([]*entity.Article, error) {
	return nil, nil
}

func (s *stubSearchPaginatedRepo) Create(_ context.Context, _ *entity.Article) error {
	return nil
}

func (s *stubSearchPaginatedRepo) Update(_ context.Context, _ *entity.Article) error {
	return nil
}

func (s *stubSearchPaginatedRepo) Delete(_ context.Context, _ int64) error {
	return nil
}

func (s *stubSearchPaginatedRepo) ExistsByURL(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (s *stubSearchPaginatedRepo) ExistsByURLBatch(_ context.Context, _ []string) (map[string]bool, error) {
	return nil, nil
}

func (s *stubSearchPaginatedRepo) GetWithSource(_ context.Context, _ int64) (*entity.Article, string, error) {
	return nil, "", nil
}

func (s *stubSearchPaginatedRepo) ListWithSource(_ context.Context) ([]repository.ArticleWithSource, error) {
	return nil, nil
}

func (s *stubSearchPaginatedRepo) ListWithSourcePaginated(_ context.Context, offset, limit int) ([]repository.ArticleWithSource, error) {
	return nil, nil
}

func (s *stubSearchPaginatedRepo) CountArticles(_ context.Context) (int64, error) {
	return 0, nil
}

func (s *stubSearchPaginatedRepo) CountArticlesWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) (int64, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.totalCount, nil
}

func (s *stubSearchPaginatedRepo) SearchWithFiltersPaginated(_ context.Context, _ []string, _ repository.ArticleSearchFilters, offset, limit int) ([]repository.ArticleWithSource, error) {
	if s.searchErr != nil {
		return nil, s.searchErr
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

/* ───────── テストケース ───────── */

// TestSearchPaginated_ValidRequest tests basic search with keyword
func TestSearchPaginated_ValidRequest(t *testing.T) {
	t.Parallel()

	now := time.Now()
	articlesWithSrc := []repository.ArticleWithSource{
		{
			Article: &entity.Article{
				ID:          1,
				SourceID:    10,
				Title:       "Go Programming Guide",
				URL:         "https://example.com/article1",
				Summary:     "Learn Go programming",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Tech Blog",
		},
		{
			Article: &entity.Article{
				ID:          2,
				SourceID:    10,
				Title:       "Go Tutorial",
				URL:         "https://example.com/article2",
				Summary:     "Go basics",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Tech Blog",
		},
	}

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      2,
	}

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=Go&page=1&limit=10", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Data) != 2 {
		t.Fatalf("result.Data length = %d, want 2", len(result.Data))
	}

	if result.Pagination.Total != 2 {
		t.Errorf("Pagination.Total = %d, want 2", result.Pagination.Total)
	}

	if result.Pagination.Page != 1 {
		t.Errorf("Pagination.Page = %d, want 1", result.Pagination.Page)
	}
}

// TestSearchPaginated_WithSourceIDFilter tests search with source_id filter
func TestSearchPaginated_WithSourceIDFilter(t *testing.T) {
	t.Parallel()

	now := time.Now()
	articlesWithSrc := []repository.ArticleWithSource{
		{
			Article: &entity.Article{
				ID:          1,
				SourceID:    5,
				Title:       "Article from Source 5",
				URL:         "https://example.com/article1",
				Summary:     "Summary",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Source 5",
		},
	}

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      1,
	}

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=Article&source_id=5", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Data) != 1 {
		t.Fatalf("result.Data length = %d, want 1", len(result.Data))
	}

	if result.Data[0].SourceID != 5 {
		t.Errorf("result.Data[0].SourceID = %d, want 5", result.Data[0].SourceID)
	}
}

// TestSearchPaginated_WithDateRange tests search with from/to date filters
func TestSearchPaginated_WithDateRange(t *testing.T) {
	t.Parallel()

	now := time.Now()
	articlesWithSrc := []repository.ArticleWithSource{
		{
			Article: &entity.Article{
				ID:          1,
				SourceID:    10,
				Title:       "Recent Article",
				URL:         "https://example.com/article1",
				Summary:     "Summary",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Tech Blog",
		},
	}

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      1,
	}

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=Article&from=2025-01-01&to=2025-12-31", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}
}

// TestSearchPaginated_EmptyResults tests search with no matching articles
func TestSearchPaginated_EmptyResults(t *testing.T) {
	t.Parallel()

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: []repository.ArticleWithSource{},
		totalCount:      0,
	}

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=nonexistent", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Data) != 0 {
		t.Errorf("result.Data length = %d, want 0", len(result.Data))
	}

	if result.Pagination.Total != 0 {
		t.Errorf("Pagination.Total = %d, want 0", result.Pagination.Total)
	}
}

// TestSearchPaginated_InvalidPage tests invalid page parameter
func TestSearchPaginated_InvalidPage(t *testing.T) {
	t.Parallel()

	handler := article.SearchPaginatedHandler{
		Svc: artUC.Service{},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
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
			req := httptest.NewRequest(http.MethodGet, "/articles/search?"+tt.query, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

// TestSearchPaginated_InvalidLimit tests invalid limit parameter
func TestSearchPaginated_InvalidLimit(t *testing.T) {
	t.Parallel()

	handler := article.SearchPaginatedHandler{
		Svc: artUC.Service{},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
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
			req := httptest.NewRequest(http.MethodGet, "/articles/search?"+tt.query, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

// TestSearchPaginated_InvalidSourceID tests invalid source_id parameter
func TestSearchPaginated_InvalidSourceID(t *testing.T) {
	t.Parallel()

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{},
		PaginationCfg: pagination.DefaultConfig(),
	}

	tests := []struct {
		name  string
		query string
	}{
		{"non-integer source_id", "source_id=abc"},
		{"negative source_id", "source_id=-1"},
		{"zero source_id", "source_id=0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/articles/search?"+tt.query, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

// TestSearchPaginated_InvalidFromDate tests invalid from date parameter
func TestSearchPaginated_InvalidFromDate(t *testing.T) {
	t.Parallel()

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{},
		PaginationCfg: pagination.DefaultConfig(),
	}

	tests := []struct {
		name  string
		query string
	}{
		{"wrong format", "from=2025/01/01"},
		{"invalid date", "from=not-a-date"},
		{"incomplete date", "from=2025-01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/articles/search?"+tt.query, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

// TestSearchPaginated_InvalidToDate tests invalid to date parameter
func TestSearchPaginated_InvalidToDate(t *testing.T) {
	t.Parallel()

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{},
		PaginationCfg: pagination.DefaultConfig(),
	}

	tests := []struct {
		name  string
		query string
	}{
		{"wrong format", "to=2025/12/31"},
		{"invalid date", "to=not-a-date"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/articles/search?"+tt.query, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

// TestSearchPaginated_InvalidDateRange tests when from > to
func TestSearchPaginated_InvalidDateRange(t *testing.T) {
	t.Parallel()

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{},
		PaginationCfg: pagination.DefaultConfig(),
	}

	// from date is after to date
	req := httptest.NewRequest(http.MethodGet, "/articles/search?from=2025-12-31&to=2025-01-01", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

// TestSearchPaginated_MultiplePages tests pagination behavior
func TestSearchPaginated_MultiplePages(t *testing.T) {
	t.Parallel()

	now := time.Now()
	// Create 50 test articles
	articlesWithSrc := make([]repository.ArticleWithSource, 50)
	for i := 0; i < 50; i++ {
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

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      50,
	}

	handler := article.SearchPaginatedHandler{
		Svc: artUC.Service{Repo: stub},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
	}

	// Request page 2
	req := httptest.NewRequest(http.MethodGet, "/articles/search?page=2&limit=20", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Page 2 with limit 20 should return 20 items (items 21-40)
	if len(result.Data) != 20 {
		t.Errorf("Data length = %d, want 20", len(result.Data))
	}

	if result.Pagination.Page != 2 {
		t.Errorf("Pagination.Page = %d, want 2", result.Pagination.Page)
	}

	if result.Pagination.Total != 50 {
		t.Errorf("Pagination.Total = %d, want 50", result.Pagination.Total)
	}

	if result.Pagination.TotalPages != 3 {
		t.Errorf("Pagination.TotalPages = %d, want 3", result.Pagination.TotalPages)
	}
}

// TestSearchPaginated_LastPage tests last page with partial results
func TestSearchPaginated_LastPage(t *testing.T) {
	t.Parallel()

	now := time.Now()
	// Create 25 test articles
	articlesWithSrc := make([]repository.ArticleWithSource, 25)
	for i := 0; i < 25; i++ {
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

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      25,
	}

	handler := article.SearchPaginatedHandler{
		Svc: artUC.Service{Repo: stub},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
	}

	// Request page 2 - should return 5 items (items 21-25)
	req := httptest.NewRequest(http.MethodGet, "/articles/search?page=2&limit=20", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Last page should return 5 items
	if len(result.Data) != 5 {
		t.Errorf("Data length = %d, want 5", len(result.Data))
	}

	if result.Pagination.TotalPages != 2 {
		t.Errorf("Pagination.TotalPages = %d, want 2", result.Pagination.TotalPages)
	}
}

// TestSearchPaginated_PageBeyondTotal tests requesting page beyond available data
func TestSearchPaginated_PageBeyondTotal(t *testing.T) {
	t.Parallel()

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: []repository.ArticleWithSource{},
		totalCount:      10,
	}

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
	}

	// Request page 100 when total is only 10
	req := httptest.NewRequest(http.MethodGet, "/articles/search?page=100", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return empty data
	if len(result.Data) != 0 {
		t.Errorf("Data length = %d, want 0", len(result.Data))
	}

	if result.Pagination.Total != 10 {
		t.Errorf("Pagination.Total = %d, want 10", result.Pagination.Total)
	}
}

// TestSearchPaginated_DatabaseError tests database error handling
func TestSearchPaginated_DatabaseError(t *testing.T) {
	t.Parallel()

	stub := &stubSearchPaginatedRepo{
		searchErr: errors.New("database connection failed"),
	}

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status code = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

// TestSearchPaginated_CountError tests graceful degradation when count fails
func TestSearchPaginated_CountError(t *testing.T) {
	t.Parallel()

	now := time.Now()
	articlesWithSrc := []repository.ArticleWithSource{
		{
			Article: &entity.Article{
				ID:          1,
				SourceID:    10,
				Title:       "Article",
				URL:         "https://example.com/article1",
				Summary:     "Summary",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Test Source",
		},
	}

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: articlesWithSrc,
		countErr:        errors.New("count query failed"),
	}

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
	}

	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return data even if count failed
	if len(result.Data) != 1 {
		t.Errorf("Data length = %d, want 1", len(result.Data))
	}

	// Total should be -1 for graceful degradation
	if result.Pagination.Total != -1 {
		t.Errorf("Pagination.Total = %d, want -1 (graceful degradation)", result.Pagination.Total)
	}

	// TotalPages should be 0 when total is unknown
	if result.Pagination.TotalPages != 0 {
		t.Errorf("Pagination.TotalPages = %d, want 0", result.Pagination.TotalPages)
	}
}

// TestSearchPaginated_DefaultParameters tests default page and limit
func TestSearchPaginated_DefaultParameters(t *testing.T) {
	t.Parallel()

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: []repository.ArticleWithSource{},
		totalCount:      0,
	}

	handler := article.SearchPaginatedHandler{
		Svc: artUC.Service{Repo: stub},
		PaginationCfg: pagination.Config{
			DefaultPage:  1,
			DefaultLimit: 20,
			MaxLimit:     100,
		},
	}

	// No page/limit parameters
	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Defaults should be applied
	if result.Pagination.Page != 1 {
		t.Errorf("Pagination.Page = %d, want 1 (default)", result.Pagination.Page)
	}

	if result.Pagination.Limit != 20 {
		t.Errorf("Pagination.Limit = %d, want 20 (default)", result.Pagination.Limit)
	}
}

// TestSearchPaginated_MultiKeyword tests multi-keyword search
func TestSearchPaginated_MultiKeyword(t *testing.T) {
	t.Parallel()

	now := time.Now()
	articlesWithSrc := []repository.ArticleWithSource{
		{
			Article: &entity.Article{
				ID:          1,
				SourceID:    10,
				Title:       "Go Programming Language Tutorial",
				URL:         "https://example.com/article1",
				Summary:     "Complete guide",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Tech Blog",
		},
	}

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      1,
	}

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
	}

	// Multi-keyword search (space-separated)
	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=Go+Programming", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Data) != 1 {
		t.Errorf("Data length = %d, want 1", len(result.Data))
	}
}

// TestSearchPaginated_AllFilters tests search with all filters combined
func TestSearchPaginated_AllFilters(t *testing.T) {
	t.Parallel()

	now := time.Now()
	articlesWithSrc := []repository.ArticleWithSource{
		{
			Article: &entity.Article{
				ID:          1,
				SourceID:    5,
				Title:       "Go Programming",
				URL:         "https://example.com/article1",
				Summary:     "Summary",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Tech Blog",
		},
	}

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      1,
	}

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
	}

	// All filters: keyword, source_id, from, to
	req := httptest.NewRequest(http.MethodGet, "/articles/search?keyword=Go&source_id=5&from=2025-01-01&to=2025-12-31&page=1&limit=10", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Data) != 1 {
		t.Errorf("Data length = %d, want 1", len(result.Data))
	}
}

// TestSearchPaginated_NoKeyword tests search without keyword (browse with filters)
func TestSearchPaginated_NoKeyword(t *testing.T) {
	t.Parallel()

	now := time.Now()
	articlesWithSrc := []repository.ArticleWithSource{
		{
			Article: &entity.Article{
				ID:          1,
				SourceID:    5,
				Title:       "Article",
				URL:         "https://example.com/article1",
				Summary:     "Summary",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Tech Blog",
		},
	}

	stub := &stubSearchPaginatedRepo{
		articlesWithSrc: articlesWithSrc,
		totalCount:      1,
	}

	handler := article.SearchPaginatedHandler{
		Svc:           artUC.Service{Repo: stub},
		PaginationCfg: pagination.DefaultConfig(),
	}

	// No keyword - browse with source_id filter only
	req := httptest.NewRequest(http.MethodGet, "/articles/search?source_id=5", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result article.PaginatedResponse
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result.Data) != 1 {
		t.Errorf("Data length = %d, want 1", len(result.Data))
	}
}
