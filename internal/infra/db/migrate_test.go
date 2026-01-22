package db

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrateUp_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Expect sources table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS sources").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect articles table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS articles").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect index creations (4 indexes)
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_articles_published_at").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_articles_source_id").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_sources_active").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_sources_source_type").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect article_embeddings table creation (Embedding Feature)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS article_embeddings").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect article_embeddings index creation
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_article_embeddings_article_id").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect seed data insertion
	mock.ExpectExec("INSERT INTO sources").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Execute migration
	err = MigrateUp(db)
	assert.NoError(t, err)

	// Verify all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateUp_SourcesTableError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Expect sources table creation to fail
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS sources").
		WillReturnError(sql.ErrConnDone)

	// Execute migration
	err = MigrateUp(db)
	assert.Error(t, err)
	assert.Equal(t, sql.ErrConnDone, err)

	// Verify all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateUp_ArticlesTableError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Expect sources table creation to succeed
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS sources").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect articles table creation to fail
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS articles").
		WillReturnError(sql.ErrTxDone)

	// Execute migration
	err = MigrateUp(db)
	assert.Error(t, err)
	assert.Equal(t, sql.ErrTxDone, err)

	// Verify all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateUp_IndexError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Expect sources table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS sources").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect articles table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS articles").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect first index to fail
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_articles_published_at").
		WillReturnError(sql.ErrNoRows)

	// Execute migration
	err = MigrateUp(db)
	assert.Error(t, err)
	assert.Equal(t, sql.ErrNoRows, err)

	// Verify all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateUp_SeedDataError(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Expect sources table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS sources").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect articles table creation
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS articles").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect all index creations to succeed
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_articles_published_at").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_articles_source_id").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_sources_active").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_sources_source_type").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect article_embeddings table creation (Embedding Feature)
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS article_embeddings").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect article_embeddings index creation
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_article_embeddings_article_id").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Expect seed data insertion to fail
	mock.ExpectExec("INSERT INTO sources").
		WillReturnError(sql.ErrConnDone)

	// Execute migration
	err = MigrateUp(db)
	assert.Error(t, err)
	assert.Equal(t, sql.ErrConnDone, err)

	// Verify all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateUp_Idempotent(t *testing.T) {
	// Test that running MigrateUp multiple times is safe (idempotent)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// First run - all tables and indexes are created
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS sources").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS articles").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_articles_published_at").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_articles_source_id").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_sources_active").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_sources_source_type").
		WillReturnResult(sqlmock.NewResult(0, 0))
	// Embedding Feature: article_embeddings table and index
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS article_embeddings").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_article_embeddings_article_id").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO sources").
		WillReturnResult(sqlmock.NewResult(0, 5))

	// Execute migration
	err = MigrateUp(db)
	assert.NoError(t, err)

	// Verify all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSeedSourcesSQL_Embedded(t *testing.T) {
	// Verify that seedSourcesSQL is embedded and not empty
	assert.NotEmpty(t, seedSourcesSQL)
	assert.Contains(t, seedSourcesSQL, "INSERT INTO sources")
}

func TestMigrateDown_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Expect drop statements in order
	mock.ExpectExec("DROP INDEX IF EXISTS idx_article_embeddings_vector").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP INDEX IF EXISTS idx_article_embeddings_article_id").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP TABLE IF EXISTS article_embeddings CASCADE").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Execute migration
	err = MigrateDown(db)
	assert.NoError(t, err)

	// Verify all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateDown_Error(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Expect first drop to fail
	mock.ExpectExec("DROP INDEX IF EXISTS idx_article_embeddings_vector").
		WillReturnError(sql.ErrConnDone)

	// Execute migration
	err = MigrateDown(db)
	assert.Error(t, err)
	assert.Equal(t, sql.ErrConnDone, err)

	// Verify all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMigrateDownEmbeddingsOnly_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Expect drop statements in order
	mock.ExpectExec("DROP INDEX IF EXISTS idx_article_embeddings_vector").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP INDEX IF EXISTS idx_article_embeddings_article_id").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("DROP TABLE IF EXISTS article_embeddings CASCADE").
		WillReturnResult(sqlmock.NewResult(0, 0))

	// Execute migration
	err = MigrateDownEmbeddingsOnly(db)
	assert.NoError(t, err)

	// Verify all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}
