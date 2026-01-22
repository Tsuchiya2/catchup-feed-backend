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
		// ORDER BY published_at DESC で使用(全クエリで使用)
		`CREATE INDEX IF NOT EXISTS idx_articles_published_at ON articles(published_at DESC)`,
		// ソース別記事取得用
		`CREATE INDEX IF NOT EXISTS idx_articles_source_id ON articles(source_id)`,
		// アクティブソース絞り込み用(WHERE active = TRUE)
		`CREATE INDEX IF NOT EXISTS idx_sources_active ON sources(active) WHERE active = TRUE`,
		// ソースタイプ別フィルタリング用(Web Scraper対応)
		`CREATE INDEX IF NOT EXISTS idx_sources_source_type ON sources(source_type)`,
	}

	// pg_trgm拡張を有効化(ILIKE検索高速化用)
	// エラーを無視(既に存在する場合やスーパーユーザー権限がない場合)
	_, _ = db.Exec(`CREATE EXTENSION IF NOT EXISTS pg_trgm`)

	// ILIKE検索用GINインデックス追加(マルチキーワード検索高速化)
	searchIndexes := []string{
		// 記事タイトル・サマリーのILIKE検索用
		`CREATE INDEX IF NOT EXISTS idx_articles_title_gin ON articles USING gin(title gin_trgm_ops)`,
		`CREATE INDEX IF NOT EXISTS idx_articles_summary_gin ON articles USING gin(summary gin_trgm_ops)`,
		// ソース名・URLのILIKE検索用
		`CREATE INDEX IF NOT EXISTS idx_sources_name_gin ON sources USING gin(name gin_trgm_ops)`,
		`CREATE INDEX IF NOT EXISTS idx_sources_feed_url_gin ON sources USING gin(feed_url gin_trgm_ops)`,
	}
	for _, idx := range searchIndexes {
		// pg_trgm拡張がない場合はエラーになるため無視
		_, _ = db.Exec(idx)
	}

	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			return err
		}
	}

	// Web Scraper対応: source_type制約追加
	// PostgreSQL特有の制約構文のため、エラーを無視(既に存在する場合)
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

	// Embedding Feature: pgvector拡張を有効化
	// エラーを無視(既に存在する場合やスーパーユーザー権限がない場合)
	_, _ = db.Exec(`CREATE EXTENSION IF NOT EXISTS vector`)

	// Embedding Feature: article_embeddings テーブル作成
	// Note: article_id is INTEGER to match articles.id (SERIAL = INTEGER)
	// Note: vector(1536) is fixed size for OpenAI text-embedding-3-small model
	//       The dimension column stores metadata for validation purposes
	//       If multi-dimension support is needed, consider separate tables per dimension
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS article_embeddings (
    id              SERIAL PRIMARY KEY,
    article_id      INTEGER NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    embedding_type  VARCHAR(50) NOT NULL,
    provider        VARCHAR(50) NOT NULL,
    model           VARCHAR(100) NOT NULL,
    dimension       INT NOT NULL,
    embedding       vector(1536) NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(article_id, embedding_type, provider, model)
)`); err != nil {
		return err
	}

	// Embedding Feature: article_embeddings インデックス追加
	embeddingIndexes := []string{
		// article_id による検索用 B-tree インデックス
		`CREATE INDEX IF NOT EXISTS idx_article_embeddings_article_id ON article_embeddings(article_id)`,
	}
	for _, idx := range embeddingIndexes {
		if _, err := db.Exec(idx); err != nil {
			return err
		}
	}

	// Embedding Feature: IVFFlat ベクトル類似検索インデックス
	// エラーを無視(pgvector拡張がない場合にエラーとなるため)
	// lists=100 は <1M レコードに適した値
	_, _ = db.Exec(`
CREATE INDEX IF NOT EXISTS idx_article_embeddings_vector
    ON article_embeddings USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100)`)

	// シードデータの投入(重複は自動的にスキップ)
	if _, err := db.Exec(seedSourcesSQL); err != nil {
		return err
	}

	return nil
}

// MigrateDown rolls back the database schema.
// This function removes tables and indexes in reverse order of creation.
// Use with caution: this will delete all data in the affected tables.
func MigrateDown(db *sql.DB) error {
	// Embedding Feature: Drop article_embeddings table and related objects
	// Drop indexes first (CASCADE will handle this automatically, but explicit is safer)
	dropStatements := []string{
		// Drop IVFFlat vector index
		`DROP INDEX IF EXISTS idx_article_embeddings_vector`,
		// Drop article_id index
		`DROP INDEX IF EXISTS idx_article_embeddings_article_id`,
		// Drop article_embeddings table (CASCADE to handle foreign key references)
		`DROP TABLE IF EXISTS article_embeddings CASCADE`,
	}

	for _, stmt := range dropStatements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	// Note: We do NOT drop the vector extension as it may be used by other tables
	// Note: We do NOT drop sources/articles tables as they are core tables

	return nil
}

// MigrateDownEmbeddingsOnly rolls back only the embedding feature.
// This is a targeted rollback that preserves other schema elements.
func MigrateDownEmbeddingsOnly(db *sql.DB) error {
	dropStatements := []string{
		`DROP INDEX IF EXISTS idx_article_embeddings_vector`,
		`DROP INDEX IF EXISTS idx_article_embeddings_article_id`,
		`DROP TABLE IF EXISTS article_embeddings CASCADE`,
	}

	for _, stmt := range dropStatements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}

	return nil
}
