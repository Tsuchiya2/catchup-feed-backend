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

/* ───────── モック実装 ───────── */

type stubArticleRepo struct {
	articles []*entity.Article
	listErr  error
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

/* ───────── テストケース ───────── */

func TestListHandler_Success(t *testing.T) {
	now := time.Now()
	stub := &stubArticleRepo{
		articles: []*entity.Article{
			{
				ID:          1,
				SourceID:    10,
				Title:       "Test Article 1",
				URL:         "https://example.com/article1",
				Summary:     "Summary 1",
				PublishedAt: now,
				CreatedAt:   now,
			},
			{
				ID:          2,
				SourceID:    10,
				Title:       "Test Article 2",
				URL:         "https://example.com/article2",
				Summary:     "Summary 2",
				PublishedAt: now,
				CreatedAt:   now,
			},
		},
	}

	handler := article.ListHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
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

	// 結果の検証
	if len(result) != 2 {
		t.Fatalf("result length = %d, want 2", len(result))
	}
	if result[0].ID != 1 {
		t.Errorf("result[0].ID = %d, want 1", result[0].ID)
	}
	if result[0].Title != "Test Article 1" {
		t.Errorf("result[0].Title = %q, want %q", result[0].Title, "Test Article 1")
	}
	if result[1].ID != 2 {
		t.Errorf("result[1].ID = %d, want 2", result[1].ID)
	}
}

func TestListHandler_EmptyList(t *testing.T) {
	stub := &stubArticleRepo{
		articles: []*entity.Article{},
	}

	handler := article.ListHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
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

	// 空のリストが返ることを確認
	if len(result) != 0 {
		t.Fatalf("result length = %d, want 0", len(result))
	}
}

func TestListHandler_Error(t *testing.T) {
	stub := &stubArticleRepo{
		listErr: errors.New("database error"),
	}

	handler := article.ListHandler{Svc: artUC.Service{Repo: stub}}

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// エラー時は500を返す
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}
