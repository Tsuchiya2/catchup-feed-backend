package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"

	"github.com/lib/pq"
)

type ArticleRepo struct{ db *sql.DB }

func NewArticleRepo(db *sql.DB) repository.ArticleRepository {
	return &ArticleRepo{db: db}
}

func (repo *ArticleRepo) List(ctx context.Context) ([]*entity.Article, error) {
	const query = `
SELECT id, source_id, title, url, summary, published_at, created_at
FROM articles
ORDER BY published_at DESC`
	rows, err := repo.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	articles := make([]*entity.Article, 0, 100)
	for rows.Next() {
		var article entity.Article
		if err := rows.Scan(&article.ID, &article.SourceID, &article.Title,
			&article.URL, &article.Summary, &article.PublishedAt, &article.CreatedAt); err != nil {
			return nil, fmt.Errorf("List: Scan: %w", err)
		}
		articles = append(articles, &article)
	}
	return articles, rows.Err()
}

func (repo *ArticleRepo) Get(ctx context.Context, id int64) (*entity.Article, error) {
	const query = `
SELECT id, source_id, title, url, summary, published_at, created_at
FROM articles
WHERE id = $1
LIMIT 1`
	var article entity.Article
	err := repo.db.QueryRowContext(ctx, query, id).
		Scan(&article.ID, &article.SourceID, &article.Title, &article.URL,
			&article.Summary, &article.PublishedAt, &article.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get: %w", err)
	}
	return &article, nil
}

func (repo *ArticleRepo) Search(ctx context.Context, keyword string) ([]*entity.Article, error) {
	const query = `
SELECT id, source_id, title, url, summary, published_at, created_at
FROM articles
WHERE title   ILIKE $1
    OR summary ILIKE $1
ORDER BY published_at DESC`
	param := "%" + keyword + "%"
	rows, err := repo.db.QueryContext(ctx, query, param)
	if err != nil {
		return nil, fmt.Errorf("Search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	articles := make([]*entity.Article, 0, 100)
	for rows.Next() {
		var article entity.Article
		if err := rows.Scan(&article.ID, &article.SourceID, &article.Title,
			&article.URL, &article.Summary, &article.PublishedAt, &article.CreatedAt); err != nil {
			return nil, fmt.Errorf("Search: Scan: %w", err)
		}
		articles = append(articles, &article)
	}
	return articles, rows.Err()
}

func (repo *ArticleRepo) Create(ctx context.Context, article *entity.Article) error {
	const query = `
INSERT INTO articles
	   (source_id, title, url, summary, published_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := repo.db.ExecContext(ctx, query,
		article.SourceID, article.Title, article.URL,
		article.Summary, article.PublishedAt, article.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("Create: %w", err)
	}
	return nil
}

func (repo *ArticleRepo) Update(ctx context.Context, article *entity.Article) error {
	const query = `
UPDATE articles SET
       source_id    = $1,
       title        = $2,
       url          = $3,
       summary      = $4,
       published_at = $5
WHERE id = $6`
	res, err := repo.db.ExecContext(ctx, query,
		article.SourceID, article.Title, article.URL,
		article.Summary, article.PublishedAt, article.ID,
	)
	if err != nil {
		return fmt.Errorf("Update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("Update: no rows affected")
	}
	return nil
}

func (repo *ArticleRepo) Delete(ctx context.Context, id int64) error {
	const query = `DELETE FROM articles WHERE id = $1`
	res, err := repo.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("Delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("Delete: no rows affected")
	}
	return nil
}

func (repo *ArticleRepo) ExistsByURL(ctx context.Context, url string) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM articles WHERE url = $1)`
	var existsFlag bool
	err := repo.db.QueryRowContext(ctx, query, url).Scan(&existsFlag)
	if err != nil {
		return false, fmt.Errorf("ExistsByURL: %w", err)
	}
	return existsFlag, nil
}

// ExistsByURLBatch はバッチでURL存在チェックを行い、N+1問題を解消する
func (repo *ArticleRepo) ExistsByURLBatch(ctx context.Context, urls []string) (map[string]bool, error) {
	if len(urls) == 0 {
		return make(map[string]bool), nil
	}

	const query = `SELECT url FROM articles WHERE url = ANY($1)`
	rows, err := repo.db.QueryContext(ctx, query, pq.Array(urls))
	if err != nil {
		return nil, fmt.Errorf("ExistsByURLBatch: QueryContext: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]bool)
	for rows.Next() {
		var url string
		if err := rows.Scan(&url); err != nil {
			return nil, fmt.Errorf("ExistsByURLBatch: Scan: %w", err)
		}
		result[url] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ExistsByURLBatch: rows.Err: %w", err)
	}

	return result, nil
}
