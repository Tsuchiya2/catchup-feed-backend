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
	"catchup-feed/internal/handler/http/auth"
	"catchup-feed/internal/handler/http/source"
	"catchup-feed/internal/repository"
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

// ListActive mirrors the SQL WHERE active = TRUE (D-27).
func (s *stubSourceRepo) ListActive(_ context.Context) ([]*entity.Source, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	active := make([]*entity.Source, 0, len(s.sources))
	for _, src := range s.sources {
		if src.Active {
			active = append(active, src)
		}
	}
	return active, nil
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubSourceRepo) Get(_ context.Context, _ int64) (*entity.Source, error) {
	return nil, nil
}
func (s *stubSourceRepo) Search(_ context.Context, _ string) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubSourceRepo) SearchWithFilters(_ context.Context, _ []string, _ repository.SourceSearchFilters) ([]*entity.Source, error) {
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

/* ───────── テストケース ───────── */

func TestListHandler_Success(t *testing.T) {
	now := time.Now()
	stub := &stubSourceRepo{
		sources: []*entity.Source{
			{
				ID:        1,
				Name:      "Tech Blog",
				FeedURL:   "https://example.com/feed",
				Category:  "dev",
				Kind:      entity.SourceKindRSS,
				CreatedAt: now,
				Active:    true,
			},
			{
				ID:        2,
				Name:      "News Site",
				FeedURL:   "https://news.example.com/rss",
				Category:  "dev",
				Kind:      entity.SourceKindPodcast,
				CreatedAt: now,
				Active:    false,
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
	if result[0].Kind != entity.SourceKindRSS {
		t.Errorf("result[0].Kind = %q, want %q", result[0].Kind, entity.SourceKindRSS)
	}
	if result[1].ID != 2 {
		t.Errorf("result[1].ID = %d, want 2", result[1].ID)
	}
	if result[1].Active != false {
		t.Errorf("result[1].Active = %v, want false", result[1].Active)
	}
	if result[1].Kind != entity.SourceKindPodcast {
		t.Errorf("result[1].Kind = %q, want %q", result[1].Kind, entity.SourceKindPodcast)
	}
}

// TestListHandler_RoleFiltering は D-27 のサーバー側強制フィルタの検証:
// viewer ロールのリクエストにはアクティブなソースのみ返り、admin と
// ロールなし(直接ハンドラ呼び出し=既存テスト経路)は全件返る。
func TestListHandler_RoleFiltering(t *testing.T) {
	now := time.Now()
	stub := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, Name: "Active Blog", FeedURL: "https://a.example.com/feed", Category: "dev", Kind: entity.SourceKindRSS, CreatedAt: now, Active: true},
			{ID: 2, Name: "Inactive Blog", FeedURL: "https://b.example.com/feed", Category: "dev", Kind: entity.SourceKindRSS, CreatedAt: now, Active: false},
		},
	}
	handler := source.ListHandler{Svc: srcUC.Service{Repo: stub}}

	tests := []struct {
		name      string
		role      string // "" = context に role を載せない
		wantIDs   []int64
		wantCount int
	}{
		{name: "viewer sees active only", role: auth.RoleViewer, wantIDs: []int64{1}, wantCount: 1},
		{name: "admin sees everything", role: auth.RoleAdmin, wantIDs: []int64{1, 2}, wantCount: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/sources", nil)
			if tt.role != "" {
				req = req.WithContext(auth.WithIdentity(req.Context(), "user@example.com", tt.role))
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
			}
			var result []source.DTO
			if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}
			if len(result) != tt.wantCount {
				t.Fatalf("result length = %d, want %d", len(result), tt.wantCount)
			}
			for i, id := range tt.wantIDs {
				if result[i].ID != id {
					t.Errorf("result[%d].ID = %d, want %d", i, result[i].ID, id)
				}
			}
		})
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
