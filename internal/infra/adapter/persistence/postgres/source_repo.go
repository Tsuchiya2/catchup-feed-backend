package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/pkg/search"
	"catchup-feed/internal/repository"
)

// sourceColumns is the §4 sources column list used by every SELECT.
const sourceColumns = "id, name, feed_url, category, lang, active, created_at"

type SourceRepo struct{ db *sql.DB }

func NewSourceRepo(db *sql.DB) repository.SourceRepository {
	return &SourceRepo{db: db}
}

// scanner abstracts *sql.Row / *sql.Rows for shared scan helpers.
type scanner interface {
	Scan(dest ...any) error
}

func scanSource(s scanner) (*entity.Source, error) {
	var source entity.Source
	if err := s.Scan(
		&source.ID, &source.Name, &source.FeedURL,
		&source.Category, &source.Lang, &source.Active, &source.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &source, nil
}

func (repo *SourceRepo) querySources(ctx context.Context, op, query string, args ...any) ([]*entity.Source, error) {
	rows, err := repo.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer func() { _ = rows.Close() }()

	sources := make([]*entity.Source, 0, 50)
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (repo *SourceRepo) Get(ctx context.Context, id int64) (*entity.Source, error) {
	query := `
SELECT ` + sourceColumns + `
FROM sources
WHERE id = $1
LIMIT 1`
	source, err := scanSource(repo.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get: %w", err)
	}
	return source, nil
}

func (repo *SourceRepo) List(ctx context.Context) ([]*entity.Source, error) {
	query := `
SELECT ` + sourceColumns + `
FROM sources
ORDER BY id ASC`
	return repo.querySources(ctx, "List", query)
}

func (repo *SourceRepo) ListActive(ctx context.Context) ([]*entity.Source, error) {
	query := `
SELECT ` + sourceColumns + `
FROM sources
WHERE active = TRUE
ORDER BY id ASC`
	return repo.querySources(ctx, "ListActive", query)
}

func (repo *SourceRepo) Search(ctx context.Context, kw string) ([]*entity.Source, error) {
	query := `
SELECT ` + sourceColumns + `
FROM sources
WHERE name     ILIKE $1
OR feed_url ILIKE $1
ORDER BY id ASC`
	return repo.querySources(ctx, "Search", query, "%"+kw+"%")
}

// SearchWithFilters searches sources with multi-keyword AND logic and optional filters
func (repo *SourceRepo) SearchWithFilters(
	ctx context.Context,
	keywords []string,
	filters repository.SourceSearchFilters,
) ([]*entity.Source, error) {
	// Apply search timeout to prevent long-running queries
	ctx, cancel := context.WithTimeout(ctx, search.DefaultSearchTimeout)
	defer cancel()

	// Build WHERE clause conditions
	var conditions []string
	var args []interface{}
	paramIndex := 1

	// Add keyword conditions (AND logic between keywords, OR logic within each keyword)
	for _, kw := range keywords {
		escapedKeyword := search.EscapeILIKE(kw)
		conditions = append(conditions, fmt.Sprintf(
			"(name ILIKE $%d OR feed_url ILIKE $%d)",
			paramIndex, paramIndex,
		))
		args = append(args, escapedKeyword)
		paramIndex++
	}

	// Add category filter if provided
	if filters.Category != nil {
		conditions = append(conditions, fmt.Sprintf("category = $%d", paramIndex))
		args = append(args, *filters.Category)
		paramIndex++
	}

	// Add active filter if provided
	if filters.Active != nil {
		conditions = append(conditions, fmt.Sprintf("active = $%d", paramIndex))
		args = append(args, *filters.Active)
	}

	// Build final query with dynamic WHERE clause
	query := `
SELECT ` + sourceColumns + `
FROM sources`
	if len(conditions) > 0 {
		query += "\nWHERE " + strings.Join(conditions, "\n  AND ")
	}
	query += "\nORDER BY id ASC"

	return repo.querySources(ctx, "SearchWithFilters", query, args...)
}

func (repo *SourceRepo) Create(ctx context.Context, source *entity.Source) error {
	if source.Lang == "" {
		source.Lang = entity.DefaultSourceLang
	}
	const query = `
INSERT INTO sources (name, feed_url, category, lang, active)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, created_at`
	err := repo.db.QueryRowContext(ctx, query,
		source.Name, source.FeedURL, source.Category, source.Lang, source.Active,
	).Scan(&source.ID, &source.CreatedAt)
	if err != nil {
		return fmt.Errorf("Create: %w", err)
	}
	return nil
}

func (repo *SourceRepo) Update(ctx context.Context, source *entity.Source) error {
	if source.Lang == "" {
		source.Lang = entity.DefaultSourceLang
	}
	const query = `
UPDATE sources SET
       name     = $1,
       feed_url = $2,
       category = $3,
       lang     = $4,
       active   = $5
WHERE id = $6`
	res, err := repo.db.ExecContext(ctx, query,
		source.Name, source.FeedURL, source.Category,
		source.Lang, source.Active, source.ID,
	)
	if err != nil {
		return fmt.Errorf("Update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("Update: no rows affected")
	}
	return nil
}

func (repo *SourceRepo) Delete(ctx context.Context, id int64) error {
	const query = `DELETE FROM sources WHERE id = $1`
	res, err := repo.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("Delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("Delete: no rows affected")
	}
	return nil
}
