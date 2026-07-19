// Package db provides database connection management and schema migration.
//
// Migration style is inherited from the old system: idempotent SQL
// (CREATE ... IF NOT EXISTS) executed at process startup. The schema below
// is the pulse Phase 1 data model, transcribed verbatim from the design
// document §4 (docs/pulse-phase1-design.md). There is no compatibility
// path from the old catchup-feed schema: the old DB is not migrated (§9,
// source definitions are ported via the seed file only).
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed seeds/sources.sql
var seedSourcesSQL string

// createVectorExtension enables pgvector for book_chunks.embedding
// (Phase 2 §6, U-24). Idempotent; requires the extension to be present in
// the PostgreSQL image (compose/CI/Pi all run pgvector/pgvector:pg18) and
// the connecting role to own the database (POSTGRES_USER does). On a plain
// postgres image this statement fails and MigrateUp aborts with a message
// naming the required image — the server refuses to start rather than boot
// with a half-applied schema.
const createVectorExtension = `CREATE EXTENSION IF NOT EXISTS vector`

// createTableStatements is the §4 schema, one statement per table, in
// dependency order.
var createTableStatements = []string{
	// ===== コンテンツ系(既存概念の継承)=====
	`CREATE TABLE IF NOT EXISTS sources (
    id            bigserial PRIMARY KEY,
    name          text NOT NULL,
    feed_url      text NOT NULL UNIQUE,
    category      text NOT NULL,            -- 台本のコーナー分けに使用
    lang          text NOT NULL DEFAULT 'en',
    kind          text NOT NULL DEFAULT 'rss'
                  CONSTRAINT sources_kind_check
                  CHECK (kind IN ('rss', 'youtube', 'podcast')),  -- Phase 2 §4
    active        boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
)`,
	`CREATE TABLE IF NOT EXISTS articles (
    id            bigserial PRIMARY KEY,
    source_id     bigint NOT NULL REFERENCES sources,
    url           text NOT NULL UNIQUE,
    title         text NOT NULL,
    content       text,                     -- go-readability 抽出全文
    published_at  timestamptz,
    crawled_at    timestamptz NOT NULL DEFAULT now()
)`,
	`CREATE TABLE IF NOT EXISTS summaries (
    article_id    bigint PRIMARY KEY REFERENCES articles,
    body          text NOT NULL,            -- 日本語要約
    provider      text NOT NULL,            -- gemini / groq / ollama(フォールバック観測用)
    created_at    timestamptz NOT NULL DEFAULT now()
)`,
	// ===== ラジオ系(新規)=====
	`CREATE TABLE IF NOT EXISTS episodes (
    id            bigserial PRIMARY KEY,
    feed_kind     text NOT NULL,            -- 'public' | 'private'
    title         text NOT NULL,
    show_notes    text NOT NULL,            -- 記事リンク集。通知にも流用
    audio_path    text NOT NULL,            -- Pi ローカルパス
    audio_bytes   bigint NOT NULL,
    duration_sec  int NOT NULL,
    published_at  timestamptz NOT NULL DEFAULT now()
)`,
	`CREATE TABLE IF NOT EXISTS segments (
    id            bigserial PRIMARY KEY,
    episode_id    bigint NOT NULL REFERENCES episodes,
    position      int NOT NULL,
    kind          text NOT NULL,            -- 'intro'|'news'|'outro'。Phase 3 で 'quiz'|'review' 追加
    article_id    bigint REFERENCES articles,  -- news のとき
    script        text NOT NULL,            -- 読み上げ原稿(検索・振り返り資産)
    UNIQUE (episode_id, position)
)`,
	// ===== 配信・購読系(新規)=====
	`CREATE TABLE IF NOT EXISTS subscribers (
    id             bigserial PRIMARY KEY,
    name           text NOT NULL,
    note           text,                    -- 期待するフィードバックの種類など
    email          text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    deactivated_at timestamptz              -- NULL = アクティブ
)`,
	// viewers: 友人向け閲覧専用アカウント(D-27)。subscribers(ポッドキャスト
	// の視聴コントロール)とは別エンティティ — こちらは Web ダッシュボードへの
	// アクセスコントロール。無効化は論理(deactivated_at)、削除は物理。
	// CREATE TABLE IF NOT EXISTS なので既存 DB への追加マイグレーションとして安全。
	`CREATE TABLE IF NOT EXISTS viewers (
    id             bigserial PRIMARY KEY,
    name           text NOT NULL,
    email          text NOT NULL UNIQUE,
    password_hash  text NOT NULL,            -- bcrypt(admin が作成時に設定)
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    deactivated_at timestamptz               -- NULL = アクティブ
)`,
	`CREATE TABLE IF NOT EXISTS feed_tokens (
    id            bigserial PRIMARY KEY,
    subscriber_id bigint NOT NULL REFERENCES subscribers,
    token_hash    text NOT NULL UNIQUE,     -- 32byte 乱数(base64url)の SHA-256 hex。平文は発行時のみ表示(D-5)
    created_at    timestamptz NOT NULL DEFAULT now(),
    revoked_at    timestamptz               -- NULL = 有効
)`,
	`CREATE TABLE IF NOT EXISTS feed_access_logs (
    id            bigserial PRIMARY KEY,
    token_id      bigint NOT NULL REFERENCES feed_tokens,
    episode_id    bigint REFERENCES episodes,  -- NULL = feed.xml 取得
    user_agent    text,
    accessed_at   timestamptz NOT NULL DEFAULT now()
)`,
	// ===== ジョブ連携(worker/radio 間)=====
	`CREATE TABLE IF NOT EXISTS jobs (
    id            bigserial PRIMARY KEY,
    kind          text NOT NULL,            -- 'regenerate_feed' | 'notify_episode' など
    payload       jsonb NOT NULL DEFAULT '{}',
    status        text NOT NULL DEFAULT 'pending',  -- pending|running|done|failed
    attempts      int NOT NULL DEFAULT 0,
    last_error    text,
    run_after     timestamptz NOT NULL DEFAULT now(),
    created_at    timestamptz NOT NULL DEFAULT now()
)`,
	// ===== 書籍 RAG(Phase 2 §6)=====
	// Go コードからのアクセスは Phase 2 では発生しない(書き込み・検索は
	// Python の pulse-books)。リポジトリ層・entity は右サイズ原則で作らない。
	// DDL の正: catchup-feed-ai tests/test_books_db_integration.py(設計書
	// Phase 2 §4 と同一)。embedding の次元は D-12 決定の bge-m3(1024次元)。
	`CREATE TABLE IF NOT EXISTS books (
  id          bigserial PRIMARY KEY,
  title       text NOT NULL,
  file_path   text NOT NULL,
  imported_at timestamptz NOT NULL DEFAULT now()
)`,
	`CREATE TABLE IF NOT EXISTS book_chunks (
  id        bigserial PRIMARY KEY,
  book_id   bigint NOT NULL REFERENCES books,
  position  int NOT NULL,
  content   text NOT NULL,
  embedding vector(1024),
  UNIQUE (book_id, position)
)`,
	// ===== 学習ループ(Phase 3 §4)=====
	// DDL は docs/pulse-phase3-design.md §4 の逐語転記。
	// segments.kind は Phase 1 から text 型・CHECK なしで 'quiz'|'review'|
	// 'book_review' を予約済みのため ALTER 不要(§4 設計メモ。実 DB での確認は
	// learning_integration_test.go)。
	`CREATE TABLE IF NOT EXISTS learning_items (
  id           bigserial PRIMARY KEY,
  kind         text NOT NULL,                 -- 'article' | 'book'
  article_id   bigint REFERENCES articles,    -- kind='article' のとき必須
  book_id      bigint REFERENCES books,       -- kind='book' のとき必須
  concept      text NOT NULL,                 -- 学習項目の見出し(1行。トラッカー・週次振り返り・ショーノートで使用)
  question     text NOT NULL,                 -- 読み上げ用クイズ文(ラジオ口調の日本語)
  answer       text NOT NULL,                 -- 読み上げ用の答え+一言解説
  provider     text NOT NULL,                 -- 生成 LLM(gemini/groq/ollama。kind='book' は ollama 固定 = C-12)
  stage        int  NOT NULL DEFAULT 0,       -- 間隔ラダーの現在段(0 起点)
  due_on       date NOT NULL,                 -- 次回出題予定日
  retired_at   timestamptz,                   -- NULL = 現役。卒業(ラダー完走)or 手動アーカイブ
  created_at   timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind = 'article') = (article_id IS NOT NULL)),
  CHECK ((kind = 'book')    = (book_id    IS NOT NULL))
)`,
	`CREATE TABLE IF NOT EXISTS review_logs (
  id          bigserial PRIMARY KEY,
  item_id     bigint NOT NULL REFERENCES learning_items,
  episode_id  bigint REFERENCES episodes,     -- どのエピソードで出題したか
  asked_on    date NOT NULL,                  -- 出題日(JST の放送日)
  result      text,                           -- 'good' | 'fuzzy' | 'forgot' | NULL = 未採点
  graded_at   timestamptz,                    -- 採点時刻(自動解決時は NULL のまま result='auto' — §6)
  UNIQUE (item_id, asked_on)                  -- 同日 rev 再実行(radio の冪等仕様)の冪等キー
)`,
}

