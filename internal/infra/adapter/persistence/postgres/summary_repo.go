package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// SummaryRepo persists article summaries (summaries table, §4).
type SummaryRepo struct{ db *sql.DB }

func NewSummaryRepo(db *sql.DB) repository.SummaryRepository {
	return &SummaryRepo{db: db}
}

// Upsert inserts or replaces the summary of an article. article_id is the
// primary key (one summary per article); re-summarizing refreshes body,
// provider and created_at.
func (repo *SummaryRepo) Upsert(ctx context.Context, summary *entity.Summary) error {
	if summary.Provider == "" {
		summary.Provider = entity.SummaryProviderUnknown
	}
	const query = `
INSERT INTO summaries (article_id, body, provider)
VALUES ($1, $2, $3)
ON CONFLICT (article_id) DO UPDATE SET
       body       = EXCLUDED.body,
       provider   = EXCLUDED.provider,
       created_at = now()`
	if _, err := repo.db.ExecContext(ctx, query,
		summary.ArticleID, summary.Body, summary.Provider,
	); err != nil {
		return fmt.Errorf("Upsert: %w", err)
	}
	return nil
}

// GetByArticleID returns the summary for an article, or nil when the
// article has not been summarized yet.
func (repo *SummaryRepo) GetByArticleID(ctx context.Context, articleID int64) (*entity.Summary, error) {
	const query = `
SELECT article_id, body, provider, created_at
FROM summaries
WHERE article_id = $1
LIMIT 1`
	var summary entity.Summary
	err := repo.db.QueryRowContext(ctx, query, articleID).Scan(
		&summary.ArticleID, &summary.Body, &summary.Provider, &summary.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetByArticleID: %w", err)
	}
	return &summary, nil
}
