package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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

// vectorLiteral renders a 1024-dim unit vector along axis as the pgvector
// input syntax '[v1,v2,...]'.
func vectorLiteral(axis int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := 0; i < 1024; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		if i == axis {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	b.WriteByte(']')
	return b.String()
}

// TestBooksVector_RealPostgres verifies the Phase 2 §6 book-RAG migration
// (U-24) against a real pgvector-enabled PostgreSQL:
//   - books / book_chunks accept the exact write shape pulse-books (Python)
//     uses, including vector(1024) embeddings
//   - `<=>` (cosine distance) nearest-neighbour search works
//   - the vector(1024) typmod rejects other dimensionalities
//   - UNIQUE (book_id, position) holds
//   - re-running MigrateUp with data present is a non-destructive no-op
//     (existing-DB safety for the live Pi)
func TestBooksVector_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))

	// Snapshot an unrelated Phase 1 table to prove non-interference.
	var sourcesBefore int
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM sources`).Scan(&sourcesBefore))

	var bookID int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO books (title, file_path) VALUES ('Go言語による並行処理', '/books/concurrency-in-go.pdf') RETURNING id`,
	).Scan(&bookID))
	defer func() {
		_, _ = conn.Exec(`DELETE FROM book_chunks WHERE book_id = $1`, bookID)
		_, _ = conn.Exec(`DELETE FROM books WHERE id = $1`, bookID)
	}()

	// imported_at defaults to now().
	var importedAtSet bool
	require.NoError(t, conn.QueryRow(
		`SELECT imported_at IS NOT NULL FROM books WHERE id = $1`, bookID).Scan(&importedAtSet))
	assert.True(t, importedAtSet)

	// Chunks along different axes: axis 0 and axis 1 are orthogonal.
	for pos, axis := range []int{0, 1} {
		_, err := conn.Exec(
			`INSERT INTO book_chunks (book_id, position, content, embedding) VALUES ($1, $2, $3, $4::vector)`,
			bookID, pos, fmt.Sprintf("chunk %d", pos), vectorLiteral(axis))
		require.NoError(t, err)
	}

	// <=> cosine-distance search: a query vector on axis 0 must rank the
	// axis-0 chunk first (distance 0) ahead of the orthogonal one.
	var nearestPos int
	var nearestDist float64
	require.NoError(t, conn.QueryRow(
		`SELECT position, embedding <=> $2::vector
		 FROM book_chunks WHERE book_id = $1
		 ORDER BY embedding <=> $2::vector LIMIT 1`,
		bookID, vectorLiteral(0)).Scan(&nearestPos, &nearestDist))
	assert.Equal(t, 0, nearestPos)
	assert.InDelta(t, 0.0, nearestDist, 1e-9)

	// vector(1024) rejects a 3-dim vector (D-12: bge-m3 の次元を型で固定).
	_, err := conn.Exec(
		`INSERT INTO book_chunks (book_id, position, content, embedding) VALUES ($1, 99, 'bad dim', '[1,0,0]'::vector)`,
		bookID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1024")

	// UNIQUE (book_id, position): re-inserting position 0 must fail.
	_, err = conn.Exec(
		`INSERT INTO book_chunks (book_id, position, content, embedding) VALUES ($1, 0, 'dup', $2::vector)`,
		bookID, vectorLiteral(2))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "book_chunks_book_id_position_key")

	// Idempotent re-run with live data: nothing is dropped or duplicated.
	require.NoError(t, MigrateUp(conn), "MigrateUp re-run over populated books tables")
	var chunks int
	require.NoError(t, conn.QueryRow(
		`SELECT count(*) FROM book_chunks WHERE book_id = $1`, bookID).Scan(&chunks))
	assert.Equal(t, 2, chunks, "re-run keeps existing chunks intact")

	// Unrelated Phase 1 data untouched (seed is ON CONFLICT DO NOTHING).
	var sourcesAfter int
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM sources`).Scan(&sourcesAfter))
	assert.Equal(t, sourcesBefore, sourcesAfter, "books migration must not touch sources")
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
	// draining that backlog to exhaustion — however many pi-kind jobs the
	// database happens to hold — must never reach the transcribe job.
	jobs := pgRepo.NewJobRepo(conn)
	piWorkerKinds := []string{
		entity.JobKindRegenerateFeed, entity.JobKindNotifyEpisode,
		entity.JobKindNotifyError, entity.JobKindCleanupOldMedia,
	}
	var drained []int64
	for {
		job, err := jobs.ClaimNext(context.Background(), piWorkerKinds...)
		require.NoError(t, err)
		if job == nil {
			break // pi-kind backlog fully drained; transcribe was never claimed
		}
		require.NotEqual(t, entity.JobKindTranscribe, job.Kind,
			"Pi worker kinds must never claim transcribe jobs")
		drained = append(drained, job.ID)
	}
	// Put the unrelated backlog jobs back the way we found them.
	for _, id := range drained {
		_, err := conn.Exec(`UPDATE jobs SET status='pending', attempts=attempts-1 WHERE id=$1`, id)
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

// TestRequeueRunning_KindScoped_RealPostgres proves the multi-consumer
// stale-sweep contract (repository.JobRepository) against a real
// PostgreSQL: the Pi worker's startup sweep, restricted to its registered
// kinds, must not flip a 'running' transcribe job — that row belongs to the
// Mac Python worker and is mid-execution, not stale. The reverse scope
// (sweeping only 'transcribe') is verified too.
func TestRequeueRunning_KindScoped_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))

	insertRunning := func(kind string) int64 {
		var id int64
		require.NoError(t, conn.QueryRow(
			`INSERT INTO jobs (kind, status, attempts) VALUES ($1, 'running', 1) RETURNING id`,
			kind).Scan(&id))
		t.Cleanup(func() { _, _ = conn.Exec(`DELETE FROM jobs WHERE id = $1`, id) })
		return id
	}
	transcribeID := insertRunning(entity.JobKindTranscribe)
	piID := insertRunning(entity.JobKindRegenerateFeed)

	jobStatus := func(id int64) (status string, attempts int) {
		require.NoError(t, conn.QueryRow(
			`SELECT status, attempts FROM jobs WHERE id = $1`, id).Scan(&status, &attempts))
		return status, attempts
	}

	// Pi worker restart: sweeps only its own kinds.
	jobs := pgRepo.NewJobRepo(conn)
	piWorkerKinds := []string{
		entity.JobKindRegenerateFeed, entity.JobKindNotifyEpisode,
		entity.JobKindNotifyError, entity.JobKindCleanupOldMedia,
	}
	n, err := jobs.RequeueRunning(context.Background(), piWorkerKinds...)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, int64(1))

	status, attempts := jobStatus(piID)
	assert.Equal(t, entity.JobStatusPending, status, "own-kind orphan is requeued")
	assert.Equal(t, 1, attempts, "attempts from the crashed claim stay counted")

	status, attempts = jobStatus(transcribeID)
	assert.Equal(t, entity.JobStatusRunning, status,
		"the Mac worker's running transcribe job must survive the Pi sweep")
	assert.Equal(t, 1, attempts,
		"the Mac worker's job must not gain attempts from the Pi sweep")

	// The Mac worker's own sweep (kind 'transcribe') does requeue it.
	n, err = jobs.RequeueRunning(context.Background(), entity.JobKindTranscribe)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, n, int64(1))
	status, _ = jobStatus(transcribeID)
	assert.Equal(t, entity.JobStatusPending, status)
}