// alterTableStatements upgrade a database created by an earlier schema
// version (CREATE TABLE IF NOT EXISTS is a no-op on existing tables, so
// column additions need explicit idempotent ALTERs). Executed after the
// CREATE TABLEs, before the indexes.
//
//   - sources.kind (Phase 2 §4): the constant DEFAULT 'rss' is recorded in
//     the catalog only (PostgreSQL 11+ ADD COLUMN with a constant default
//     does not rewrite the table); existing Phase 1 rows simply read back
//     'rss', keeping them fully compatible. The CHECK constraint is
//     added via a DO block because PostgreSQL has no ADD CONSTRAINT IF NOT
//     EXISTS; duplicate_object makes the re-run a no-op (fresh databases
//     already get the constraint inline from CREATE TABLE).
//   - books.review_cursor / books.review_status (Phase 3 §7.3): book_review
//     progress lives on the books row (専用テーブルは過剰). The canonical
//     books CREATE TABLE is owned by catchup-feed-ai (Phase 2 §6), so the
//     Phase 3 columns are added here as idempotent ALTERs only — fresh and
//     existing databases both go through the same path. review_status takes
//     'idle' | 'active' | 'finished'; deliberately NO CHECK constraint: the
//     "active は同時に最大1冊" exclusivity is a cross-row invariant that a
//     column CHECK cannot express, so it is enforced in the application
//     layer (設計書 §7.3, 管理 API の activate が担う).
var alterTableStatements = []string{
	`ALTER TABLE sources ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'rss'`,
	`DO $$
BEGIN
    ALTER TABLE sources ADD CONSTRAINT sources_kind_check
        CHECK (kind IN ('rss', 'youtube', 'podcast'));
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$`,
	`ALTER TABLE books ADD COLUMN IF NOT EXISTS review_cursor int NOT NULL DEFAULT 0`,
	`ALTER TABLE books ADD COLUMN IF NOT EXISTS review_status text NOT NULL DEFAULT 'idle'`,
}

