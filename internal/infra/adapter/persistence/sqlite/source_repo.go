package sqlite

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

type SourceRepo struct{ db *sql.DB }

func NewSourceRepo(db *sql.DB) repository.SourceRepository {
	return &SourceRepo{db: db}
}

func (repo *SourceRepo) Get(ctx context.Context, id int64) (*entity.Source, error) {
	const query = `
SELECT id, name, feed_url, last_crawled_at, active
FROM sources
WHERE id = ?
LIMIT 1`
	var source entity.Source
	err := repo.db.QueryRowContext(ctx, query, id).Scan(
		&source.ID, &source.Name, &source.FeedURL, &source.LastCrawledAt, &source.Active,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get: QueryRowContext: %w", err)
	}
	return &source, nil
}

func (repo *SourceRepo) List(ctx context.Context) ([]*entity.Source, error) {
	const query = `
SELECT
    id,
    name,
    feed_url,
    last_crawled_at,
    active
FROM sources
ORDER BY id ASC
`
	rows, err := repo.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("List: QueryContext: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	sources := make([]*entity.Source, 0, 50)
	for rows.Next() {
		var source entity.Source
		err := rows.Scan(&source.ID,
			&source.Name, &source.FeedURL,
			&source.LastCrawledAt,
			&source.Active)
		if err != nil {
			return nil, fmt.Errorf("List: Scan: %w", err)
		}
		sources = append(sources, &source)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("List: rows.Err: %w", err)
	}

	return sources, nil
}

func (repo *SourceRepo) ListActive(ctx context.Context) ([]*entity.Source, error) {
	const query = `
SELECT id, name, feed_url, last_crawled_at, active
FROM sources
WHERE active = TRUE
ORDER BY id ASC`
	rows, err := repo.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ListActive: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	activeSource := make([]*entity.Source, 0, 50)
	for rows.Next() {
		var source entity.Source
		if err := rows.Scan(&source.ID, &source.Name, &source.FeedURL,
			&source.LastCrawledAt, &source.Active); err != nil {
			return nil, fmt.Errorf("ListActive: Scan: %w", err)
		}
		activeSource = append(activeSource, &source)
	}
	return activeSource, rows.Err()
}

func (repo *SourceRepo) Search(ctx context.Context, keyword string) ([]*entity.Source, error) {
	const query = `
SELECT
    id,
    name,
    feed_url,
    last_crawled_at,
    active
FROM sources
WHERE name  LIKE ?
OR feed_url LIKE ?
ORDER BY id ASC
`
	param := "%" + keyword + "%"
	rows, err := repo.db.QueryContext(ctx, query, param, param)
	if err != nil {
		return nil, fmt.Errorf("Search: QueryContext: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	sources := make([]*entity.Source, 0, 50)
	for rows.Next() {
		var source entity.Source
		err := rows.Scan(&source.ID,
			&source.Name, &source.FeedURL,
			&source.LastCrawledAt,
			&source.Active)
		if err != nil {
			return nil, fmt.Errorf("Search: Scan: %w", err)
		}
		sources = append(sources, &source)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("Search: rows.Err: %w", err)
	}

	return sources, nil
}

// SearchWithFilters searches sources with multi-keyword AND logic and optional filters.
// Note: SQLite uses LIKE instead of ILIKE (case-insensitive by default for ASCII).
func (repo *SourceRepo) SearchWithFilters(ctx context.Context, keywords []string, filters repository.SourceSearchFilters) ([]*entity.Source, error) {
	// Apply search timeout to prevent long-running queries
	ctx, cancel := context.WithTimeout(ctx, search.DefaultSearchTimeout)
	defer cancel()

	// Build WHERE clause conditions
	var conditions []string
	var args []interface{}

	// Add keyword conditions (AND logic between keywords, OR logic within each keyword)
	for _, kw := range keywords {
		// SQLite uses LIKE with % wildcards
		pattern := "%" + kw + "%"
		conditions = append(conditions, "(name LIKE ? OR feed_url LIKE ?)")
		args = append(args, pattern, pattern)
	}

	// Add source_type filter if provided
	if filters.SourceType != nil {
		conditions = append(conditions, "source_type = ?")
		args = append(args, *filters.SourceType)
	}

	// Add active filter if provided
	if filters.Active != nil {
		conditions = append(conditions, "active = ?")
		args = append(args, *filters.Active)
	}

	// Build final query with dynamic WHERE clause
	var query string
	if len(conditions) > 0 {
		// With filters or keywords
		query = `
SELECT id, name, feed_url, source_type, last_crawled_at, active
FROM sources
WHERE ` + strings.Join(conditions, " AND ") + `
ORDER BY id ASC`
	} else {
		// No keywords, no filters - return all sources (browse mode)
		query = `
SELECT id, name, feed_url, source_type, last_crawled_at, active
FROM sources
ORDER BY id ASC`
	}

	// Execute query
	rows, err := repo.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("SearchWithFilters: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	sources := make([]*entity.Source, 0, 50)
	for rows.Next() {
		var source entity.Source
		if err := rows.Scan(&source.ID, &source.Name, &source.FeedURL,
			&source.SourceType, &source.LastCrawledAt, &source.Active); err != nil {
			return nil, fmt.Errorf("SearchWithFilters: Scan: %w", err)
		}
		sources = append(sources, &source)
	}
	return sources, rows.Err()
}

func (repo *SourceRepo) Create(ctx context.Context, source *entity.Source) error {
	const query = `
INSERT INTO sources
(name, feed_url, last_crawled_at, active)
VALUES (?, ?, ?, ?)
`

	_, err := repo.db.ExecContext(ctx, query,
		source.Name, source.FeedURL,
		source.LastCrawledAt, source.Active,
	)
	if err != nil {
		return fmt.Errorf("Create: ExecContext: %w", err)
	}
	return nil
}

func (repo *SourceRepo) Update(ctx context.Context, source *entity.Source) error {
	const query = `
UPDATE sources SET
    name            = ?,
    feed_url        = ?,
    last_crawled_at = ?,
    active          = ?
WHERE id = ?
`
	res, err := repo.db.ExecContext(ctx, query,
		source.Name, source.FeedURL,
		source.LastCrawledAt, source.Active, source.ID,
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

func (repo *SourceRepo) Delete(ctx context.Context, id int64) error {
	const query = `DELETE FROM sources WHERE id = ?`

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

func (repo *SourceRepo) TouchCrawledAt(ctx context.Context, id int64, time time.Time) error {
	const query = `UPDATE sources SET last_crawled_at = ? WHERE id = ?`
	_, err := repo.db.ExecContext(ctx, query, time, id)
	return err
}
