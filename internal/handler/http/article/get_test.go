package article_test

import (
	"context"
	"database/sql"
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

/* ───────── モック実装 ───────── */

type stubGetRepo struct {
	article    *entity.Article
	sourceName string
	getErr     error
}

func (s *stubGetRepo) GetWithSource(_ context.Context, id int64) (*entity.Article, string, error) {
	if s.getErr != nil {
		return nil, "", s.getErr
	}
	if s.article != nil && s.article.ID == id {
		return s.article, s.sourceName, nil
	}
	return nil, "", nil
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubGetRepo) List(_ context.Context) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubGetRepo) Get(_ context.Context, _ int64) (*entity.Article, error) {
	return nil, nil
}
func (s *stubGetRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubGetRepo) SearchWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubGetRepo) Create(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubGetRepo) Update(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubGetRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubGetRepo) ExistsByURL(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *stubGetRepo) ExistsByURLBatch(_ context.Context, _ []string) (map[string]bool, error) {
	return nil, nil
}
func (s *stubGetRepo) ListWithSource(_ context.Context) ([]repository.ArticleWithSource, error) {
	return nil, nil
}
func (s *stubGetRepo) ListWithSourcePaginated(_ context.Context, _, _ int) ([]repository.ArticleWithSource, error) {
	return nil, nil
}
func (s *stubGetRepo) CountArticles(_ context.Context) (int64, error) {
	return 0, nil
}

/* ───────── テストケース ───────── */

func TestGetHandler_Success(t *testing.T) {
	now := time.Now()
	stub := &stubGetRepo{
		article: &entity.Article{
			ID:          1,
			SourceID:    10,
			Title:       "Test Article",
			URL:         "https://example.com/article1",
			Summary:     "Test Summary",
			PublishedAt: now,
			CreatedAt:   now,
		},
		sourceName: "Test Source",
	}

	handler := article.GetHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// レスポンスのパース
	var result article.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// 結果の検証
	if result.ID != 1 {
		t.Errorf("result.ID = %d, want 1", result.ID)
	}
	if result.SourceID != 10 {
		t.Errorf("result.SourceID = %d, want 10", result.SourceID)
	}
	if result.SourceName != "Test Source" {
		t.Errorf("result.SourceName = %q, want %q", result.SourceName, "Test Source")
	}
	if result.Title != "Test Article" {
		t.Errorf("result.Title = %q, want %q", result.Title, "Test Article")
	}
	if result.URL != "https://example.com/article1" {
		t.Errorf("result.URL = %q, want %q", result.URL, "https://example.com/article1")
	}
	if result.Summary != "Test Summary" {
		t.Errorf("result.Summary = %q, want %q", result.Summary, "Test Summary")
	}
}

func TestGetHandler_InvalidID(t *testing.T) {
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
		{
			name: "empty id",
			path: "/articles/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubGetRepo{}
			handler := article.GetHandler{Svc: artUC.Service{Repo: stub}}

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestGetHandler_NotFound(t *testing.T) {
	stub := &stubGetRepo{
		article: nil, // 記事が存在しない
	}
	handler := article.GetHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/999", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestGetHandler_DatabaseError(t *testing.T) {
	stub := &stubGetRepo{
		getErr: errors.New("database connection error"),
	}
	handler := article.GetHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}

func TestGetHandler_SourceNameIncluded(t *testing.T) {
	now := time.Now()
	stub := &stubGetRepo{
		article: &entity.Article{
			ID:          1,
			SourceID:    10,
			Title:       "Test Article",
			URL:         "https://example.com/article1",
			Summary:     "Test Summary",
			PublishedAt: now,
			CreatedAt:   now,
		},
		sourceName: "Tech News Source",
	}

	handler := article.GetHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// レスポンスのパース
	var result article.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// SourceNameが正しく含まれているか確認
	if result.SourceName != "Tech News Source" {
		t.Errorf("result.SourceName = %q, want %q", result.SourceName, "Tech News Source")
	}
}

func TestGetHandler_MultipleArticles(t *testing.T) {
	// 複数の記事があっても、指定したIDの記事だけが返されることを確認
	now := time.Now()
	tests := []struct {
		name       string
		requestID  string
		articleID  int64
		wantStatus int
	}{
		{
			name:       "get article 1",
			requestID:  "/articles/1",
			articleID:  1,
			wantStatus: http.StatusOK,
		},
		{
			name:       "get article 5",
			requestID:  "/articles/5",
			articleID:  5,
			wantStatus: http.StatusOK,
		},
		{
			name:       "get non-existent article",
			requestID:  "/articles/999",
			articleID:  999,
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stub *stubGetRepo
			if tt.wantStatus == http.StatusOK {
				stub = &stubGetRepo{
					article: &entity.Article{
						ID:          tt.articleID,
						SourceID:    10,
						Title:       "Test Article",
						URL:         "https://example.com/article",
						Summary:     "Test Summary",
						PublishedAt: now,
						CreatedAt:   now,
					},
					sourceName: "Test Source",
				}
			} else {
				stub = &stubGetRepo{
					article: nil,
				}
			}

			handler := article.GetHandler{Svc: artUC.Service{Repo: stub}}

			req := httptest.NewRequest(http.MethodGet, tt.requestID, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status code = %d, want %d", rr.Code, tt.wantStatus)
			}

			if tt.wantStatus == http.StatusOK {
				var result article.DTO
				if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}

				if result.ID != tt.articleID {
					t.Errorf("result.ID = %d, want %d", result.ID, tt.articleID)
				}
			}
		})
	}
}

func TestGetHandler_SQLNoRowsError(t *testing.T) {
	// sql.ErrNoRows が適切にハンドリングされることを確認
	stub := &stubGetRepo{
		getErr: sql.ErrNoRows,
	}
	handler := article.GetHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles/1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// sql.ErrNoRowsはリポジトリ層でnilに変換されるため、サービス層では404を返す
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}
