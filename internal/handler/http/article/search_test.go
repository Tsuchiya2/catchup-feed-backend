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
	artUC "catchup-feed/internal/usecase/article"
)

type stubSearchRepo struct {
	articles  []*entity.Article
	searchErr error
}

func (s *stubSearchRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
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
