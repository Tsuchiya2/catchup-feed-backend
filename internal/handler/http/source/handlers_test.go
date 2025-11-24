package source_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/source"
	srcUC "catchup-feed/internal/usecase/source"
)

/* ───────── Create Handler テスト ───────── */

type stubCreateRepo struct {
	createErr  error
	lastSource *entity.Source
}

func (s *stubCreateRepo) Create(_ context.Context, src *entity.Source) error {
	s.lastSource = src
	return s.createErr
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubCreateRepo) Get(_ context.Context, _ int64) (*entity.Source, error) {
	return nil, nil
}
func (s *stubCreateRepo) List(_ context.Context) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubCreateRepo) ListActive(_ context.Context) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubCreateRepo) Search(_ context.Context, _ string) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubCreateRepo) Update(_ context.Context, _ *entity.Source) error {
	return nil
}
func (s *stubCreateRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubCreateRepo) TouchCrawledAt(_ context.Context, _ int64, _ time.Time) error {
	return nil
}

func TestCreateHandler_Success(t *testing.T) {
	stub := &stubCreateRepo{}
	handler := source.CreateHandler{Svc: srcUC.Service{Repo: stub}}

	body := `{
		"name": "Tech Blog",
		"feedURL": "https://example.com/feed"
	}`
	req := httptest.NewRequest(http.MethodPost, "/sources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusCreated)
	}

	if stub.lastSource.Name != "Tech Blog" {
		t.Errorf("Name = %q, want %q", stub.lastSource.Name, "Tech Blog")
	}
	if stub.lastSource.FeedURL != "https://example.com/feed" {
		t.Errorf("FeedURL = %q, want %q", stub.lastSource.FeedURL, "https://example.com/feed")
	}
}

