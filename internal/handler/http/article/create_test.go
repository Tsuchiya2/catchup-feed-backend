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
	artUC "catchup-feed/internal/usecase/article"
)

type stubCreateRepo struct {
	createErr   error
	lastArticle *entity.Article
}

func (s *stubCreateRepo) Create(_ context.Context, a *entity.Article) error {
	s.lastArticle = a
	return s.createErr
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubCreateRepo) List(_ context.Context) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubCreateRepo) Get(_ context.Context, _ int64) (*entity.Article, error) {
	return nil, nil
}
func (s *stubCreateRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubCreateRepo) Update(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubCreateRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubCreateRepo) ExistsByURL(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *stubCreateRepo) ExistsByURLBatch(_ context.Context, _ []string) (map[string]bool, error) {
	return nil, nil
}

func TestCreateHandler_Success(t *testing.T) {
	stub := &stubCreateRepo{}
	handler := article.CreateHandler{Svc: artUC.Service{Repo: stub}}

	body := `{
		"source_id": 1,
		"title": "New Article",
		"url": "https://example.com/new",
		"summary": "Test summary",
		"published_at": "2025-10-26T12:00:00Z"
	}`
	req := httptest.NewRequest(http.MethodPost, "/articles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusCreated)
	}

	// 入力値の検証
	if stub.lastArticle.SourceID != 1 {
		t.Errorf("SourceID = %d, want 1", stub.lastArticle.SourceID)
	}
	if stub.lastArticle.Title != "New Article" {
		t.Errorf("Title = %q, want %q", stub.lastArticle.Title, "New Article")
	}
	if stub.lastArticle.URL != "https://example.com/new" {
		t.Errorf("URL = %q, want %q", stub.lastArticle.URL, "https://example.com/new")
	}
}

func TestCreateHandler_MissingRequiredFields(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing source_id",
			body: `{"title": "Test", "url": "https://example.com"}`,
		},
		{
			name: "missing title",
			body: `{"source_id": 1, "url": "https://example.com"}`,
		},
		{
			name: "missing url",
			body: `{"source_id": 1, "title": "Test"}`,
		},
		{
			name: "empty title",
			body: `{"source_id": 1, "title": "", "url": "https://example.com"}`,
		},
		{
			name: "empty url",
			body: `{"source_id": 1, "title": "Test", "url": ""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubCreateRepo{}
			handler := article.CreateHandler{Svc: artUC.Service{Repo: stub}}

			req := httptest.NewRequest(http.MethodPost, "/articles", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestCreateHandler_InvalidJSON(t *testing.T) {
	stub := &stubCreateRepo{}
	handler := article.CreateHandler{Svc: artUC.Service{Repo: stub}}

	body := `{"source_id": 1, "title": "Test", "url":}`
	req := httptest.NewRequest(http.MethodPost, "/articles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestCreateHandler_ServiceError(t *testing.T) {
	stub := &stubCreateRepo{
		createErr: errors.New("database error"),
	}
	handler := article.CreateHandler{Svc: artUC.Service{Repo: stub}}

	body := `{
		"source_id": 1,
		"title": "New Article",
		"url": "https://example.com/new",
		"summary": "Test summary"
	}`
	req := httptest.NewRequest(http.MethodPost, "/articles", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}