// createIndexStatements are implementation-need indexes beyond §4 (which
// only specifies constraints). Kept deliberately small — single-user scale:
//   - idx_articles_published_at: every article listing / radio article
//     selection orders by published_at DESC.
//   - idx_articles_source_id: FK join sources<->articles used by all
//     "with source" queries.
//   - idx_jobs_pending: partial index backing the ClaimNext polling query
//     (WHERE status='pending' AND run_after <= now()).
//   - idx_feed_access_logs_token_id: per-friend access aggregation on the
//     only table expected to grow unbounded.
var createIndexStatements = []string{
	`CREATE INDEX IF NOT EXISTS idx_articles_published_at ON articles (published_at DESC)`,
	`CREATE INDEX IF NOT EXISTS idx_articles_source_id ON articles (source_id)`,
	`CREATE INDEX IF NOT EXISTS idx_jobs_pending ON jobs (run_after) WHERE status = 'pending'`,
	`CREATE INDEX IF NOT EXISTS idx_feed_access_logs_token_id ON feed_access_logs (token_id)`,
}

// MigrateUp applies the pulse schema (Phase 1 §4 + Phase 2 §4/§6 + Phase 3
// §4 差分). It is idempotent and safe to run at every process startup.
func MigrateUp(db *sql.DB) error {
	// U-24: the vector type must exist before CREATE TABLE book_chunks.
	if _, err := db.Exec(createVectorExtension); err != nil {
		return fmt.Errorf(
			"enable pgvector extension (U-24): book_chunks.embedding requires a pgvector-enabled PostgreSQL image such as pgvector/pgvector:pg18: %w", err)
	}
	for _, stmt := range createTableStatements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	for _, stmt := range alterTableStatements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	for _, stmt := range createIndexStatements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	// ソース定義の手動移植(§9)。ON CONFLICT DO NOTHING で冪等。
	if _, err := db.Exec(seedSourcesSQL); err != nil {
		return err
	}
	return nil
}
