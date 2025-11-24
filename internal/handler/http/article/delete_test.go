package article_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/article"
	artUC "catchup-feed/internal/usecase/article"
)

type stubDeleteRepo struct {
	deleteErr error
	deleted   bool
	deletedID int64
}

func (s *stubDeleteRepo) Delete(_ context.Context, id int64) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = true
	s.deletedID = id
	return nil
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubDeleteRepo) List(_ context.Context) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubDeleteRepo) Get(_ context.Context, _ int64) (*entity.Article, error) {
	return nil, nil
}
func (s *stubDeleteRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubDeleteRepo) Create(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubDeleteRepo) Update(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubDeleteRepo) ExistsByURL(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *stubDeleteRepo) ExistsByURLBatch(_ context.Context, _ []string) (map[string]bool, error) {
	return nil, nil
}

func TestDeleteHandler_Success(t *testing.T) {
	stub := &stubDeleteRepo{}
	handler := article.DeleteHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodDelete, "/articles/1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusNoContent)
	}

	if !stub.deleted {
		t.Error("Delete was not called")
	}
	if stub.deletedID != 1 {
		t.Errorf("deleted ID = %d, want 1", stub.deletedID)
	}
}

func TestDeleteHandler_InvalidID(t *testing.T) {
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
			stub := &stubDeleteRepo{}
			handler := article.DeleteHandler{Svc: artUC.Service{Repo: stub}}

			req := httptest.NewRequest(http.MethodDelete, tt.path, nil)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
			}

			if stub.deleted {
				t.Error("Delete should not be called for invalid ID")
			}
		})
	}
}

func TestDeleteHandler_DeleteError(t *testing.T) {
	stub := &stubDeleteRepo{
		deleteErr: errors.New("database error"),
	}
	handler := article.DeleteHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodDelete, "/articles/1", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}
