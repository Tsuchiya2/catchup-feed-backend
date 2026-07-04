package repository

import (
	"context"

	"catchup-feed/internal/domain/entity"
)

// SummaryRepository persists article summaries (summaries table, §4).
// article_id is the primary key: one summary per article.
type SummaryRepository interface {
	// Upsert inserts the summary or, when a summary already exists for
	// the article, replaces its body/provider (created_at is refreshed).
	Upsert(ctx context.Context, summary *entity.Summary) error
	// GetByArticleID returns the summary for an article, or nil when the
	// article has not been summarized yet.
	GetByArticleID(ctx context.Context, articleID int64) (*entity.Summary, error)
}
