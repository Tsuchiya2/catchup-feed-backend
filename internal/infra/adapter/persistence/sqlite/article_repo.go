// Package sqlite provides SQLite implementations of repository interfaces.
// It includes repositories for articles and sources with optimized query performance.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/pkg/search"
	"catchup-feed/internal/repository"
)

// ArticleRepo implements the ArticleRepository interface using SQLite.
type ArticleRepo struct {
	db           *sql.DB
	queryBuilder *ArticleQueryBuilder
}

// NewArticleRepo creates a new SQLite-backed article repository.
func NewArticleRepo(db *sql.DB) repository.ArticleRepository {
	return &ArticleRepo{
		db:           db,
		queryBuilder: NewArticleQueryBuilder(),
	}
}

// List retrieves all articles ordered by published date (newest first).
func (repo *ArticleRepo) List(ctx context.Context) ([]*entity.Article, error) {
	const query = `
SELECT id, source_id, title, url, summary, published_at, created_at
FROM articles
ORDER BY published_at DESC
`

	rows, err := repo.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("List: QueryContext: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	articles := make([]*entity.Article, 0, 100)
	for rows.Next() {
		var article entity.Article
		err := rows.Scan(&article.ID,
			&article.SourceID, &article.Title,
			&article.URL, &article.Summary,
			&article.PublishedAt, &article.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("List: Scan: %w", err)
		}
		articles = append(articles, &article)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("List: rows.Err: %w", err)
	}

	return articles, nil
}

// ListWithSource retrieves all articles with their source names.
func (repo *ArticleRepo) ListWithSource(ctx context.Context) ([]repository.ArticleWithSource, error) {
	const query = `
SELECT a.id, a.source_id, a.title, a.url, a.summary, a.published_at, a.created_at, s.name AS source_name
FROM articles a
INNER JOIN sources s ON a.source_id = s.id
ORDER BY a.published_at DESC
`

	rows, err := repo.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ListWithSource: QueryContext: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	result := make([]repository.ArticleWithSource, 0, 100)
	for rows.Next() {
		var article entity.Article
		var sourceName string
		err := rows.Scan(&article.ID,
			&article.SourceID, &article.Title,
			&article.URL, &article.Summary,
			&article.PublishedAt, &article.CreatedAt, &sourceName)
		if err != nil {
			return nil, fmt.Errorf("ListWithSource: Scan: %w", err)
		}
		result = append(result, repository.ArticleWithSource{
			Article:    &article,
			SourceName: sourceName,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListWithSource: rows.Err: %w", err)
	}

	return result, nil
}

// ListWithSourcePaginated retrieves paginated articles with source names.
// Uses LIMIT and OFFSET for efficient pagination.
func (repo *ArticleRepo) ListWithSourcePaginated(ctx context.Context, offset, limit int) ([]repository.ArticleWithSource, error) {
	const query = `
SELECT a.id, a.source_id, a.title, a.url, a.summary, a.published_at, a.created_at, s.name AS source_name
FROM articles a
INNER JOIN sources s ON a.source_id = s.id
ORDER BY a.published_at DESC
LIMIT ? OFFSET ?
`

	rows, err := repo.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("ListWithSourcePaginated: QueryContext: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]repository.ArticleWithSource, 0, limit)
	for rows.Next() {
		var article entity.Article
		var sourceName string
		err := rows.Scan(&article.ID,
			&article.SourceID, &article.Title,
			&article.URL, &article.Summary,
			&article.PublishedAt, &article.CreatedAt, &sourceName)
		if err != nil {
			return nil, fmt.Errorf("ListWithSourcePaginated: Scan: %w", err)
		}
		result = append(result, repository.ArticleWithSource{
			Article:    &article,
			SourceName: sourceName,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListWithSourcePaginated: rows.Err: %w", err)
	}

	return result, nil
}

// CountArticles returns the total number of articles in the database.
func (repo *ArticleRepo) CountArticles(ctx context.Context) (int64, error) {
	const query = `SELECT COUNT(*) FROM articles`
	var count int64
	err := repo.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("CountArticles: QueryRowContext: %w", err)
	}
	return count, nil
}

func (repo *ArticleRepo) Get(ctx context.Context, id int64) (*entity.Article, error) {
	const query = `
SELECT id, source_id, title, url, summary, published_at, created_at
FROM articles
WHERE id = ?
LIMIT 1
`
	var article entity.Article
	err := repo.db.QueryRowContext(ctx, query, id).Scan(
		&article.ID, &article.SourceID, &article.Title, &article.URL,
		&article.Summary, &article.PublishedAt, &article.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("Get: QueryRowContext: %w", err)
	}
	return &article, nil
}

func (repo *ArticleRepo) GetWithSource(ctx context.Context, id int64) (*entity.Article, string, error) {
	const query = `
SELECT a.id, a.source_id, a.title, a.url, a.summary, a.published_at, a.created_at, s.name AS source_name
FROM articles a
INNER JOIN sources s ON a.source_id = s.id
WHERE a.id = ?
LIMIT 1
`
	var article entity.Article
	var sourceName string
	err := repo.db.QueryRowContext(ctx, query, id).Scan(
		&article.ID, &article.SourceID, &article.Title, &article.URL,
		&article.Summary, &article.PublishedAt, &article.CreatedAt, &sourceName,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("GetWithSource: QueryRowContext: %w", err)
	}
	return &article, sourceName, nil
}

func (repo *ArticleRepo) Search(ctx context.Context, keyword string) ([]*entity.Article, error) {
	const query = `
SELECT id, source_id, title, url, summary, published_at, created_at
FROM articles
WHERE title   LIKE ?
OR summary    LIKE ?
ORDER BY published_at DESC
`
	param := "%" + keyword + "%"
	rows, err := repo.db.QueryContext(ctx, query, param, param)
	if err != nil {
		return nil, fmt.Errorf("Search: QueryContext: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	articles := make([]*entity.Article, 0, 100)
	for rows.Next() {
		var article entity.Article
		err := rows.Scan(&article.ID,
			&article.SourceID, &article.Title,
			&article.URL, &article.Summary,
			&article.PublishedAt, &article.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("Search: Scan: %w", err)
		}
		articles = append(articles, &article)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Search: rows.Err: %w", err)
	}

	return articles, nil
}

// SearchWithFilters searches articles with multi-keyword AND logic and optional filters.
// Note: SQLite uses LIKE instead of ILIKE (case-insensitive by default for ASCII).
func (repo *ArticleRepo) SearchWithFilters(ctx context.Context, keywords []string, filters repository.ArticleSearchFilters) ([]*entity.Article, error) {
	// Empty keywords -> return empty result
	if len(keywords) == 0 {
		return []*entity.Article{}, nil
	}

	// Apply search timeout to prevent long-running queries
	ctx, cancel := context.WithTimeout(ctx, search.DefaultSearchTimeout)
	defer cancel()

	// Build WHERE clause using shared QueryBuilder
	whereClause, args := repo.queryBuilder.BuildWhereClause(keywords, filters)

	// Construct final query
	query := `
SELECT id, source_id, title, url, summary, published_at, created_at
FROM articles
` + whereClause + `
ORDER BY published_at DESC`

	rows, err := repo.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("SearchWithFilters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	articles := make([]*entity.Article, 0, 100)
	for rows.Next() {
		var article entity.Article
		if err := rows.Scan(&article.ID, &article.SourceID, &article.Title,
			&article.URL, &article.Summary, &article.PublishedAt, &article.CreatedAt); err != nil {
			return nil, fmt.Errorf("SearchWithFilters: Scan: %w", err)
		}
		articles = append(articles, &article)
	}
	return articles, rows.Err()
}

// CountArticlesWithFilters returns the total number of articles matching the search criteria.
// Uses the same QueryBuilder as SearchWithFilters for consistency.
func (repo *ArticleRepo) CountArticlesWithFilters(ctx context.Context, keywords []string, filters repository.ArticleSearchFilters) (int64, error) {
	// Empty keywords -> return 0
	if len(keywords) == 0 {
		return 0, nil
	}

	// Apply search timeout to prevent long-running queries
	ctx, cancel := context.WithTimeout(ctx, search.DefaultSearchTimeout)
	defer cancel()

	// Build WHERE clause using shared QueryBuilder
	whereClause, args := repo.queryBuilder.BuildWhereClause(keywords, filters)

	// Construct COUNT query
	query := "SELECT COUNT(*) FROM articles " + whereClause

	var count int64
	err := repo.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("CountArticlesWithFilters: %w", err)
	}

	return count, nil
}

// SearchWithFiltersPaginated searches articles with pagination support.
// Includes source_name from JOIN with sources table.
func (repo *ArticleRepo) SearchWithFiltersPaginated(ctx context.Context, keywords []string, filters repository.ArticleSearchFilters, offset, limit int) ([]repository.ArticleWithSource, error) {
	// Empty keywords -> return empty result
	if len(keywords) == 0 {
		return []repository.ArticleWithSource{}, nil
	}

	// Apply search timeout to prevent long-running queries
	ctx, cancel := context.WithTimeout(ctx, search.DefaultSearchTimeout)
	defer cancel()

	// Build WHERE clause using shared QueryBuilder
	// Note: We need to prefix 'a.' to column names for JOIN query
	whereClause, args := repo.queryBuilder.BuildWhereClause(keywords, filters)
	// Replace column names with table alias
	whereClause = strings.ReplaceAll(whereClause, "title LIKE", "a.title LIKE")
	whereClause = strings.ReplaceAll(whereClause, "summary LIKE", "a.summary LIKE")
	whereClause = strings.ReplaceAll(whereClause, "source_id =", "a.source_id =")
	whereClause = strings.ReplaceAll(whereClause, "published_at >=", "a.published_at >=")
	whereClause = strings.ReplaceAll(whereClause, "published_at <=", "a.published_at <=")

	// Construct query with JOIN
	query := `
SELECT a.id, a.source_id, a.title, a.url, a.summary, a.published_at, a.created_at, s.name AS source_name
FROM articles a
INNER JOIN sources s ON a.source_id = s.id
` + whereClause + `
ORDER BY a.published_at DESC
LIMIT ? OFFSET ?`

	// Add LIMIT and OFFSET to args
	args = append(args, limit, offset)

	rows, err := repo.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("SearchWithFiltersPaginated: %w", err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]repository.ArticleWithSource, 0, limit)
	for rows.Next() {
		var article entity.Article
		var sourceName string
		if err := rows.Scan(&article.ID, &article.SourceID, &article.Title,
			&article.URL, &article.Summary, &article.PublishedAt, &article.CreatedAt, &sourceName); err != nil {
			return nil, fmt.Errorf("SearchWithFiltersPaginated: Scan: %w", err)
		}
		result = append(result, repository.ArticleWithSource{
			Article:    &article,
			SourceName: sourceName,
		})
	}

	return result, rows.Err()
}

func (repo *ArticleRepo) Create(ctx context.Context, article *entity.Article) error {
	const query = `
INSERT INTO articles
(source_id, title, url, summary, published_at, created_at)
VALUES (?, ?, ?, ?, ?, ?)
`
	_, err := repo.db.ExecContext(ctx, query,
		article.SourceID, article.Title, article.URL,
		article.Summary, article.PublishedAt, article.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("Create: ExecContext: %w", err)
	}
	return nil
}

func (repo *ArticleRepo) Update(ctx context.Context, article *entity.Article) error {
	const query = `
UPDATE articles SET
	source_id 	 = ?,
	title 		 = ?,
	url 		 = ?,
	summary 	 = ?,
	published_at = ?
WHERE id = ?
`
	res, err := repo.db.ExecContext(ctx, query,
		article.SourceID, article.Title, article.URL,
		article.Summary, article.PublishedAt, article.ID,
	)

	if err != nil {
		return fmt.Errorf("Update: ExecContext: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("Update: RowsAffected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("Update: no rows affected")
	}
	return nil
}

func (repo *ArticleRepo) Delete(ctx context.Context, id int64) error {
	const query = `DELETE FROM articles WHERE id = ?
`
	res, err := repo.db.ExecContext(ctx, query, id)

	if err != nil {
		return fmt.Errorf("Delete: ExecContext: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("Delete: RowsAffected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("Delete: no rows affected")
	}
	return nil
}

func (repo *ArticleRepo) ExistsByURL(ctx context.Context, url string) (bool, error) {
	const query = `SELECT 1 FROM articles WHERE url = ? LIMIT 1`
	var existsFlag bool
	err := repo.db.QueryRowContext(ctx, query, url).Scan(&existsFlag)
	if err == sql.ErrNoRows {
		return false, nil // データが存在しない場合はエラーではない
	}
	if err != nil {
		return false, fmt.Errorf("ExistsByURL: %w", err)
	}
	return true, nil
}

// ExistsByURLBatch はバッチでURL存在チェックを行い、N+1問題を解消する
func (repo *ArticleRepo) ExistsByURLBatch(ctx context.Context, urls []string) (map[string]bool, error) {
	if len(urls) == 0 {
		return make(map[string]bool), nil
	}

	// SQLiteのプレースホルダ上限は999
	// 参考: https://www.sqlite.org/limits.html#max_variable_number
	const maxPlaceholders = 999
	if len(urls) > maxPlaceholders {
		return nil, fmt.Errorf("ExistsByURLBatch: too many URLs (%d > %d)", len(urls), maxPlaceholders)
	}

	// 安全性確認: placeholdersは"?"のみを含むため、SQLインジェクションのリスクはない
	// fmt.Sprintf使用は許容される（placeholders配列の内容が制御されているため）
	placeholders := make([]string, len(urls))
	args := make([]interface{}, len(urls))
	for i, url := range urls {
		placeholders[i] = "?" // 固定値のみ
		args[i] = url
	}

	// クエリ組み立て（placeholdersは制御された値のみ）
	query := fmt.Sprintf("SELECT url FROM articles WHERE url IN (%s)",
		strings.Join(placeholders, ","))

	rows, err := repo.db.QueryContext(ctx, query, args...)
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
