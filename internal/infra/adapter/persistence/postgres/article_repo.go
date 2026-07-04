package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/pkg/search"
	"catchup-feed/internal/repository"
)

// articleColumns selects the §4 articles columns plus the summary body
// joined from summaries (entity.Article.Summary is a read-only join field).
// Every read query uses the same "articles a LEFT JOIN summaries sm" shape.
const (
	articleColumns = `a.id, a.source_id, a.title, a.url, COALESCE(a.content, '') AS content,
       COALESCE(sm.body, '') AS summary, a.published_at, a.crawled_at`
	articleFrom = `FROM articles a
LEFT JOIN summaries sm ON sm.article_id = a.id`
)

type ArticleRepo struct {
	db           *sql.DB
	queryBuilder *ArticleQueryBuilder
}

func NewArticleRepo(db *sql.DB) repository.ArticleRepository {
	return &ArticleRepo{
		db:           db,
		queryBuilder: NewArticleQueryBuilder(),
	}
}

func scanArticle(s scanner, extra ...any) (*entity.Article, error) {
	var (
		article     entity.Article
		publishedAt sql.NullTime
	)
	dest := []any{
		&article.ID, &article.SourceID, &article.Title, &article.URL,
		&article.Content, &article.Summary, &publishedAt, &article.CrawledAt,
	}
	dest = append(dest, extra...)
	if err := s.Scan(dest...); err != nil {
		return nil, err
	}
	article.PublishedAt = publishedAt.Time // zero value when NULL (§4: published_at is nullable)
	return &article, nil
}

func (repo *ArticleRepo) queryArticles(ctx context.Context, op, query string, args ...any) ([]*entity.Article, error) {
	rows, err := repo.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer func() { _ = rows.Close() }()

	articles := make([]*entity.Article, 0, 100)
	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: Scan: %w", op, err)
		}
		articles = append(articles, article)
	}
	return articles, rows.Err()
}

func (repo *ArticleRepo) queryArticlesWithSource(ctx context.Context, op, query string, capacity int, args ...any) ([]repository.ArticleWithSource, error) {
	rows, err := repo.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer func() { _ = rows.Close() }()

	result := make([]repository.ArticleWithSource, 0, capacity)
	for rows.Next() {
		var sourceName string
		article, err := scanArticle(rows, &sourceName)
		if err != nil {
			return nil, fmt.Errorf("%s: Scan: %w", op, err)
		}
		result = append(result, repository.ArticleWithSource{
			Article:    article,
			SourceName: sourceName,
		})
	}
	return result, rows.Err()
}

func (repo *ArticleRepo) List(ctx context.Context) ([]*entity.Article, error) {
	query := `
SELECT ` + articleColumns + `
` + articleFrom + `
ORDER BY a.published_at DESC`
	return repo.queryArticles(ctx, "List", query)
}

func (repo *ArticleRepo) ListWithSource(ctx context.Context) ([]repository.ArticleWithSource, error) {
	query := `
SELECT ` + articleColumns + `, s.name AS source_name
` + articleFrom + `
INNER JOIN sources s ON a.source_id = s.id
ORDER BY a.published_at DESC`
	return repo.queryArticlesWithSource(ctx, "ListWithSource", query, 100)
}

// ListWithSourcePaginated retrieves paginated articles with source names.
// Uses LIMIT and OFFSET for efficient pagination.
func (repo *ArticleRepo) ListWithSourcePaginated(ctx context.Context, offset, limit int) ([]repository.ArticleWithSource, error) {
	query := `
SELECT ` + articleColumns + `, s.name AS source_name
` + articleFrom + `
INNER JOIN sources s ON a.source_id = s.id
ORDER BY a.published_at DESC
LIMIT $1 OFFSET $2`
	return repo.queryArticlesWithSource(ctx, "ListWithSourcePaginated", query, limit, limit, offset)
}

// CountArticles returns the total number of articles in the database.
func (repo *ArticleRepo) CountArticles(ctx context.Context) (int64, error) {
	const query = `SELECT COUNT(*) FROM articles`
	var count int64
	err := repo.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("CountArticles: %w", err)
	}
	return count, nil
}

func (repo *ArticleRepo) Get(ctx context.Context, id int64) (*entity.Article, error) {
	query := `
SELECT ` + articleColumns + `
` + articleFrom + `
WHERE a.id = $1
LIMIT 1`
	article, err := scanArticle(repo.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get: %w", err)
	}
	return article, nil
}

