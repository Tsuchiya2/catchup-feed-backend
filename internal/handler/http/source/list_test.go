package source_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/source"
	srcUC "catchup-feed/internal/usecase/source"
)

/* ───────── モック実装 ───────── */

type stubSourceRepo struct {
	sources []*entity.Source
	listErr error
}

func (s *stubSourceRepo) List(_ context.Context) ([]*entity.Source, error) {
	return s.sources, s.listErr
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubSourceRepo) Get(_ context.Context, _ int64) (*entity.Source, error) {
	return nil, nil
}
func (s *stubSourceRepo) ListActive(_ context.Context) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubSourceRepo) Search(_ context.Context, _ string) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubSourceRepo) Create(_ context.Context, _ *entity.Source) error {
	return nil
}
func (s *stubSourceRepo) Update(_ context.Context, _ *entity.Source) error {
	return nil
}
func (s *stubSourceRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubSourceRepo) TouchCrawledAt(_ context.Context, _ int64, _ time.Time) error {
	return nil
}

/* ───────── テストケース ───────── */

func TestListHandler_Success(t *testing.T) {
	now := time.Now()
	stub := &stubSourceRepo{
		sources: []*entity.Source{
			{
				ID:            1,
				Name:          "Tech Blog",
				FeedURL:       "https://example.com/feed",
				LastCrawledAt: &now,
				Active:        true,
			},
			{
				ID:            2,
				Name:          "News Site",
				FeedURL:       "https://news.example.com/rss",
				LastCrawledAt: &now,
				Active:        false,
			},
		},
	}

	handler := source.ListHandler{Svc: srcUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/sources", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// レスポンスのパース
	var result []source.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// 結果の検証
	if len(result) != 2 {
		t.Fatalf("result length = %d, want 2", len(result))
	}
	if result[0].ID != 1 {
		t.Errorf("result[0].ID = %d, want 1", result[0].ID)
	}
	if result[0].Name != "Tech Blog" {
		t.Errorf("result[0].Name = %q, want %q", result[0].Name, "Tech Blog")
	}
	if result[0].Active != true {
		t.Errorf("result[0].Active = %v, want true", result[0].Active)
	}
	if result[1].ID != 2 {
		t.Errorf("result[1].ID = %d, want 2", result[1].ID)
	}
	if result[1].Active != false {
		t.Errorf("result[1].Active = %v, want false", result[1].Active)
	}
}

func TestListHandler_EmptyList(t *testing.T) {
	stub := &stubSourceRepo{
		sources: []*entity.Source{},
	}

	handler := source.ListHandler{Svc: srcUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/sources", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
	}

	// レスポンスのパース
	var result []source.DTO
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// 空のリストが返ることを確認
	if len(result) != 0 {
		t.Fatalf("result length = %d, want 0", len(result))
	}
}

func TestListHandler_Error(t *testing.T) {
	stub := &stubSourceRepo{
		listErr: errors.New("database error"),
	}

	handler := source.ListHandler{Svc: srcUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/sources", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// エラー時は500を返す
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}
