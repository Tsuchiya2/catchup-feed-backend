package repository

import (
	"context"
	"time"

	"catchup-feed/internal/domain/entity"
)

// ArticleWithSource represents an article along with its source name.
type ArticleWithSource struct {
	Article    *entity.Article
	SourceName string
}

// ArticleSearchFilters contains optional filters for article search
type ArticleSearchFilters struct {
	SourceID *int64     // Optional: Filter by source ID
	From     *time.Time // Optional: Filter articles published >= this date
	To       *time.Time // Optional: Filter articles published <= this date
}

type ArticleRepository interface {
	List(ctx context.Context) ([]*entity.Article, error)
	// ListWithSource retrieves all articles with their source names.
	// Returns a slice of ArticleWithSource containing article and source name pairs.
	ListWithSource(ctx context.Context) ([]ArticleWithSource, error)
	// ListWithSourcePaginated retrieves paginated articles with their source names.
	// Uses LIMIT and OFFSET for efficient pagination.
	// Parameters:
	//   - offset: Number of rows to skip (calculated from page number)
	//   - limit: Maximum number of rows to return
	// Returns articles ordered by published_at DESC.
	ListWithSourcePaginated(ctx context.Context, offset, limit int) ([]ArticleWithSource, error)
	// CountArticles returns the total number of articles in the database.
	// This is used for calculating pagination metadata (total pages, etc.).
	CountArticles(ctx context.Context) (int64, error)
	Get(ctx context.Context, id int64) (*entity.Article, error)
	// GetWithSource retrieves an article by ID and includes the source name.
	// Returns the article entity, source name, and error.
	// Returns (nil, "", nil) if the article is not found.
	GetWithSource(ctx context.Context, id int64) (*entity.Article, string, error)
	Search(ctx context.Context, keyword string) ([]*entity.Article, error)
	// SearchWithFilters searches articles with multi-keyword AND logic and optional filters
	SearchWithFilters(ctx context.Context, keywords []string, filters ArticleSearchFilters) ([]*entity.Article, error)
	Create(ctx context.Context, article *entity.Article) error
	Update(ctx context.Context, article *entity.Article) error
	Delete(ctx context.Context, id int64) error
	ExistsByURL(ctx context.Context, url string) (bool, error)
	// ExistsByURLBatch はバッチでURL存在チェックを行い、N+1問題を解消する
	ExistsByURLBatch(ctx context.Context, urls []string) (map[string]bool, error)
}
