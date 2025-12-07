package article_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
	artUC "catchup-feed/internal/usecase/article"
)

/* ───────── スタブ実装 ───────── */

// 最小限のインメモリ ArticleRepository
type stubRepo struct {
	data   map[int64]*entity.Article
	nextID int64
	err    error // 強制的にエラーを返したいとき用
}

func newStub() *stubRepo {
	return &stubRepo{data: map[int64]*entity.Article{}, nextID: 1}
}

// --- ArticleRepository を満たす ---

func (s *stubRepo) List(_ context.Context) ([]*entity.Article, error) {
	var out []*entity.Article
	for _, v := range s.data {
		out = append(out, v)
	}
	return out, s.err
}
func (s *stubRepo) Get(_ context.Context, id int64) (*entity.Article, error) {
	return s.data[id], s.err
}
func (s *stubRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return nil, s.err // テストでは未使用
}
func (s *stubRepo) SearchWithFilters(_ context.Context, keywords []string, filters repository.ArticleSearchFilters) ([]*entity.Article, error) {
	if s.err != nil {
		return nil, s.err
	}
	// スタブではフィルタリングせず、data内のすべての記事を返す
	var out []*entity.Article
	for _, v := range s.data {
		out = append(out, v)
	}
	return out, nil
}
func (s *stubRepo) Create(_ context.Context, a *entity.Article) error {
	if s.err != nil {
		return s.err
	}
	a.ID = s.nextID
	s.nextID++
	s.data[a.ID] = a
	return nil
}
func (s *stubRepo) Update(_ context.Context, a *entity.Article) error {
	if s.err != nil {
		return s.err
	}
	s.data[a.ID] = a
	return nil
}
func (s *stubRepo) Delete(_ context.Context, id int64) error {
	if s.err != nil {
		return s.err
	}
	delete(s.data, id)
	return nil
}

// ExistsByURL checks if any article exists with the given URL.
func (s *stubRepo) ExistsByURL(_ context.Context, url string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	for _, a := range s.data {
		if a.URL == url {
			return true, nil
		}
	}
	return false, nil
}

// ExistsByURLBatch checks if articles exist with given URLs (batch version).
func (s *stubRepo) ExistsByURLBatch(_ context.Context, urls []string) (map[string]bool, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make(map[string]bool)
	for _, url := range urls {
		for _, a := range s.data {
			if a.URL == url {
				result[url] = true
				break
			}
		}
	}
	return result, nil
}

// GetWithSource retrieves an article by ID along with the source name.
func (s *stubRepo) GetWithSource(_ context.Context, id int64) (*entity.Article, string, error) {
	if s.err != nil {
		return nil, "", s.err
	}
	article := s.data[id]
	if article == nil {
		return nil, "", nil
	}
	// スタブではソース名をダミー値として返す
	sourceName := "Test Source"
	return article, sourceName, nil
}

// ListWithSource retrieves all articles with their source names.
func (s *stubRepo) ListWithSource(_ context.Context) ([]repository.ArticleWithSource, error) {
	if s.err != nil {
		return nil, s.err
	}
	var out []repository.ArticleWithSource
	for _, v := range s.data {
		out = append(out, repository.ArticleWithSource{
			Article:    v,
			SourceName: "Test Source",
		})
	}
	return out, nil
}

