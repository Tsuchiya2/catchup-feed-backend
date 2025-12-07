package article_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"catchup-feed/internal/common/pagination"
	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
	"catchup-feed/internal/usecase/article"
)

/* ───────── モック実装 ───────── */

type mockArticleRepo struct {
	articlesWithSrc []repository.ArticleWithSource
	totalCount      int64
	listErr         error
	countErr        error
}

func (m *mockArticleRepo) ListWithSourcePaginated(_ context.Context, offset, limit int) ([]repository.ArticleWithSource, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	// Return subset based on offset/limit
	if offset >= len(m.articlesWithSrc) {
		return []repository.ArticleWithSource{}, nil
	}
	end := offset + limit
	if end > len(m.articlesWithSrc) {
		end = len(m.articlesWithSrc)
	}
	return m.articlesWithSrc[offset:end], nil
}

func (m *mockArticleRepo) CountArticles(_ context.Context) (int64, error) {
	if m.countErr != nil {
		return 0, m.countErr
	}
	return m.totalCount, nil
}

// Unused but required for interface
func (m *mockArticleRepo) List(_ context.Context) ([]*entity.Article, error) {
	return nil, nil
}
func (m *mockArticleRepo) Get(_ context.Context, _ int64) (*entity.Article, error) {
	return nil, nil
}
func (m *mockArticleRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return nil, nil
}
func (m *mockArticleRepo) SearchWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) ([]*entity.Article, error) {
	return nil, nil
}
func (m *mockArticleRepo) Create(_ context.Context, _ *entity.Article) error {
	return nil
}
func (m *mockArticleRepo) Update(_ context.Context, _ *entity.Article) error {
	return nil
}
func (m *mockArticleRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (m *mockArticleRepo) ExistsByURL(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *mockArticleRepo) ExistsByURLBatch(_ context.Context, _ []string) (map[string]bool, error) {
	return nil, nil
}
func (m *mockArticleRepo) GetWithSource(_ context.Context, _ int64) (*entity.Article, string, error) {
	return nil, "", nil
}
func (m *mockArticleRepo) ListWithSource(_ context.Context) ([]repository.ArticleWithSource, error) {
	return nil, nil
}

/* ───────── テストケース ───────── */

func TestService_ListWithSourcePaginated(t *testing.T) {
	t.Parallel()

	now := time.Now()
	mockArticles := []repository.ArticleWithSource{
		{
			Article: &entity.Article{
				ID:          1,
				SourceID:    10,
				Title:       "Article 1",
				URL:         "https://example.com/1",
				Summary:     "Summary 1",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Test Source",
		},
		{
			Article: &entity.Article{
				ID:          2,
				SourceID:    10,
				Title:       "Article 2",
				URL:         "https://example.com/2",
				Summary:     "Summary 2",
				PublishedAt: now,
				CreatedAt:   now,
			},
			SourceName: "Test Source",
		},
	}

	tests := []struct {
		name          string
		params        pagination.Params
		mockArticles  []repository.ArticleWithSource
		mockTotalCount int64
		wantDataLen   int
		wantTotal     int64
		wantPage      int
		wantTotalPages int
	}{
		{
			name: "first page with 2 results",
			params: pagination.Params{
				Page:  1,
				Limit: 20,
			},
			mockArticles:  mockArticles,
			mockTotalCount: 150,
			wantDataLen:   2,
			wantTotal:     150,
			wantPage:      1,
			wantTotalPages: 8,
		},
		{
			name: "second page",
			params: pagination.Params{
				Page:  2,
				Limit: 20,
			},
			mockArticles:  nil, // Will be generated in the test
			mockTotalCount: 150,
			wantDataLen:   20,
			wantTotal:     150,
			wantPage:      2,
			wantTotalPages: 8,
		},
		{
			name: "total less than limit",
			params: pagination.Params{
				Page:  1,
				Limit: 20,
			},
			mockArticles:  mockArticles,
			mockTotalCount: 10,
			wantDataLen:   2,
			wantTotal:     10,
			wantPage:      1,
			wantTotalPages: 1,
		},
		{
			name: "total equals limit",
			params: pagination.Params{
				Page:  1,
				Limit: 20,
			},
			mockArticles:  mockArticles,
			mockTotalCount: 20,
			wantDataLen:   2,
			wantTotal:     20,
			wantPage:      1,
			wantTotalPages: 1,
		},
		{
			name: "zero total",
			params: pagination.Params{
				Page:  1,
				Limit: 20,
			},
			mockArticles:  []repository.ArticleWithSource{},
			mockTotalCount: 0,
			wantDataLen:   0,
			wantTotal:     0,
			wantPage:      1,
			wantTotalPages: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a larger dataset for second page test
			allArticles := make([]repository.ArticleWithSource, 100)
			for i := 0; i < 100; i++ {
				allArticles[i] = repository.ArticleWithSource{
					Article: &entity.Article{
						ID:          int64(i + 1),
						SourceID:    10,
						Title:       "Article " + string(rune(i+1)),
						URL:         "https://example.com/" + string(rune(i+1)),
						Summary:     "Summary " + string(rune(i+1)),
						PublishedAt: now,
						CreatedAt:   now,
					},
					SourceName: "Test Source",
				}
			}

			// Use the provided mock articles or the generated ones
			mockData := tt.mockArticles
			if len(mockData) == 0 && tt.mockTotalCount > 0 {
				mockData = allArticles
			}

			mock := &mockArticleRepo{
				articlesWithSrc: mockData,
				totalCount:     tt.mockTotalCount,
			}

			svc := article.Service{Repo: mock}
			result, err := svc.ListWithSourcePaginated(context.Background(), tt.params)

			if err != nil {
				t.Fatalf("ListWithSourcePaginated() error = %v, want nil", err)
			}

			if len(result.Data) != tt.wantDataLen {
				t.Errorf("Data length = %d, want %d", len(result.Data), tt.wantDataLen)
			}

			if result.Pagination.Total != tt.wantTotal {
				t.Errorf("Pagination.Total = %d, want %d", result.Pagination.Total, tt.wantTotal)
			}

			if result.Pagination.Page != tt.wantPage {
				t.Errorf("Pagination.Page = %d, want %d", result.Pagination.Page, tt.wantPage)
			}

			if result.Pagination.Limit != tt.params.Limit {
				t.Errorf("Pagination.Limit = %d, want %d", result.Pagination.Limit, tt.params.Limit)
			}

			if result.Pagination.TotalPages != tt.wantTotalPages {
				t.Errorf("Pagination.TotalPages = %d, want %d", result.Pagination.TotalPages, tt.wantTotalPages)
			}
		})
	}
}

func TestService_ListWithSourcePaginated_CountError(t *testing.T) {
	t.Parallel()

	mock := &mockArticleRepo{
		countErr: errors.New("count error"),
	}

	svc := article.Service{Repo: mock}
	_, err := svc.ListWithSourcePaginated(context.Background(), pagination.Params{
		Page:  1,
		Limit: 20,
	})

	if err == nil {
		t.Fatal("ListWithSourcePaginated() error = nil, want error")
	}
}

func TestService_ListWithSourcePaginated_ListError(t *testing.T) {
	t.Parallel()

	mock := &mockArticleRepo{
		totalCount: 150,
		listErr:    errors.New("list error"),
	}

	svc := article.Service{Repo: mock}
	_, err := svc.ListWithSourcePaginated(context.Background(), pagination.Params{
		Page:  1,
		Limit: 20,
	})

	if err == nil {
		t.Fatal("ListWithSourcePaginated() error = nil, want error")
	}
}

func TestService_ListWithSourcePaginated_OffsetCalculation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		page        int
		limit       int
		wantOffset  int
	}{
		{
			name:       "page 1",
			page:       1,
			limit:      20,
			wantOffset: 0,
		},
		{
			name:       "page 2",
			page:       2,
			limit:      20,
			wantOffset: 20,
		},
		{
			name:       "page 10",
			page:       10,
			limit:      50,
			wantOffset: 450,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify offset calculation using pagination utilities
			offset := pagination.CalculateOffset(tt.page, tt.limit)
			if offset != tt.wantOffset {
				t.Errorf("CalculateOffset(%d, %d) = %d, want %d", tt.page, tt.limit, offset, tt.wantOffset)
			}
		})
	}
}
