package db

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// §4 (+ Phase 2 §6 books + Phase 3 §4 learning) tables in dependency order
// — MigrateUp must create exactly these.
var wantTables = []string{
	"sources", "articles", "summaries",
	"episodes", "segments",
	"subscribers", "viewers", "feed_tokens", "feed_access_logs",
	"jobs",
	"books", "book_chunks",
	"learning_items", "review_logs",
}

// expectMigrationDDL expects the schema part of MigrateUp (extension +
// tables + alters + indexes), without the seed step.
func expectMigrationDDL(mock sqlmock.Sqlmock) {
	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS vector").
		WillReturnResult(sqlmock.NewResult(0, 0))
	for _, table := range wantTables {
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS " + table + " ").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	// Phase 2 upgrade path: ALTER TABLE sources (kind) + CHECK constraint
	// swap (DROP IF EXISTS + ADD — newsletter 追加で値リストを広げる冪等ペア).
	mock.ExpectExec("ALTER TABLE sources ADD COLUMN IF NOT EXISTS kind").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP CONSTRAINT IF EXISTS sources_kind_check").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ADD CONSTRAINT sources_kind_check").
		WillReturnResult(sqlmock.NewResult(0, 0))
	// ソース論理削除の upgrade path: sources.deleted_at。
	mock.ExpectExec("ALTER TABLE sources ADD COLUMN IF NOT EXISTS deleted_at").
		WillReturnResult(sqlmock.NewResult(0, 0))
	// Phase 3 upgrade path: books の book_review 進捗2カラム(§7.3)。
	mock.ExpectExec("ALTER TABLE books ADD COLUMN IF NOT EXISTS review_cursor").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ALTER TABLE books ADD COLUMN IF NOT EXISTS review_status").
		WillReturnResult(sqlmock.NewResult(0, 0))
	for range createIndexStatements {
		mock.ExpectExec("CREATE INDEX IF NOT EXISTS").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
}

// expectSeedCheck expects the "seed only when sources is empty" guard: a
// COUNT query answered with existingSources, followed by the seed INSERT
// only when the table is empty.
func expectSeedCheck(mock sqlmock.Sqlmock, existingSources int) {
	mock.ExpectQuery(`SELECT count\(\*\) FROM sources`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(existingSources))
	if existingSources == 0 {
		mock.ExpectExec("INSERT INTO sources").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
}

func expectFullMigration(mock sqlmock.Sqlmock) {
	expectMigrationDDL(mock)
	expectSeedCheck(mock, 0)
}

func TestMigrateUp_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectFullMigration(mock)

	require.NoError(t, MigrateUp(db))
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMigrateUp_Idempotent: every DDL statement uses IF NOT EXISTS, so
// running twice issues the same SQL and succeeds both times. The seed is
// NOT idempotent-by-rerun anymore: the first run (empty table) seeds, the
// second run sees the seeded rows and must skip the INSERT — otherwise
// sources deleted from the dashboard would resurrect on every restart.
// (実 DB での冪等性は migrate_integration_test.go の
// TestMigrateUp_RealPostgres が TEST_DATABASE_URL 指定時に検証する。)
func TestMigrateUp_Idempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectMigrationDDL(mock)
	expectSeedCheck(mock, 0) // 初回: 空 → シード投入
	expectMigrationDDL(mock)
	expectSeedCheck(mock, 36) // 再起動: 行がある → シードは走らない

	require.NoError(t, MigrateUp(db))
	require.NoError(t, MigrateUp(db))
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMigrateUp_SeedSkippedWhenSourcesExist pins the restart behaviour that
// caused the resurrection bug: with any row present — soft-deleted rows
// count too — the seed INSERT must not be issued at all.
func TestMigrateUp_SeedSkippedWhenSourcesExist(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectMigrationDDL(mock)
	expectSeedCheck(mock, 1)

	require.NoError(t, MigrateUp(db))
	assert.NoError(t, mock.ExpectationsWereMet(),
		"INSERT INTO sources must not run when the table has rows")
}

// TestMigrateUp_ExtensionError: on an image without pgvector the CREATE
// EXTENSION fails and MigrateUp must abort with a message naming the
// required image (U-24 運用上の落とし穴: server が起動不能になる理由を
// ログから即断できるようにする).
func TestMigrateUp_ExtensionError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS vector").
		WillReturnError(sql.ErrConnDone)

	err = MigrateUp(db)
	require.Error(t, err)
	assert.ErrorIs(t, err, sql.ErrConnDone, "original driver error stays unwrappable")
	assert.Contains(t, err.Error(), "pgvector/pgvector:pg18",
		"error must name the required image")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateUp_TableError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS vector").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS sources").
		WillReturnError(sql.ErrConnDone)

	err = MigrateUp(db)
	require.Error(t, err)
	assert.Equal(t, sql.ErrConnDone, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateUp_IndexError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS vector").
		WillReturnResult(sqlmock.NewResult(0, 0))
	for range wantTables {
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	for range alterTableStatements {
		mock.ExpectExec("").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS").
		WillReturnError(sql.ErrTxDone)

	assert.Error(t, MigrateUp(db))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateUp_AlterError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS vector").
		WillReturnResult(sqlmock.NewResult(0, 0))
	for range wantTables {
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec("ALTER TABLE sources ADD COLUMN IF NOT EXISTS kind").
		WillReturnError(sql.ErrTxDone)

	assert.Error(t, MigrateUp(db))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateUp_SeedError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS vector").
		WillReturnResult(sqlmock.NewResult(0, 0))
	for range wantTables {
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	for range alterTableStatements {
		mock.ExpectExec("").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	for range createIndexStatements {
		mock.ExpectExec("CREATE INDEX IF NOT EXISTS").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectQuery(`SELECT count\(\*\) FROM sources`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectExec("INSERT INTO sources").
		WillReturnError(sql.ErrConnDone)

	assert.Error(t, MigrateUp(db))
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMigrateUp_SeedCountError: the empty-check itself failing must abort
// the migration (better a boot failure than silently skipping the seed on a
// genuinely fresh database).
func TestMigrateUp_SeedCountError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectMigrationDDL(mock)
	mock.ExpectQuery(`SELECT count\(\*\) FROM sources`).
		WillReturnError(sql.ErrConnDone)

	err = MigrateUp(db)
	require.Error(t, err)
	assert.ErrorIs(t, err, sql.ErrConnDone)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSchema_MatchesDesignDoc pins load-bearing details of §4 that the
// sqlmock regexes above cannot see.
func TestSchema_MatchesDesignDoc(t *testing.T) {
	all := strings.Join(createTableStatements, "\n")

	tests := []struct {
		name string
		want string
	}{
		{"summaries keyed by article (one summary per article)", "article_id    bigint PRIMARY KEY REFERENCES articles"},
		{"summaries.provider is NOT NULL (fallback observability)", "provider      text NOT NULL"},
		{"segments unique per (episode_id, position)", "UNIQUE (episode_id, position)"},
		{"feed_tokens stores only the hash, unique (D-5)", "token_hash    text NOT NULL UNIQUE"},
		{"subscribers deactivate instead of delete (C-8)", "deactivated_at timestamptz"},
		// D-27 — viewer 閲覧専用アカウント。
		{"viewers.email is the unique login identifier (D-27)", "email          text NOT NULL UNIQUE"},
		{"viewers store only the bcrypt hash (D-27)", "password_hash  text NOT NULL"},
		{"jobs default to pending (C-4 DB queue)", "status        text NOT NULL DEFAULT 'pending'"},
		{"jobs carry a jsonb payload", "payload       jsonb NOT NULL DEFAULT '{}'"},
		{"episodes store the mp3 path, not the blob (C-10)", "audio_path    text NOT NULL"},
		{"sources carry the script corner category", "category      text NOT NULL"},
		{"sources default lang to en", "lang          text NOT NULL DEFAULT 'en'"},
		{"sources default kind to rss (Phase 2 §4)", "kind          text NOT NULL DEFAULT 'rss'"},
		{"sources.kind constrained to rss|youtube|podcast|newsletter", "CHECK (kind IN ('rss', 'youtube', 'podcast', 'newsletter'))"},
		{"sources delete is logical (articles FK 保護)", "deleted_at    timestamptz"},
		{"book_chunks reference books with NOT NULL FK (Phase 2 §6)", "book_id   bigint NOT NULL REFERENCES books"},
		{"book_chunks embedding is 1024-dim (D-12: bge-m3)", "embedding vector(1024)"},
		{"book_chunks unique per (book_id, position)", "UNIQUE (book_id, position)"},
		// Phase 3 §4 — 学習ループ。DDL は設計書の逐語一致が原則。
		{"learning_items start at stage 0 (Phase 3 §4)", "stage        int  NOT NULL DEFAULT 0"},
		{"learning_items.due_on is a date (JST 放送日, Phase 3 §12-10)", "due_on       date NOT NULL"},
		{"learning_items require article_id iff kind='article'", "CHECK ((kind = 'article') = (article_id IS NOT NULL))"},
		{"learning_items require book_id iff kind='book'", "CHECK ((kind = 'book')    = (book_id    IS NOT NULL))"},
		{"review_logs reference items with NOT NULL FK", "item_id     bigint NOT NULL REFERENCES learning_items"},
		{"review_logs.asked_on is a date (JST 放送日)", "asked_on    date NOT NULL"},
		{"review_logs unique per (item_id, asked_on) — 同日 rev 冪等キー", "UNIQUE (item_id, asked_on)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, all, tt.want)
		})
	}
}

// TestSegmentsKind_NoCheckConstraint pins the Phase 3 precondition (Phase 3
// §4 設計メモ): segments.kind is a bare text column, so 'quiz' | 'review' |
// 'book_review' rows insert without any ALTER. If someone adds a CHECK to
// segments later, this fails before the radio batch does. (実 DB 側の確認は
// learning_integration_test.go が行う。)
func TestSegmentsKind_NoCheckConstraint(t *testing.T) {
	for _, stmt := range createTableStatements {
		if strings.Contains(stmt, "CREATE TABLE IF NOT EXISTS segments ") {
			assert.NotContains(t, stmt, "CHECK",
				"segments must stay CHECK-free so Phase 3 kinds insert as-is")
			return
		}
	}
	t.Fatal("segments DDL not found in createTableStatements")
}