func TestCreateHandler_MissingFields(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing name",
			body: `{"feedURL": "https://example.com/feed"}`,
		},
		{
			name: "missing feedURL",
			body: `{"name": "Test"}`,
		},
		{
			name: "empty name",
			body: `{"name": "", "feedURL": "https://example.com/feed"}`,
		},
		{
			name: "empty feedURL",
			body: `{"name": "Test", "feedURL": ""}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubCreateRepo{}
			handler := source.CreateHandler{Svc: srcUC.Service{Repo: stub}}

			req := httptest.NewRequest(http.MethodPost, "/sources", strings.NewReader(tt.body))
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
	handler := source.CreateHandler{Svc: srcUC.Service{Repo: stub}}

	body := `{"name": "Test", "feedURL":}`
	req := httptest.NewRequest(http.MethodPost, "/sources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

/* ───────── Update Handler テスト ───────── */

type stubUpdateRepo struct {
	source    *entity.Source
	updateErr error
	getErr    error
}

func (s *stubUpdateRepo) Get(_ context.Context, id int64) (*entity.Source, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.source != nil && s.source.ID == id {
		return s.source, nil
	}
	return nil, nil
}

func (s *stubUpdateRepo) Update(_ context.Context, src *entity.Source) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	s.source = src
	return nil
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubUpdateRepo) List(_ context.Context) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubUpdateRepo) ListActive(_ context.Context) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubUpdateRepo) Search(_ context.Context, _ string) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubUpdateRepo) Create(_ context.Context, _ *entity.Source) error {
	return nil
}
func (s *stubUpdateRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubUpdateRepo) TouchCrawledAt(_ context.Context, _ int64, _ time.Time) error {
	return nil
}

func TestUpdateHandler_Success(t *testing.T) {
	stub := &stubUpdateRepo{
		source: &entity.Source{
			ID:      1,
			Name:    "Old Name",
			FeedURL: "https://example.com/old",
			Active:  true,
		},
	}
	handler := source.UpdateHandler{Svc: srcUC.Service{Repo: stub}}

	body := `{
		"name": "Updated Name",
		"feedURL": "https://example.com/new"
	}`
	req := httptest.NewRequest(http.MethodPut, "/sources/1", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusNoContent)
	}

	if stub.source.Name != "Updated Name" {
		t.Errorf("Name = %q, want %q", stub.source.Name, "Updated Name")
	}
}

func TestUpdateHandler_InvalidID(t *testing.T) {
	stub := &stubUpdateRepo{}
	handler := source.UpdateHandler{Svc: srcUC.Service{Repo: stub}}

	body := `{"name": "Test"}`
	req := httptest.NewRequest(http.MethodPut, "/sources/0", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestUpdateHandler_NotFound(t *testing.T) {
	stub := &stubUpdateRepo{
		source: nil, // ソースが存在しない
	}
	handler := source.UpdateHandler{Svc: srcUC.Service{Repo: stub}}

	body := `{"name": "Test"}`
	req := httptest.NewRequest(http.MethodPut, "/sources/999", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

/* ───────── Delete Handler テスト ───────── */

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
func (s *stubDeleteRepo) Get(_ context.Context, _ int64) (*entity.Source, error) {
	return nil, nil
}
func (s *stubDeleteRepo) List(_ context.Context) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubDeleteRepo) ListActive(_ context.Context) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubDeleteRepo) Search(_ context.Context, _ string) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubDeleteRepo) Create(_ context.Context, _ *entity.Source) error {
	return nil
}
func (s *stubDeleteRepo) Update(_ context.Context, _ *entity.Source) error {
	return nil
}
func (s *stubDeleteRepo) TouchCrawledAt(_ context.Context, _ int64, _ time.Time) error {
	return nil
}

func TestDeleteHandler_Success(t *testing.T) {
	stub := &stubDeleteRepo{}
	handler := source.DeleteHandler{Svc: srcUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodDelete, "/sources/1", nil)
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
	stub := &stubDeleteRepo{}
	handler := source.DeleteHandler{Svc: srcUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodDelete, "/sources/0", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}

	if stub.deleted {
		t.Error("Delete should not be called for invalid ID")
	}
}

/* ───────── Search Handler テスト ───────── */

type stubSearchRepo struct {
	sources   []*entity.Source
	searchErr error
}

func (s *stubSearchRepo) Search(_ context.Context, _ string) ([]*entity.Source, error) {
	return s.sources, s.searchErr
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubSearchRepo) Get(_ context.Context, _ int64) (*entity.Source, error) {
	return nil, nil
}
func (s *stubSearchRepo) List(_ context.Context) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubSearchRepo) ListActive(_ context.Context) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubSearchRepo) Create(_ context.Context, _ *entity.Source) error {
	return nil
}
func (s *stubSearchRepo) Update(_ context.Context, _ *entity.Source) error {
	return nil
}
func (s *stubSearchRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubSearchRepo) TouchCrawledAt(_ context.Context, _ int64, _ time.Time) error {
	return nil
}

func TestSearchHandler_Success(t *testing.T) {
	now := time.Now()
	stub := &stubSearchRepo{
		sources: []*entity.Source{
			{
				ID:            1,
				Name:          "Tech Blog",
				FeedURL:       "https://example.com/feed",
				LastCrawledAt: &now,
				Active:        true,
			},
		},
	}
	handler := source.SearchHandler{Svc: srcUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/sources/search?keyword=tech", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result []source.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 1 {
		t.Fatalf("result length = %d, want 1", len(result))
	}
	if result[0].Name != "Tech Blog" {
		t.Errorf("Name = %q, want %q", result[0].Name, "Tech Blog")
	}
}

func TestSearchHandler_MissingKeyword(t *testing.T) {
	stub := &stubSearchRepo{}
	handler := source.SearchHandler{Svc: srcUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/sources/search", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestSearchHandler_EmptyResult(t *testing.T) {
	stub := &stubSearchRepo{
		sources: []*entity.Source{},
	}
	handler := source.SearchHandler{Svc: srcUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/sources/search?keyword=nonexistent", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	var result []source.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 0 {
		t.Fatalf("result length = %d, want 0", len(result))
	}
}
