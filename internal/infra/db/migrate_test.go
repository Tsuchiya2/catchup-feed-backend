package db

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// §4 (+ Phase 2 §6 books) tables in dependency order — MigrateUp must
// create exactly these.
var wantTables = []string{
	"sources", "articles", "summaries",
	"episodes", "segments",
	"subscribers", "feed_tokens", "feed_access_logs",
	"jobs",
	"books", "book_chunks",
}

func expectFullMigration(mock sqlmock.Sqlmock) {
	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS vector").
		WillReturnResult(sqlmock.NewResult(0, 0))
	for _, table := range wantTables {
		mock.ExpectExec("CREATE TABLE IF NOT EXISTS " + table + " ").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	// Phase 2 upgrade path: ALTER TABLE sources (kind) + DO block (CHECK).
	mock.ExpectExec("ALTER TABLE sources ADD COLUMN IF NOT EXISTS kind").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("sources_kind_check").
		WillReturnResult(sqlmock.NewResult(0, 0))
	for range createIndexStatements {
		mock.ExpectExec("CREATE INDEX IF NOT EXISTS").
			WillReturnResult(sqlmock.NewResult(0, 0))
	}
	mock.ExpectExec("INSERT INTO sources").
		WillReturnResult(sqlmock.NewResult(0, 0))
}

func TestMigrateUp_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectFullMigration(mock)

	require.NoError(t, MigrateUp(db))
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestMigrateUp_Idempotent: every statement uses IF NOT EXISTS / ON CONFLICT
// DO NOTHING, so running twice issues the same SQL and succeeds both times.
// (実 DB での冪等性は migrate_integration_test.go の
// TestMigrateUp_RealPostgres が TEST_DATABASE_URL 指定時に検証する。)
func TestMigrateUp_Idempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	expectFullMigration(mock)
	expectFullMigration(mock)

	require.NoError(t, MigrateUp(db))
	require.NoError(t, MigrateUp(db))
	assert.NoError(t, mock.ExpectationsWereMet())
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
	mock.ExpectExec("INSERT INTO sources").
		WillReturnError(sql.ErrConnDone)

	assert.Error(t, MigrateUp(db))
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
		{"jobs default to pending (C-4 DB queue)", "status        text NOT NULL DEFAULT 'pending'"},
		{"jobs carry a jsonb payload", "payload       jsonb NOT NULL DEFAULT '{}'"},
		{"episodes store the mp3 path, not the blob (C-10)", "audio_path    text NOT NULL"},
		{"sources carry the script corner category", "category      text NOT NULL"},
		{"sources default lang to en", "lang          text NOT NULL DEFAULT 'en'"},
		{"sources default kind to rss (Phase 2 §4)", "kind          text NOT NULL DEFAULT 'rss'"},
		{"sources.kind constrained to rss|youtube|podcast", "CHECK (kind IN ('rss', 'youtube', 'podcast'))"},
		{"book_chunks reference books with NOT NULL FK (Phase 2 §6)", "book_id   bigint NOT NULL REFERENCES books"},
		{"book_chunks embedding is 1024-dim (D-12: bge-m3)", "embedding vector(1024)"},
		{"book_chunks unique per (book_id, position)", "UNIQUE (book_id, position)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, all, tt.want)
		})
	}
}
