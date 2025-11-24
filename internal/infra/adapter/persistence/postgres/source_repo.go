package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

type SourceRepo struct{ db *sql.DB }

func NewSourceRepo(db *sql.DB) repository.SourceRepository {
	return &SourceRepo{db: db}
}

// scanSource is a helper function to scan a source row including scraper_config
func scanSource(rows *sql.Rows) (*entity.Source, error) {
	var source entity.Source
	var scraperConfigJSON []byte
	if err := rows.Scan(
		&source.ID, &source.Name, &source.FeedURL, &source.LastCrawledAt, &source.Active,
		&source.SourceType, &scraperConfigJSON,
	); err != nil {
		return nil, err
	}

	// Unmarshal scraper_config if present
	if len(scraperConfigJSON) > 0 {
		var config entity.ScraperConfig
		if err := json.Unmarshal(scraperConfigJSON, &config); err != nil {
			return nil, fmt.Errorf("unmarshal scraper_config: %w", err)
		}
		source.ScraperConfig = &config
	}

	return &source, nil
}

func (repo *SourceRepo) Get(ctx context.Context, id int64) (*entity.Source, error) {
	const query = `
SELECT id, name, feed_url, last_crawled_at, active, source_type, scraper_config
FROM sources
WHERE id = $1
LIMIT 1`
	var source entity.Source
	var scraperConfigJSON []byte
	err := repo.db.QueryRowContext(ctx, query, id).Scan(
		&source.ID, &source.Name, &source.FeedURL, &source.LastCrawledAt, &source.Active,
		&source.SourceType, &scraperConfigJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get: %w", err)
	}

	// Unmarshal scraper_config if present
	if len(scraperConfigJSON) > 0 {
		var config entity.ScraperConfig
		if err := json.Unmarshal(scraperConfigJSON, &config); err != nil {
			return nil, fmt.Errorf("Get: unmarshal scraper_config: %w", err)
		}
		source.ScraperConfig = &config
	}

	return &source, nil
}

func (repo *SourceRepo) List(ctx context.Context) ([]*entity.Source, error) {
	const query = `
SELECT id, name, feed_url, last_crawled_at, active, source_type, scraper_config
FROM sources
ORDER BY id ASC`
	rows, err := repo.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	sources := make([]*entity.Source, 0, 50)
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, fmt.Errorf("List: %w", err)
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (repo *SourceRepo) ListActive(ctx context.Context) ([]*entity.Source, error) {
	const query = `
SELECT id, name, feed_url, last_crawled_at, active, source_type, scraper_config
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
		source, err := scanSource(rows)
		if err != nil {
			return nil, fmt.Errorf("ListActive: %w", err)
		}
		activeSource = append(activeSource, source)
	}
	return activeSource, rows.Err()
}

func (repo *SourceRepo) Search(ctx context.Context, kw string) ([]*entity.Source, error) {
	const query = `
SELECT id, name, feed_url, last_crawled_at, active, source_type, scraper_config
FROM sources
WHERE name     ILIKE $1
OR feed_url ILIKE $1
ORDER BY id ASC`
	param := "%" + kw + "%"
	rows, err := repo.db.QueryContext(ctx, query, param)
	if err != nil {
		return nil, fmt.Errorf("Search: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// パフォーマンス最適化: メモリ再割り当てを削減するため事前割り当て
	sources := make([]*entity.Source, 0, 50)
	for rows.Next() {
		source, err := scanSource(rows)
		if err != nil {
			return nil, fmt.Errorf("Search: %w", err)
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (repo *SourceRepo) Create(ctx context.Context, source *entity.Source) error {
	// Default to RSS if source_type is empty
	if source.SourceType == "" {
		source.SourceType = "RSS"
	}

	// Marshal scraper_config to JSON if present
	var scraperConfigJSON []byte
	if source.ScraperConfig != nil {
		var err error
		scraperConfigJSON, err = json.Marshal(source.ScraperConfig)
		if err != nil {
			return fmt.Errorf("Create: marshal scraper_config: %w", err)
		}
	}

	const query = `
INSERT INTO sources (name, feed_url, last_crawled_at, active, source_type, scraper_config)
VALUES ($1, $2, $3, $4, $5, $6)`
	_, err := repo.db.ExecContext(ctx, query,
		source.Name, source.FeedURL,
		source.LastCrawledAt, source.Active,
		source.SourceType, scraperConfigJSON,
	)
	if err != nil {
		return fmt.Errorf("Create: %w", err)
	}
	return nil
}

func (repo *SourceRepo) Update(ctx context.Context, source *entity.Source) error {
	// Default to RSS if source_type is empty
	if source.SourceType == "" {
		source.SourceType = "RSS"
	}

	// Marshal scraper_config to JSON if present
	var scraperConfigJSON []byte
	if source.ScraperConfig != nil {
		var err error
		scraperConfigJSON, err = json.Marshal(source.ScraperConfig)
		if err != nil {
			return fmt.Errorf("Update: marshal scraper_config: %w", err)
		}
	}

	const query = `
UPDATE sources SET
       name            = $1,
       feed_url        = $2,
       last_crawled_at = $3,
       active          = $4,
       source_type     = $5,
       scraper_config  = $6
WHERE id = $7`
	res, err := repo.db.ExecContext(ctx, query,
		source.Name, source.FeedURL,
		source.LastCrawledAt, source.Active,
		source.SourceType, scraperConfigJSON, source.ID,
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

func (repo *SourceRepo) TouchCrawledAt(ctx context.Context, id int64, time time.Time) error {
	const query = `UPDATE sources SET last_crawled_at = $1 WHERE id = $2`
	_, err := repo.db.ExecContext(ctx, query, time, id)
	return err
}