func (repo *ArticleRepo) GetWithSource(ctx context.Context, id int64) (*entity.Article, string, error) {
	query := `
SELECT ` + articleColumns + `, s.name AS source_name
` + articleFrom + `
INNER JOIN sources s ON a.source_id = s.id
WHERE a.id = $1
LIMIT 1`
	var sourceName string
	article, err := scanArticle(repo.db.QueryRowContext(ctx, query, id), &sourceName)
	if err == sql.ErrNoRows {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("GetWithSource: %w", err)
	}
	return article, sourceName, nil
}

func (repo *ArticleRepo) Search(ctx context.Context, keyword string) ([]*entity.Article, error) {
	query := `
SELECT ` + articleColumns + `
` + articleFrom + `
WHERE a.title ILIKE $1
    OR sm.body ILIKE $1
ORDER BY a.published_at DESC`
	return repo.queryArticles(ctx, "Search", query, "%"+keyword+"%")
}

func (repo *ArticleRepo) SearchWithFilters(ctx context.Context, keywords []string, filters repository.ArticleSearchFilters) ([]*entity.Article, error) {
	// Check if there are any search criteria (keywords or filters)
	hasKeywords := len(keywords) > 0
	hasFilters := filters.SourceID != nil || filters.From != nil || filters.To != nil

	// No keywords and no filters -> return empty result
	if !hasKeywords && !hasFilters {
		return []*entity.Article{}, nil
	}

	// Apply search timeout to prevent long-running queries
	ctx, cancel := context.WithTimeout(ctx, search.DefaultSearchTimeout)
	defer cancel()

	// Build WHERE clause using QueryBuilder
	whereClause, args := repo.queryBuilder.BuildWhereClause(keywords, filters, "a")

	// #nosec G201 -- whereClause is generated by QueryBuilder using parameterized placeholders ($1, $2, etc.)
	query := fmt.Sprintf(`
SELECT %s
%s
%s
ORDER BY a.published_at DESC`, articleColumns, articleFrom, whereClause)

	return repo.queryArticles(ctx, "SearchWithFilters", query, args...)
}

// CountArticlesWithFilters returns the total number of articles matching the search criteria.
// Uses the same filters as SearchWithFilters for consistency.
func (repo *ArticleRepo) CountArticlesWithFilters(ctx context.Context, keywords []string, filters repository.ArticleSearchFilters) (int64, error) {
	// Check if there are any search criteria (keywords or filters)
	hasKeywords := len(keywords) > 0
	hasFilters := filters.SourceID != nil || filters.From != nil || filters.To != nil

	// No keywords and no filters -> return 0
	if !hasKeywords && !hasFilters {
		return 0, nil
	}

	// Apply search timeout to prevent long-running queries
	ctx, cancel := context.WithTimeout(ctx, search.DefaultSearchTimeout)
	defer cancel()

	// Build WHERE clause using QueryBuilder
	whereClause, args := repo.queryBuilder.BuildWhereClause(keywords, filters, "a")

	// Keyword conditions search sm.body, so the count query needs the same join.
	query := "SELECT COUNT(*) " + articleFrom + " " + whereClause

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
	// Check if there are any search criteria (keywords or filters)
	hasKeywords := len(keywords) > 0
	hasFilters := filters.SourceID != nil || filters.From != nil || filters.To != nil

	// No keywords and no filters -> return empty result
	if !hasKeywords && !hasFilters {
		return []repository.ArticleWithSource{}, nil
	}

	// Apply search timeout to prevent long-running queries
	ctx, cancel := context.WithTimeout(ctx, search.DefaultSearchTimeout)
	defer cancel()

	// Build WHERE clause using QueryBuilder with table alias 'a'
	whereClause, args := repo.queryBuilder.BuildWhereClause(keywords, filters, "a")

	// Calculate parameter index for LIMIT and OFFSET
	paramIndex := len(args) + 1

	// Add LIMIT and OFFSET to args
	args = append(args, limit, offset)

	// #nosec G201 -- whereClause is generated by QueryBuilder using parameterized placeholders ($1, $2, etc.)
	// paramIndex values are integers computed from len(args), not user input.
	query := fmt.Sprintf(`
SELECT %s, s.name AS source_name
%s
INNER JOIN sources s ON a.source_id = s.id
%s
ORDER BY a.published_at DESC
LIMIT $%d OFFSET $%d`, articleColumns, articleFrom, whereClause, paramIndex, paramIndex+1)

	return repo.queryArticlesWithSource(ctx, "SearchWithFiltersPaginated", query, limit, args...)
}

