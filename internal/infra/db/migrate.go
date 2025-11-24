package db

import (
	"database/sql"
	_ "embed"
)

//go:embed seeds/sources.sql
var seedSourcesSQL string

func MigrateUp(db *sql.DB) error {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS sources (
    id              SERIAL PRIMARY KEY,
    name            TEXT NOT NULL,
    feed_url        TEXT NOT NULL UNIQUE,
    last_crawled_at TIMESTAMPTZ,
    active          BOOLEAN DEFAULT TRUE,
    source_type     VARCHAR(20) NOT NULL DEFAULT 'RSS',
    scraper_config  JSONB
)`); err != nil {
		return err
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS articles (
    id           SERIAL PRIMARY KEY,
    source_id    INTEGER REFERENCES sources(id),
    title        TEXT NOT NULL,
    url          TEXT UNIQUE,
    summary      TEXT,
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ DEFAULT now()
)`); err != nil {
		return err
	}

	// パフォーマンス最適化: インデックス追加
	indexes := []string{
		// ORDER BY published_at DESC で使用（全クエリで使用）
		`CREATE INDEX IF NOT EXISTS idx_articles_published_at ON articles(published_at DESC)`,
		// ソース別記事取得用
		`CREATE INDEX IF NOT EXISTS idx_articles_source_id ON articles(source_id)`,
		// アクティブソース絞り込み用（WHERE active = TRUE）
		`CREATE INDEX IF NOT EXISTS idx_sources_active ON sources(active) WHERE active = TRUE`,
		// ソースタイプ別フィルタリング用（Web Scraper対応）
		`CREATE INDEX IF NOT EXISTS idx_sources_source_type ON sources(source_type)`,
	}

	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return err
		}
	}

	// Web Scraper対応: source_type制約追加
	// PostgreSQL特有の制約構文のため、エラーを無視（既に存在する場合）
	_, _ = db.Exec(`
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'chk_source_type'
    ) THEN
        ALTER TABLE sources ADD CONSTRAINT chk_source_type
        CHECK (source_type IN ('RSS', 'Webflow', 'NextJS', 'Remix'));
    END IF;
END $$;
`)

	// シードデータの投入（重複は自動的にスキップ）
	if _, err := db.Exec(seedSourcesSQL); err != nil {
		return err
	}

	return nil
}
