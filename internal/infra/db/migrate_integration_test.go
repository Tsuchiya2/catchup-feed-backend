package db

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateUp_RealPostgres runs the full migration twice against a real
// PostgreSQL and smokes the §4 schema. Skipped unless TEST_DATABASE_URL is
// set, e.g.:
//
//	docker run -d --rm -e POSTGRES_PASSWORD=test -e POSTGRES_USER=test \
//	  -e POSTGRES_DB=pulse_test -p 55432:5432 pgvector/pgvector:pg18
//	TEST_DATABASE_URL='postgres://test:test@localhost:55432/pulse_test?sslmode=disable' \
//	  go test ./internal/infra/db/ -run RealPostgres -v
func TestMigrateUp_RealPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping real-postgres migration test")
	}

	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	require.NoError(t, conn.Ping())

	// Idempotency: the second run must be a no-op, not an error.
	require.NoError(t, MigrateUp(conn), "first MigrateUp")
	require.NoError(t, MigrateUp(conn), "second MigrateUp (idempotency)")

	// All §4 tables exist.
	for _, table := range wantTables {
		var exists bool
		err := conn.QueryRow(
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`,
			table,
		).Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists, "table %s must exist", table)
	}

	// Seeded source definitions (§9 手動移植) landed and are idempotent.
	var seeded int
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM sources`).Scan(&seeded))
	assert.Greater(t, seeded, 0, "seed sources inserted")

	// Smoke the jobs claim query shape (C-4) against the real schema.
	_, err = conn.Exec(`INSERT INTO jobs (kind) VALUES ('regenerate_feed')`)
	require.NoError(t, err)
	var claimedKind string
	err = conn.QueryRow(`
UPDATE jobs SET status='running', attempts=attempts+1
WHERE id = (
    SELECT id FROM jobs
    WHERE status = 'pending' AND run_after <= now()
    ORDER BY run_after ASC, id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING kind`).Scan(&claimedKind)
	require.NoError(t, err)
	assert.Equal(t, "regenerate_feed", claimedKind)
}