// Create inserts the article and sets article.ID (RETURNING id), which the
// crawl pipeline needs for the summaries.article_id foreign key.
// article.Summary is ignored: summaries live in their own table.
func (repo *ArticleRepo) Create(ctx context.Context, article *entity.Article) error {
	if article.CrawledAt.IsZero() {
		article.CrawledAt = time.Now()
	}
	err := repo.db.QueryRowContext(ctx, insertArticleSQL,
		article.SourceID, article.Title, article.URL,
		nullString(article.Content), nullTime(article.PublishedAt), article.CrawledAt,
	).Scan(&article.ID)
	if err != nil {
		return fmt.Errorf("Create: %w", err)
	}
	return nil
}

// insertArticleSQL inserts one article row and returns its id. Shared by
// Create and CreateWithSummary.
const insertArticleSQL = `
INSERT INTO articles
	   (source_id, title, url, content, published_at, crawled_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id`

// CreateWithSummary inserts the article and its summary atomically (same
// pattern as EpisodeRepo.Create). A summary insert failure rolls the
// article back, keeping the invariant "every stored article has a summary":
// the URL then stays unknown and the next hourly crawl retries it (§8).
func (repo *ArticleRepo) CreateWithSummary(ctx context.Context, article *entity.Article, summary *entity.Summary) error {
	if article.CrawledAt.IsZero() {
		article.CrawledAt = time.Now()
	}
	if summary.Provider == "" {
		summary.Provider = entity.SummaryProviderUnknown
	}

	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("CreateWithSummary: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := tx.QueryRowContext(ctx, insertArticleSQL,
		article.SourceID, article.Title, article.URL,
		nullString(article.Content), nullTime(article.PublishedAt), article.CrawledAt,
	).Scan(&article.ID); err != nil {
		return fmt.Errorf("CreateWithSummary: article: %w", err)
	}

	summary.ArticleID = article.ID
	const insertSummary = `
INSERT INTO summaries (article_id, body, provider)
VALUES ($1, $2, $3)`
	if _, err := tx.ExecContext(ctx, insertSummary,
		summary.ArticleID, summary.Body, summary.Provider,
	); err != nil {
		return fmt.Errorf("CreateWithSummary: summary: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("CreateWithSummary: commit: %w", err)
	}
	return nil
}

func (repo *ArticleRepo) Update(ctx context.Context, article *entity.Article) error {
	const query = `
UPDATE articles SET
       source_id    = $1,
       title        = $2,
       url          = $3,
       content      = $4,
       published_at = $5
WHERE id = $6`
	res, err := repo.db.ExecContext(ctx, query,
		article.SourceID, article.Title, article.URL,
		nullString(article.Content), nullTime(article.PublishedAt), article.ID,
	)
	if err != nil {
		return fmt.Errorf("Update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("Update: no rows affected")
	}
	return nil
}

// Delete removes the article and its summary row (summaries.article_id
// REFERENCES articles, so the summary must go first) in one transaction.
// Articles referenced by episode segments fail with an FK error on
// purpose: segment scripts are Phase 3 assets and must keep their source.
func (repo *ArticleRepo) Delete(ctx context.Context, id int64) error {
	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("Delete: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM summaries WHERE article_id = $1`, id); err != nil {
		return fmt.Errorf("Delete: summary: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM articles WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("Delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("Delete: no rows affected")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("Delete: commit: %w", err)
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

	// Build placeholders for IN clause: ($1, $2, $3, ...)
	// This is more compatible with database/sql than ANY($1::text[])
	placeholders := make([]string, len(urls))
	args := make([]interface{}, len(urls))
	for i, url := range urls {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = url
	}

	// #nosec G201 -- placeholders are programmatically generated ($1, $2, etc.), not from user input
	query := fmt.Sprintf(
		`SELECT url FROM articles WHERE url IN (%s)`,
		strings.Join(placeholders, ", "),
	)

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

// nullString maps "" to SQL NULL (articles.content is nullable in §4).
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// nullTime maps the zero time to SQL NULL (articles.published_at is
// nullable in §4: feeds without dates stay NULL instead of year 1).
func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: !t.IsZero()}
}
