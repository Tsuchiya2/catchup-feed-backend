package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"catchup-feed/internal/repository"
)

// RadioArticleRepo selects summarized articles for the radio batch (§6-1).
type RadioArticleRepo struct{ db *sql.DB }

func NewRadioArticleRepo(db *sql.DB) repository.RadioArticleRepository {
	return &RadioArticleRepo{db: db}
}

// ListSummarizedSince returns articles whose summary was created after
// since, oldest first (broadcast backlog order), up to limit. The SELECT
// deliberately excludes articles.content: only the summary body reaches
// the script generator (C-12).
//
// The NOT EXISTS on segments structurally excludes already-broadcast
// articles. It makes the deliberate cursor overlap harmless (episodes'
// published_at is the previous run's selection timestamp, so consecutive
// windows may overlap) and keeps manual -since re-runs from double-airing
// old articles (§6-6 冪等性).
func (repo *RadioArticleRepo) ListSummarizedSince(ctx context.Context, since time.Time, limit int) ([]repository.RadioArticle, error) {
	const query = `
SELECT a.id, a.title, a.url, s.category, s.name, sm.body,
       COALESCE(a.published_at, a.crawled_at) AS published_at
FROM articles a
JOIN summaries sm ON sm.article_id = a.id
JOIN sources s ON s.id = a.source_id
WHERE sm.created_at > $1
  AND NOT EXISTS (SELECT 1 FROM segments sg WHERE sg.article_id = a.id)
ORDER BY COALESCE(a.published_at, a.crawled_at) ASC, a.id ASC
LIMIT $2`
	rows, err := repo.db.QueryContext(ctx, query, since, limit)
	if err != nil {
		return nil, fmt.Errorf("ListSummarizedSince: %w", err)
	}
	defer func() { _ = rows.Close() }()

	articles := make([]repository.RadioArticle, 0, 16)
	for rows.Next() {
		var a repository.RadioArticle
		if err := rows.Scan(
			&a.ID, &a.Title, &a.URL, &a.Category, &a.SourceName,
			&a.Summary, &a.PublishedAt,
		); err != nil {
			return nil, fmt.Errorf("ListSummarizedSince: %w", err)
		}
		articles = append(articles, a)
	}
	return articles, rows.Err()
}