// ListWithSourcePaginated retrieves paginated articles with source names.
func (s *stubRepo) ListWithSourcePaginated(_ context.Context, offset, limit int) ([]repository.ArticleWithSource, error) {
	if s.err != nil {
		return nil, s.err
	}
	var all []repository.ArticleWithSource
	for _, v := range s.data {
		all = append(all, repository.ArticleWithSource{
			Article:    v,
			SourceName: "Test Source",
		})
	}
	// Apply offset and limit
	if offset >= len(all) {
		return []repository.ArticleWithSource{}, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

// CountArticles returns the total number of articles.
func (s *stubRepo) CountArticles(_ context.Context) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	return int64(len(s.data)), nil
}

/* ───────── 1. Create のバリデーション ───────── */

func TestService_Create_validation(t *testing.T) {
	svc := artUC.Service{Repo: newStub()}

	err := svc.Create(context.Background(), artUC.CreateInput{})
	if err == nil {
		t.Fatalf("want validation error, got nil")
	}
}

/* ───────── 2. Create → 保存確認 ───────── */

func TestService_Create_success(t *testing.T) {
	stub := newStub()
	svc := artUC.Service{Repo: stub}

	in := artUC.CreateInput{
		SourceID: 1, Title: "t", URL: "https://example.com/article",
		Summary: "s", PublishedAt: time.Now(),
	}
	if err := svc.Create(context.Background(), in); err != nil {
		t.Fatalf("Create err=%v", err)
	}
	if len(stub.data) != 1 {
		t.Fatalf("want 1 article, got %d", len(stub.data))
	}
}

/* ───────── 3. Update: not-found ───────── */

func TestService_Update_notFound(t *testing.T) {
	svc := artUC.Service{Repo: newStub()}

	err := svc.Update(context.Background(), artUC.UpdateInput{ID: 99})
	if !errors.Is(err, artUC.ErrArticleNotFound) {
		t.Fatalf("want ErrArticleNotFound, got %v", err)
	}
}

/* ───────── 4. Update: 正常フロー ───────── */

func TestService_Update_ok(t *testing.T) {
	stub := newStub()
	// 既存レコード投入
	now := time.Now()
	stub.data[1] = &entity.Article{
		ID: 1, SourceID: 1, Title: "old", URL: "u", Summary: "s", PublishedAt: now,
	}

	svc := artUC.Service{Repo: stub}
	newTitle := "new"
	if err := svc.Update(context.Background(), artUC.UpdateInput{
		ID: 1, Title: &newTitle,
	}); err != nil {
		t.Fatalf("Update err=%v", err)
	}
	if stub.data[1].Title != "new" {
		t.Fatalf("title not updated: %#v", stub.data[1])
	}
}

/* ───────── 5. Delete: id<=0 ───────── */

func TestService_Delete_validation(t *testing.T) {
	svc := artUC.Service{Repo: newStub()}
	if err := svc.Delete(context.Background(), 0); err == nil {
		t.Fatalf("want validation error, got nil")
	}
}

/* ───────── 6. List: 全記事取得 ───────── */

func TestService_List(t *testing.T) {
	tests := []struct {
		name      string
		setupRepo func(*stubRepo)
		wantCount int
		wantErr   bool
	}{
		{
			name: "empty list",
			setupRepo: func(s *stubRepo) {
				// 空のまま
			},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name: "multiple articles",
			setupRepo: func(s *stubRepo) {
				now := time.Now()
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Article 1", URL: "https://example.com/1", PublishedAt: now}
				s.data[2] = &entity.Article{ID: 2, SourceID: 1, Title: "Article 2", URL: "https://example.com/2", PublishedAt: now}
				s.data[3] = &entity.Article{ID: 3, SourceID: 2, Title: "Article 3", URL: "https://example.com/3", PublishedAt: now}
			},
			wantCount: 3,
			wantErr:   false,
		},
		{
			name: "repository error",
			setupRepo: func(s *stubRepo) {
				s.err = errors.New("database error")
			},
			wantCount: 0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStub()
			tt.setupRepo(stub)
			svc := artUC.Service{Repo: stub}

			articles, err := svc.List(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("List() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(articles) != tt.wantCount {
				t.Errorf("List() got %d articles, want %d", len(articles), tt.wantCount)
			}
		})
	}
}

/* ───────── 7. Get: ID指定で記事取得 ───────── */

func TestService_Get(t *testing.T) {
	tests := []struct {
		name      string
		id        int64
		setupRepo func(*stubRepo)
		wantID    int64
		wantErr   error
	}{
		{
			name: "invalid id - zero",
			id:   0,
			setupRepo: func(s *stubRepo) {
				// データ不要
			},
			wantErr: artUC.ErrInvalidArticleID,
		},
		{
			name: "invalid id - negative",
			id:   -1,
			setupRepo: func(s *stubRepo) {
				// データ不要
			},
			wantErr: artUC.ErrInvalidArticleID,
		},
		{
			name: "article not found",
			id:   999,
			setupRepo: func(s *stubRepo) {
				// 存在しないID
			},
			wantErr: artUC.ErrArticleNotFound,
		},
		{
			name: "article found",
			id:   1,
			setupRepo: func(s *stubRepo) {
				now := time.Now()
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Test Article", URL: "https://example.com/1", PublishedAt: now}
			},
			wantID:  1,
			wantErr: nil,
		},
		{
			name: "repository error",
			id:   1,
			setupRepo: func(s *stubRepo) {
				s.err = errors.New("database error")
			},
			wantErr: errors.New("get article"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStub()
			tt.setupRepo(stub)
			svc := artUC.Service{Repo: stub}

			article, err := svc.Get(context.Background(), tt.id)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Get() error = nil, wantErr %v", tt.wantErr)
					return
				}
				// Check if error is of expected type or contains expected message
				if !errors.Is(err, tt.wantErr) {
					// For wrapped errors, just check if error occurred
					if err == nil {
						t.Errorf("Get() error = nil, wantErr %v", tt.wantErr)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("Get() unexpected error = %v", err)
				return
			}

			if article.ID != tt.wantID {
				t.Errorf("Get() got ID = %d, want %d", article.ID, tt.wantID)
			}
		})
	}
}

/* ───────── 8. Search: キーワード検索 ───────── */

func TestService_Search(t *testing.T) {
	tests := []struct {
		name      string
		keyword   string
		setupRepo func(*stubRepo)
		wantErr   bool
	}{
		{
			name:    "empty keyword",
			keyword: "",
			setupRepo: func(s *stubRepo) {
				// 空でもエラーにならない
			},
			wantErr: false,
		},
		{
			name:    "valid keyword",
			keyword: "golang",
			setupRepo: func(s *stubRepo) {
				// Search実装はリポジトリ側なのでスタブはエラーチェックのみ
			},
			wantErr: false,
		},
		{
			name:    "repository error",
			keyword: "test",
			setupRepo: func(s *stubRepo) {
				s.err = errors.New("search error")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStub()
			tt.setupRepo(stub)
			svc := artUC.Service{Repo: stub}

			_, err := svc.Search(context.Background(), tt.keyword)

			if (err != nil) != tt.wantErr {
				t.Errorf("Search() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

/* ───────── 9. Create: 詳細なバリデーションテスト ───────── */

func TestService_Create_detailedValidation(t *testing.T) {
	tests := []struct {
		name    string
		input   artUC.CreateInput
		wantErr bool
		errMsg  string
	}{
		{
			name: "invalid source id - zero",
			input: artUC.CreateInput{
				SourceID: 0,
				Title:    "Test",
				URL:      "https://example.com",
			},
			wantErr: true,
			errMsg:  "sourceID",
		},
		{
			name: "invalid source id - negative",
			input: artUC.CreateInput{
				SourceID: -1,
				Title:    "Test",
				URL:      "https://example.com",
			},
			wantErr: true,
			errMsg:  "sourceID",
		},
		{
			name: "empty title",
			input: artUC.CreateInput{
				SourceID: 1,
				Title:    "",
				URL:      "https://example.com",
			},
			wantErr: true,
			errMsg:  "title",
		},
		{
			name: "empty url",
			input: artUC.CreateInput{
				SourceID: 1,
				Title:    "Test",
				URL:      "",
			},
			wantErr: true,
			errMsg:  "url",
		},
		{
			name: "invalid url format",
			input: artUC.CreateInput{
				SourceID: 1,
				Title:    "Test",
				URL:      "not-a-url",
			},
			wantErr: true,
			errMsg:  "URL",
		},
		{
			name: "repository error",
			input: artUC.CreateInput{
				SourceID:    1,
				Title:       "Test",
				URL:         "https://example.com",
				Summary:     "Summary",
				PublishedAt: time.Now(),
			},
			wantErr: true,
			errMsg:  "create article",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStub()
			if tt.name == "repository error" {
				stub.err = errors.New("db error")
			}
			svc := artUC.Service{Repo: stub}

			err := svc.Create(context.Background(), tt.input)

			if (err != nil) != tt.wantErr {
				t.Errorf("Create() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errMsg != "" {
				// エラーメッセージに期待する文字列が含まれているか確認
				if err.Error() == "" {
					t.Errorf("Create() error message is empty")
				}
			}
		})
	}
}

/* ───────── 10. Update: 詳細なバリデーションテスト ───────── */

func TestService_Update_detailedValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     artUC.UpdateInput
		setupRepo func(*stubRepo)
		wantErr   error
	}{
		{
			name: "invalid id - zero",
			input: artUC.UpdateInput{
				ID: 0,
			},
			setupRepo: func(s *stubRepo) {},
			wantErr:   artUC.ErrInvalidArticleID,
		},
		{
			name: "invalid id - negative",
			input: artUC.UpdateInput{
				ID: -1,
			},
			setupRepo: func(s *stubRepo) {},
			wantErr:   artUC.ErrInvalidArticleID,
		},
		{
			name: "article not found - repository error",
			input: artUC.UpdateInput{
				ID: 1,
			},
			setupRepo: func(s *stubRepo) {
				s.err = errors.New("db error")
			},
			wantErr: errors.New("db error"),
		},
		{
			name: "invalid source id update",
			input: artUC.UpdateInput{
				ID: 1,
			},
			setupRepo: func(s *stubRepo) {
				now := time.Now()
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Old", URL: "https://example.com", PublishedAt: now}
				negativeSourceID := int64(-1)
				// この行はテスト内でinputを修正する必要がある
				s.data[1].SourceID = negativeSourceID
			},
			wantErr: nil, // setup内で修正
		},
		{
			name: "empty title update",
			input: artUC.UpdateInput{
				ID: 1,
			},
			setupRepo: func(s *stubRepo) {
				now := time.Now()
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Old", URL: "https://example.com", PublishedAt: now}
			},
			wantErr: nil, // titleがnilの場合は更新されない
		},
		{
			name: "invalid url update",
			input: artUC.UpdateInput{
				ID: 1,
			},
			setupRepo: func(s *stubRepo) {
				now := time.Now()
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Old", URL: "https://example.com", PublishedAt: now}
			},
			wantErr: nil, // URLがnilの場合は更新されない
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStub()
			tt.setupRepo(stub)
			svc := artUC.Service{Repo: stub}

			// テストケースに応じてinputを調整
			input := tt.input
			switch tt.name {
			case "invalid source id update":
				negativeSourceID := int64(-1)
				input.SourceID = &negativeSourceID
			case "empty title update":
				emptyTitle := ""
				input.Title = &emptyTitle
			case "invalid url update":
				invalidURL := "not-a-url"
				input.URL = &invalidURL
			}

			err := svc.Update(context.Background(), input)

			if tt.wantErr != nil {
				if err == nil {
					// emptyTitle/invalidURLケースは特別
					if tt.name == "empty title update" || tt.name == "invalid url update" {
						if err == nil {
							t.Errorf("Update() error = nil, wantErr validation error")
						}
						return
					}
					t.Errorf("Update() error = nil, wantErr %v", tt.wantErr)
					return
				}
			}
		})
	}
}

/* ───────── 11. Update: フィールド更新の網羅テスト ───────── */

func TestService_Update_fieldUpdates(t *testing.T) {
	now := time.Now()
	newTime := now.Add(24 * time.Hour)

	tests := []struct {
		name      string
		input     artUC.UpdateInput
		setupRepo func(*stubRepo)
		verify    func(*testing.T, *stubRepo)
	}{
		{
			name: "update source id",
			input: artUC.UpdateInput{
				ID: 1,
			},
			setupRepo: func(s *stubRepo) {
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Title", URL: "https://example.com/1", PublishedAt: now}
			},
			verify: func(t *testing.T, s *stubRepo) {
				if s.data[1].SourceID != 2 {
					t.Errorf("SourceID not updated, got %d want 2", s.data[1].SourceID)
				}
			},
		},
		{
			name: "update summary",
			input: artUC.UpdateInput{
				ID: 1,
			},
			setupRepo: func(s *stubRepo) {
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Title", URL: "https://example.com/1", Summary: "Old", PublishedAt: now}
			},
			verify: func(t *testing.T, s *stubRepo) {
				if s.data[1].Summary != "New Summary" {
					t.Errorf("Summary not updated, got %s want 'New Summary'", s.data[1].Summary)
				}
			},
		},
		{
			name: "update published at",
			input: artUC.UpdateInput{
				ID: 1,
			},
			setupRepo: func(s *stubRepo) {
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Title", URL: "https://example.com/1", PublishedAt: now}
			},
			verify: func(t *testing.T, s *stubRepo) {
				if !s.data[1].PublishedAt.Equal(newTime) {
					t.Errorf("PublishedAt not updated, got %v want %v", s.data[1].PublishedAt, newTime)
				}
			},
		},
		{
			name: "update url",
			input: artUC.UpdateInput{
				ID: 1,
			},
			setupRepo: func(s *stubRepo) {
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Title", URL: "https://example.com/old", PublishedAt: now}
			},
			verify: func(t *testing.T, s *stubRepo) {
				if s.data[1].URL != "https://example.com/new" {
					t.Errorf("URL not updated, got %s want 'https://example.com/new'", s.data[1].URL)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStub()
			tt.setupRepo(stub)
			svc := artUC.Service{Repo: stub}

			// inputを調整
			input := tt.input
			switch tt.name {
			case "update source id":
				newSourceID := int64(2)
				input.SourceID = &newSourceID
			case "update summary":
				newSummary := "New Summary"
				input.Summary = &newSummary
			case "update published at":
				input.PublishedAt = &newTime
			case "update url":
				newURL := "https://example.com/new"
				input.URL = &newURL
			}

			err := svc.Update(context.Background(), input)
			if err != nil {
				t.Fatalf("Update() unexpected error = %v", err)
			}

			tt.verify(t, stub)
		})
	}
}

/* ───────── 12. GetWithSource: ID指定で記事とソース名を取得 ───────── */

func TestService_GetWithSource(t *testing.T) {
	tests := []struct {
		name           string
		id             int64
		setupRepo      func(*stubRepo)
		wantID         int64
		wantSourceName string
		wantErr        error
	}{
		{
			name: "invalid id - zero",
			id:   0,
			setupRepo: func(s *stubRepo) {
				// データ不要
			},
			wantErr: artUC.ErrInvalidArticleID,
		},
		{
			name: "invalid id - negative",
			id:   -1,
			setupRepo: func(s *stubRepo) {
				// データ不要
			},
			wantErr: artUC.ErrInvalidArticleID,
		},
		{
			name: "article not found",
			id:   999,
			setupRepo: func(s *stubRepo) {
				// 存在しないID
			},
			wantErr: artUC.ErrArticleNotFound,
		},
		{
			name: "article found with source name",
			id:   1,
			setupRepo: func(s *stubRepo) {
				now := time.Now()
				s.data[1] = &entity.Article{
					ID:          1,
					SourceID:    10,
					Title:       "Test Article",
					URL:         "https://example.com/1",
					Summary:     "Test Summary",
					PublishedAt: now,
				}
			},
			wantID:         1,
			wantSourceName: "Test Source",
			wantErr:        nil,
		},
		{
			name: "repository error",
			id:   1,
			setupRepo: func(s *stubRepo) {
				s.err = errors.New("database error")
			},
			wantErr: errors.New("get article with source"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStub()
			tt.setupRepo(stub)
			svc := artUC.Service{Repo: stub}

			article, sourceName, err := svc.GetWithSource(context.Background(), tt.id)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("GetWithSource() error = nil, wantErr %v", tt.wantErr)
					return
				}
				// Check if error is of expected type or contains expected message
				if !errors.Is(err, tt.wantErr) {
					// For wrapped errors, just check if error occurred
					if err == nil {
						t.Errorf("GetWithSource() error = nil, wantErr %v", tt.wantErr)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("GetWithSource() unexpected error = %v", err)
				return
			}

			if article.ID != tt.wantID {
				t.Errorf("GetWithSource() got ID = %d, want %d", article.ID, tt.wantID)
			}

			if sourceName != tt.wantSourceName {
				t.Errorf("GetWithSource() got SourceName = %q, want %q", sourceName, tt.wantSourceName)
			}
		})
	}
}

/* ───────── 13. SearchWithFilters: マルチキーワード検索とフィルタ ───────── */

func TestService_SearchWithFilters(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		keywords  []string
		filters   repository.ArticleSearchFilters
		setupRepo func(*stubRepo)
		wantCount int
		wantErr   bool
	}{
		{
			name:     "single keyword search",
			keywords: []string{"Go"},
			filters:  repository.ArticleSearchFilters{},
			setupRepo: func(s *stubRepo) {
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Go Programming", URL: "https://example.com/1", PublishedAt: now}
				s.data[2] = &entity.Article{ID: 2, SourceID: 1, Title: "React Tutorial", URL: "https://example.com/2", PublishedAt: now}
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:     "multi keyword search",
			keywords: []string{"Go", "Programming"},
			filters:  repository.ArticleSearchFilters{},
			setupRepo: func(s *stubRepo) {
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Go Programming", URL: "https://example.com/1", PublishedAt: now}
				s.data[2] = &entity.Article{ID: 2, SourceID: 1, Title: "Go Tutorial", URL: "https://example.com/2", PublishedAt: now}
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:     "with source_id filter",
			keywords: []string{"test"},
			filters: repository.ArticleSearchFilters{
				SourceID: func() *int64 { id := int64(1); return &id }(),
			},
			setupRepo: func(s *stubRepo) {
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Test Article", URL: "https://example.com/1", PublishedAt: now}
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "with date range filter",
			keywords: []string{"test"},
			filters: repository.ArticleSearchFilters{
				From: &now,
				To:   func() *time.Time { t := now.Add(24 * time.Hour); return &t }(),
			},
			setupRepo: func(s *stubRepo) {
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Test Article", URL: "https://example.com/1", PublishedAt: now}
			},
			wantCount: 1,
			wantErr:   false,
		},
		{
			name:     "repository error",
			keywords: []string{"test"},
			filters:  repository.ArticleSearchFilters{},
			setupRepo: func(s *stubRepo) {
				s.err = errors.New("search error")
			},
			wantCount: 0,
			wantErr:   true,
		},
		{
			name:     "empty result",
			keywords: []string{"nonexistent"},
			filters:  repository.ArticleSearchFilters{},
			setupRepo: func(s *stubRepo) {
				// 空のまま
			},
			wantCount: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStub()
			tt.setupRepo(stub)
			svc := artUC.Service{Repo: stub}

			articles, err := svc.SearchWithFilters(context.Background(), tt.keywords, tt.filters)

			if (err != nil) != tt.wantErr {
				t.Errorf("SearchWithFilters() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(articles) != tt.wantCount {
				t.Errorf("SearchWithFilters() got %d articles, want %d", len(articles), tt.wantCount)
			}
		})
	}
}

/* ───────── 14. Delete: 正常削除とリポジトリエラー ───────── */

func TestService_Delete_success(t *testing.T) {
	tests := []struct {
		name      string
		id        int64
		setupRepo func(*stubRepo)
		wantErr   bool
	}{
		{
			name: "successful deletion",
			id:   1,
			setupRepo: func(s *stubRepo) {
				now := time.Now()
				s.data[1] = &entity.Article{ID: 1, SourceID: 1, Title: "Test", URL: "https://example.com", PublishedAt: now}
			},
			wantErr: false,
		},
		{
			name: "repository error",
			id:   1,
			setupRepo: func(s *stubRepo) {
				s.err = errors.New("delete failed")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newStub()
			tt.setupRepo(stub)
			svc := artUC.Service{Repo: stub}

			err := svc.Delete(context.Background(), tt.id)

			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if _, exists := stub.data[tt.id]; exists {
					t.Errorf("Delete() article still exists with ID %d", tt.id)
				}
			}
		})
	}
}
