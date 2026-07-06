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
	// CountArticlesWithFilters returns the total number of articles matching the search criteria.
	// Uses the same filters as SearchWithFilters for consistency.
	CountArticlesWithFilters(ctx context.Context, keywords []string, filters ArticleSearchFilters) (int64, error)
	// SearchWithFiltersPaginated searches articles with pagination support.
	// Returns articles matching the criteria with LIMIT and OFFSET applied.
	// Includes source_name from JOIN with sources table.
	SearchWithFiltersPaginated(ctx context.Context, keywords []string, filters ArticleSearchFilters, offset, limit int) ([]ArticleWithSource, error)
	// Create inserts a new article row and sets article.ID from the
	// database (needed for the summaries.article_id foreign key).
	// article.Summary is read-only and ignored here; persist summaries
	// through SummaryRepository or CreateWithSummary.
	Create(ctx context.Context, article *entity.Article) error
	// CreateWithSummary inserts the article and its summary in one
	// transaction (crawl pipeline path). Either both rows land or neither:
	// a failed summary insert rolls the article back, so the URL stays
	// absent and the next hourly crawl retries it (§8 縮退許容 — no
	// permanently unsummarized articles). Sets article.ID and
	// summary.ArticleID.
	CreateWithSummary(ctx context.Context, article *entity.Article, summary *entity.Summary) error
	// CreateWithTranscribeJob inserts the article (content NULL) and a
	// kind='transcribe' job in one transaction (Phase 2 §5: youtube /
	// podcast の新着検知). Either both rows land or neither, so a
	// content-less article always has a pending transcribe job. The job
	// payload is entity.TranscribePayload {article_id, media_url,
	// source_kind}; it is claimed by the Mac transcribe worker only, never
	// by the Pi consumer. Sets article.ID.
	CreateWithTranscribeJob(ctx context.Context, article *entity.Article, mediaURL, sourceKind string) error
	Update(ctx context.Context, article *entity.Article) error
	Delete(ctx context.Context, id int64) error
	ExistsByURL(ctx context.Context, url string) (bool, error)
	// ExistsByURLBatch はバッチでURL存在チェックを行い、N+1問題を解消する
	ExistsByURLBatch(ctx context.Context, urls []string) (map[string]bool, error)
}
