package article_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/article"
	"catchup-feed/internal/repository"
	artUC "catchup-feed/internal/usecase/article"
)

type stubUpdateRepo struct {
	article   *entity.Article
	updateErr error
	getErr    error
}

func (s *stubUpdateRepo) Get(_ context.Context, id int64) (*entity.Article, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.article != nil && s.article.ID == id {
		return s.article, nil
	}
	return nil, nil
}

func (s *stubUpdateRepo) Update(_ context.Context, a *entity.Article) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.article = a
	return nil
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubUpdateRepo) List(_ context.Context) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubUpdateRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubUpdateRepo) SearchWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubUpdateRepo) Create(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubUpdateRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubUpdateRepo) ExistsByURL(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *stubUpdateRepo) ExistsByURLBatch(_ context.Context, _ []string) (map[string]bool, error) {
	return nil, nil
}
func (s *stubUpdateRepo) GetWithSource(_ context.Context, _ int64) (*entity.Article, string, error) {
	return nil, "", nil
}
func (s *stubUpdateRepo) ListWithSource(_ context.Context) ([]repository.ArticleWithSource, error) {
	return nil, nil
}
func (s *stubUpdateRepo) ListWithSourcePaginated(_ context.Context, _, _ int) ([]repository.ArticleWithSource, error) {
	return nil, nil
}
func (s *stubUpdateRepo) CountArticles(_ context.Context) (int64, error) {
	return 0, nil
}

func TestUpdateHandler_Success(t *testing.T) {
	stub := &stubUpdateRepo{
		article: &entity.Article{
			ID:       1,
			SourceID: 10,
			Title:    "Old Title",
			URL:      "https://example.com/old",
		},
	}
	handler := article.UpdateHandler{Svc: artUC.Service{Repo: stub}}

	newTitle := "Updated Title"
	body := `{
		"title": "Updated Title",
		"summary": "Updated summary"
	}`
	req := httptest.NewRequest(http.MethodPut, "/articles/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusNoContent)
	}

	// 更新されたことを確認
	if stub.article.Title != newTitle {
		t.Errorf("Title = %q, want %q", stub.article.Title, newTitle)
	}
}

func TestUpdateHandler_InvalidID(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{
			name: "zero id",
			path: "/articles/0",
		},
		{
			name: "negative id",
			path: "/articles/-1",
		},
		{
			name: "non-numeric id",
			path: "/articles/abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubUpdateRepo{}
			handler := article.UpdateHandler{Svc: artUC.Service{Repo: stub}}

			body := `{"title": "Test"}`
			req := httptest.NewRequest(http.MethodPut, tt.path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestUpdateHandler_NotFound(t *testing.T) {
	stub := &stubUpdateRepo{
		article: nil, // 記事が存在しない
	}
	handler := article.UpdateHandler{Svc: artUC.Service{Repo: stub}}

	body := `{"title": "Test"}`
	req := httptest.NewRequest(http.MethodPut, "/articles/999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestUpdateHandler_InvalidJSON(t *testing.T) {
	stub := &stubUpdateRepo{
		article: &entity.Article{ID: 1},
	}
	handler := article.UpdateHandler{Svc: artUC.Service{Repo: stub}}

	body := `{"title": "Test", "summary":}`
	req := httptest.NewRequest(http.MethodPut, "/articles/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestUpdateHandler_UpdateError(t *testing.T) {
	stub := &stubUpdateRepo{
		article:   &entity.Article{ID: 1},
		updateErr: errors.New("database error"),
	}
	handler := article.UpdateHandler{Svc: artUC.Service{Repo: stub}}

	body := `{"title": "Test"}`
	req := httptest.NewRequest(http.MethodPut, "/articles/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}
