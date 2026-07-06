package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
)

// openTestDB connects to TEST_DATABASE_URL or skips the test.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping real-postgres test")
	}
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.Ping())
	return conn
}

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

// TestSourcesKind_RealPostgres verifies the Phase 2 §4 sources.kind
// migration against a real PostgreSQL:
//   - the column exists with DEFAULT 'rss' (existing rows / seeds are
//     rescued by the default: Phase 1 完全互換)
//   - the CHECK constraint rejects values outside rss|youtube|podcast
//   - the upgrade path (Phase 1 table without kind → ALTER adds it) is
//     covered by dropping the column and re-running MigrateUp
func TestSourcesKind_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))

	// Seeded rows (and any pre-existing row) carry kind='rss' via DEFAULT.
	var nonRSS int
	require.NoError(t, conn.QueryRow(
		`SELECT count(*) FROM sources WHERE kind <> 'rss'`).Scan(&nonRSS))
	assert.Zero(t, nonRSS, "all seeded/pre-existing sources must default to kind='rss'")

	// CHECK constraint rejects invalid kinds.
	_, err := conn.Exec(
		`INSERT INTO sources (name, feed_url, category, kind) VALUES ('bad', $1, 'dev', 'newsletter')`,
		fmt.Sprintf("https://invalid-kind.example.com/%d", time.Now().UnixNano()))
	require.Error(t, err, "kind outside rss|youtube|podcast must violate sources_kind_check")
	assert.Contains(t, err.Error(), "sources_kind_check")

	// Valid kinds are accepted.
	feedURL := fmt.Sprintf("https://podcast.example.com/%d.rss", time.Now().UnixNano())
	var srcID int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO sources (name, feed_url, category, kind) VALUES ('pod', $1, 'dev', 'podcast') RETURNING id`,
		feedURL).Scan(&srcID))
	defer func() { _, _ = conn.Exec(`DELETE FROM sources WHERE id = $1`, srcID) }()

	// Upgrade path: a Phase 1 database has the table but not the column.
	// Dropping the column (which also drops its CHECK constraint) and
	// re-running MigrateUp must restore both.
	_, err = conn.Exec(`ALTER TABLE sources DROP COLUMN kind`)
	require.NoError(t, err)
	require.NoError(t, MigrateUp(conn), "MigrateUp on a kind-less schema (Phase 1 → 2 upgrade)")

	var kind string
	require.NoError(t, conn.QueryRow(
		`SELECT kind FROM sources WHERE id = $1`, srcID).Scan(&kind))
	assert.Equal(t, "rss", kind, "re-added column backfills DEFAULT 'rss'")

	var constraintExists bool
	require.NoError(t, conn.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'sources_kind_check')`,
	).Scan(&constraintExists))
	assert.True(t, constraintExists, "CHECK constraint restored by the DO block")
}

// TestTranscribeEnqueue_RealPostgres proves the Phase 2 §5 Pi-side contract
// against a real PostgreSQL:
//   - CreateWithTranscribeJob lands the content-NULL article and the
//     kind='transcribe' job atomically with the documented payload
//   - the Pi worker's consumer, which claims only its registered kinds,
//     never claims transcribe jobs (they stay pending for the Mac worker)
func TestTranscribeEnqueue_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))

	nano := time.Now().UnixNano()
	feedURL := fmt.Sprintf("https://enqueue.example.com/%d.rss", nano)
	var srcID int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO sources (name, feed_url, category, kind, active)
		 VALUES ('enqueue-test', $1, 'dev', 'podcast', false) RETURNING id`,
		feedURL).Scan(&srcID))
	defer func() { _, _ = conn.Exec(`DELETE FROM sources WHERE id = $1`, srcID) }()

	articles := pgRepo.NewArticleRepo(conn)
	art := &entity.Article{
		SourceID:    srcID,
		Title:       "Ep 1",
		URL:         fmt.Sprintf("https://enqueue.example.com/%d/ep1", nano),
		PublishedAt: time.Now(),
	}
	mediaURL := fmt.Sprintf("https://cdn.example.com/%d/ep1.mp3", nano)
	require.NoError(t, articles.CreateWithTranscribeJob(
		context.Background(), art, mediaURL, entity.SourceKindPodcast))
	defer func() {
		_, _ = conn.Exec(`DELETE FROM jobs WHERE kind = 'transcribe' AND payload->>'article_id' = $1`,
			fmt.Sprint(art.ID))
		_, _ = conn.Exec(`DELETE FROM articles WHERE id = $1`, art.ID)
	}()

	// Article stored with content NULL (§4: NULL のうちは要約対象外).
	var contentIsNull bool
	require.NoError(t, conn.QueryRow(
		`SELECT content IS NULL FROM articles WHERE id = $1`, art.ID).Scan(&contentIsNull))
	assert.True(t, contentIsNull)

	// Job row carries the documented payload contract.
	var payloadRaw []byte
	require.NoError(t, conn.QueryRow(
		`SELECT payload FROM jobs WHERE kind = 'transcribe' AND payload->>'article_id' = $1 AND status = 'pending'`,
		fmt.Sprint(art.ID)).Scan(&payloadRaw))
	var payload entity.TranscribePayload
	require.NoError(t, json.Unmarshal(payloadRaw, &payload))
	assert.Equal(t, entity.TranscribePayload{
		ArticleID:  art.ID,
		MediaURL:   mediaURL,
		SourceKind: entity.SourceKindPodcast,
	}, payload)

	// The Pi consumer claims only its registered kinds (cmd/worker):
	// draining with those kinds must leave the transcribe job pending.
	jobs := pgRepo.NewJobRepo(conn)
	piWorkerKinds := []string{
		entity.JobKindRegenerateFeed, entity.JobKindNotifyEpisode,
		entity.JobKindNotifyError, entity.JobKindCleanupOldMedia,
	}
	if job, err := jobs.ClaimNext(context.Background(), piWorkerKinds...); assert.NoError(t, err) && job != nil {
		assert.NotEqual(t, entity.JobKindTranscribe, job.Kind,
			"Pi worker kinds must never claim transcribe jobs")
		// Put the unrelated backlog job back the way we found it.
		_, err = conn.Exec(`UPDATE jobs SET status='pending', attempts=attempts-1 WHERE id=$1`, job.ID)
		require.NoError(t, err)
	}

	var status string
	require.NoError(t, conn.QueryRow(
		`SELECT status FROM jobs WHERE kind = 'transcribe' AND payload->>'article_id' = $1`,
		fmt.Sprint(art.ID)).Scan(&status))
	assert.Equal(t, entity.JobStatusPending, status,
		"transcribe job stays pending for the Mac worker")

	// And the Mac worker's claim (kind filter 'transcribe') does pick it up.
	claimed, err := jobs.ClaimNext(context.Background(), entity.JobKindTranscribe)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, entity.JobKindTranscribe, claimed.Kind)
}
