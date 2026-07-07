package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/learning"
)

// learningFixture creates the FK targets a learning test needs (source →
// article, book, episode) and registers cleanup in reverse dependency
// order. Cleanup also sweeps the learning rows hanging off the fixture so
// a failed assertion never leaks stale pending logs into other tests.
type learningFixture struct {
	articleID int64
	bookID    int64
	episodeID int64
}

func newLearningFixture(t *testing.T, conn *sql.DB) learningFixture {
	t.Helper()
	nano := time.Now().UnixNano()

	var srcID int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO sources (name, feed_url, category, active)
		 VALUES ('learning-test', $1, 'dev', false) RETURNING id`,
		fmt.Sprintf("https://learning.example.com/%d.rss", nano)).Scan(&srcID))

	var f learningFixture
	require.NoError(t, conn.QueryRow(
		`INSERT INTO articles (source_id, url, title) VALUES ($1, $2, 'learning test article') RETURNING id`,
		srcID, fmt.Sprintf("https://learning.example.com/%d/a", nano)).Scan(&f.articleID))
	require.NoError(t, conn.QueryRow(
		`INSERT INTO books (title, file_path) VALUES ('learning test book', $1) RETURNING id`,
		fmt.Sprintf("/books/learning-%d.pdf", nano)).Scan(&f.bookID))
	require.NoError(t, conn.QueryRow(
		`INSERT INTO episodes (feed_kind, title, show_notes, audio_path, audio_bytes, duration_sec)
		 VALUES ('private', 'learning test ep', '', $1, 1, 1) RETURNING id`,
		fmt.Sprintf("/data/episodes/learning-%d.mp3", nano)).Scan(&f.episodeID))

	t.Cleanup(func() {
		_, _ = conn.Exec(`DELETE FROM review_logs WHERE item_id IN (
			SELECT id FROM learning_items WHERE article_id = $1 OR book_id = $2)`,
			f.articleID, f.bookID)
		_, _ = conn.Exec(`DELETE FROM learning_items WHERE article_id = $1 OR book_id = $2`,
			f.articleID, f.bookID)
		_, _ = conn.Exec(`DELETE FROM segments WHERE episode_id = $1`, f.episodeID)
		_, _ = conn.Exec(`DELETE FROM episodes WHERE id = $1`, f.episodeID)
		_, _ = conn.Exec(`DELETE FROM books WHERE id = $1`, f.bookID)
		_, _ = conn.Exec(`DELETE FROM articles WHERE id = $1`, f.articleID)
		_, _ = conn.Exec(`DELETE FROM sources WHERE id = $1`, srcID)
	})
	return f
}

// insertItem is a raw-SQL shortcut for arranging SRS states the repo API
// deliberately cannot create (arbitrary stage / due_on / retired_at).
func insertLearningItem(t *testing.T, conn *sql.DB, articleID int64, stage int, dueOn string, retired bool) int64 {
	t.Helper()
	var id int64
	require.NoError(t, conn.QueryRow(`
		INSERT INTO learning_items (kind, article_id, concept, question, answer, provider, stage, due_on, retired_at)
		VALUES ('article', $1, 'c', 'q', 'a', 'gemini', $2, $3::date,
		        CASE WHEN $4 THEN now() ELSE NULL END)
		RETURNING id`,
		articleID, stage, dueOn, retired).Scan(&id))
	return id
}

// TestLearningSchema_RealPostgres verifies the Phase 3 §4 migration against
// a real PostgreSQL: idempotency, the learning_items CHECKs, the
// review_logs idempotency key, the books upgrade path (§7.3 の2カラム) and
// the design-note precondition that segments.kind carries no CHECK.
func TestLearningSchema_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	require.NoError(t, MigrateUp(conn), "MigrateUp re-run (idempotency)")
	f := newLearningFixture(t, conn)

	// --- learning_items CHECKs (§4): kind ⇔ FK ---
	_, err := conn.Exec(`
		INSERT INTO learning_items (kind, book_id, concept, question, answer, provider, due_on)
		VALUES ('article', $1, 'c', 'q', 'a', 'gemini', '2026-07-08')`, f.bookID)
	require.Error(t, err, "kind=article without article_id must violate the CHECK")
	_, err = conn.Exec(`
		INSERT INTO learning_items (kind, article_id, book_id, concept, question, answer, provider, due_on)
		VALUES ('book', $1, $2, 'c', 'q', 'a', 'ollama', '2026-07-08')`, f.articleID, f.bookID)
	require.Error(t, err, "kind=book with article_id must violate the CHECK")

	// --- review_logs UNIQUE (item_id, asked_on): 同日 rev の冪等キー ---
	itemID := insertLearningItem(t, conn, f.articleID, 0, "2026-07-07", false)
	_, err = conn.Exec(
		`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, '2026-07-07')`,
		itemID, f.episodeID)
	require.NoError(t, err)
	_, err = conn.Exec(
		`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, '2026-07-07')`,
		itemID, f.episodeID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "review_logs_item_id_asked_on_key")

	// --- books.review_cursor / review_status: defaults and upgrade path ---
	var cursor int
	var status string
	require.NoError(t, conn.QueryRow(
		`SELECT review_cursor, review_status FROM books WHERE id = $1`, f.bookID).Scan(&cursor, &status))
	assert.Equal(t, 0, cursor)
	assert.Equal(t, "idle", status)

	// Phase 2 database (columns absent) → re-running MigrateUp restores
	// them and existing rows read back the defaults.
	_, err = conn.Exec(`ALTER TABLE books DROP COLUMN review_cursor, DROP COLUMN review_status`)
	require.NoError(t, err)
	require.NoError(t, MigrateUp(conn), "MigrateUp on a Phase 2 books schema")
	require.NoError(t, conn.QueryRow(
		`SELECT review_cursor, review_status FROM books WHERE id = $1`, f.bookID).Scan(&cursor, &status))
	assert.Equal(t, 0, cursor, "re-added column backfills DEFAULT 0")
	assert.Equal(t, "idle", status, "re-added column backfills DEFAULT 'idle'")

	// --- segments.kind precondition (§4 設計メモ): no CHECK, Phase 3
	// kinds insert as-is ---
	var segChecks int
	require.NoError(t, conn.QueryRow(`
		SELECT count(*) FROM pg_constraint c
		JOIN pg_class t ON c.conrelid = t.oid
		WHERE t.relname = 'segments' AND c.contype = 'c'`).Scan(&segChecks))
	assert.Zero(t, segChecks, "segments must carry no CHECK constraint")
	for i, kind := range []string{"quiz", "review", "book_review"} {
		_, err := conn.Exec(
			`INSERT INTO segments (episode_id, position, kind, script) VALUES ($1, $2, $3, 's')`,
			f.episodeID, i+1, kind)
		require.NoError(t, err, "segments.kind=%q must insert without ALTER", kind)
	}
}

// TestLearningRepo_AskFlow_RealPostgres proves the §6.3/§12-2 contract on a
// real database: due selection order, and — the load-bearing part — that a
// same-day rev re-run is fully idempotent because asking never mutates
// learning_items and review_logs collapses on UNIQUE (item_id, asked_on).
func TestLearningRepo_AskFlow_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	repo := pgRepo.NewLearningRepo(conn)
	ctx := context.Background()

	day := learning.BroadcastDay(time.Date(2026, 7, 7, 4, 30, 0, 0, time.UTC))
	dayStr := learning.FormatDay(day) // 2026-07-07

	overdue := insertLearningItem(t, conn, f.articleID, 1, "2026-07-05", false)
	dueToday := insertLearningItem(t, conn, f.articleID, 0, dayStr, false)
	future := insertLearningItem(t, conn, f.articleID, 0, "2026-07-09", false)
	retired := insertLearningItem(t, conn, f.articleID, 2, "2026-07-01", true)

	// --- selection: oldest first, retired/future excluded ---
	items, err := repo.ListDue(ctx, day, 4)
	require.NoError(t, err)
	gotIDs := make([]int64, len(items))
	for i, item := range items {
		gotIDs[i] = item.ID
	}
	assert.Equal(t, []int64{overdue, dueToday}, gotIDs,
		"due_on ASC, id ASC; future (%d) and retired (%d) excluded", future, retired)
	assert.True(t, items[1].DueOn.Equal(day), "date round-trips as midnight UTC")

	// --- limit = 出題枠 S ---
	limited, err := repo.ListDue(ctx, day, 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)
	assert.Equal(t, overdue, limited[0].ID, "oldest-first under the cap (§6.2 スライド)")

	// --- rev1: record asking ---
	require.NoError(t, repo.RecordAsked(ctx, gotIDs, f.episodeID, day))

	// 出題は learning_items を一切更新しない(§12-2)。
	var stage int
	var due string
	require.NoError(t, conn.QueryRow(
		`SELECT stage, due_on::text FROM learning_items WHERE id = $1`, overdue).Scan(&stage, &due))
	assert.Equal(t, 1, stage)
	assert.Equal(t, "2026-07-05", due)

	// --- rev2(同日再実行): 選定結果は同一、ログは増えない ---
	again, err := repo.ListDue(ctx, day, 4)
	require.NoError(t, err)
	againIDs := make([]int64, len(again))
	for i, item := range again {
		againIDs[i] = item.ID
	}
	assert.Equal(t, gotIDs, againIDs, "same-day re-run selects the same items")

	var rev2Episode int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO episodes (feed_kind, title, show_notes, audio_path, audio_bytes, duration_sec)
		 VALUES ('private', 'rev2', '', '/data/episodes/rev2-learning.mp3', 1, 1) RETURNING id`).Scan(&rev2Episode))
	t.Cleanup(func() { _, _ = conn.Exec(`DELETE FROM episodes WHERE id = $1`, rev2Episode) })
	require.NoError(t, repo.RecordAsked(ctx, againIDs, rev2Episode, day))

	var logs int
	var loggedEpisode int64
	require.NoError(t, conn.QueryRow(
		`SELECT count(*) FROM review_logs WHERE item_id = ANY($1::bigint[]) AND asked_on = $2::date`,
		fmt.Sprintf("{%d,%d}", overdue, dueToday), dayStr).Scan(&logs))
	assert.Equal(t, 2, logs, "UNIQUE (item_id, asked_on) keeps one log per item per day")
	require.NoError(t, conn.QueryRow(
		`SELECT episode_id FROM review_logs WHERE item_id = $1 AND asked_on = $2::date`,
		overdue, dayStr).Scan(&loggedEpisode))
	assert.Equal(t, f.episodeID, loggedEpisode, "the first rev's episode_id is kept (DO NOTHING)")
}

// TestLearningRepo_InsertItem_RealPostgres exercises the item INSERT path
// including the §12-4 application-layer provider pin for books.
func TestLearningRepo_InsertItem_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	repo := pgRepo.NewLearningRepo(conn)
	ctx := context.Background()

	now := time.Date(2026, 7, 7, 4, 30, 0, 0, time.FixedZone("JST", 9*3600))
	dueOn := learning.FirstDueDay(now)

	articleItem := learning.NewItem{
		Kind: learning.KindArticle, ArticleID: &f.articleID,
		Concept: "c", Question: "q", Answer: "a", Provider: "gemini",
	}
	id, err := repo.InsertItem(ctx, articleItem, dueOn)
	require.NoError(t, err)

	var stage int
	var due string
	var retiredAt *time.Time
	require.NoError(t, conn.QueryRow(
		`SELECT stage, due_on::text, retired_at FROM learning_items WHERE id = $1`, id).
		Scan(&stage, &due, &retiredAt))
	assert.Equal(t, 0, stage, "new items start at stage 0")
	assert.Equal(t, "2026-07-08", due, "due_on = 翌日 (JST, §5.1)")
	assert.Nil(t, retiredAt)

	bookItem := learning.NewItem{
		Kind: learning.KindBook, BookID: &f.bookID,
		Concept: "c", Question: "q", Answer: "a", Provider: learning.ProviderOllama,
	}
	_, err = repo.InsertItem(ctx, bookItem, dueOn)
	require.NoError(t, err)

	// §12-4: book × クラウド provider はアプリ層で拒否(DB には届かない)。
	bookItem.Provider = "gemini"
	_, err = repo.InsertItem(ctx, bookItem, dueOn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ollama")
}

// TestLearningRepo_AutoResolve_RealPostgres proves the D-17 auto-advance on
// a real database: cutoff filtering, the single-advance dedupe, graduation,
// graded_at staying NULL, and CountOverdueActive before/after the drain.
func TestLearningRepo_AutoResolve_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	repo := pgRepo.NewLearningRepo(conn)
	ctx := context.Background()

	ladder := []int{1, 7, 30}
	now := time.Date(2026, 7, 7, 4, 30, 0, 0, time.FixedZone("JST", 9*3600))
	resolveDay := learning.BroadcastDay(now)                     // 2026-07-07
	cutoffDay := learning.BroadcastDay(now.Add(-48 * time.Hour)) // 2026-07-05

	// Drain any pending stale logs another test (or run) left behind so the
	// resolved-count assertions below are exact, and take the overdue
	// baseline for delta assertions — the shared test DB may hold foreign
	// learning items.
	_, err := repo.AutoResolve(ctx, cutoffDay, resolveDay, ladder)
	require.NoError(t, err)
	baseline, err := repo.CountOverdueActive(ctx, resolveDay)
	require.NoError(t, err)

	addLog := func(itemID int64, askedOn string) {
		_, err := conn.Exec(
			`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, $3::date)`,
			itemID, f.episodeID, askedOn)
		require.NoError(t, err)
	}

	// stale: asked 07-04 and (re-asked, still ungraded) 07-05 — two pending
	// logs, ONE advance expected.
	stale := insertLearningItem(t, conn, f.articleID, 0, "2026-07-04", false)
	addLog(stale, "2026-07-04")
	addLog(stale, "2026-07-05")
	// fresh: asked 07-06, inside the 48h window — untouched.
	fresh := insertLearningItem(t, conn, f.articleID, 0, "2026-07-06", false)
	addLog(fresh, "2026-07-06")
	// graduating: final rung with a stale log — auto completes the ladder.
	graduating := insertLearningItem(t, conn, f.articleID, 2, "2026-07-04", false)
	addLog(graduating, "2026-07-04")
	// archived: manually retired while its log sat ungraded — log closes,
	// state stays terminal.
	archived := insertLearningItem(t, conn, f.articleID, 1, "2026-07-04", true)
	addLog(archived, "2026-07-04")

	// Backpressure input before the drain: stale (07-04), graduating
	// (07-04) and fresh (07-06) are strictly overdue on 07-07; archived is
	// retired and does not count.
	overdue, err := repo.CountOverdueActive(ctx, resolveDay)
	require.NoError(t, err)
	assert.Equal(t, baseline+3, overdue)

	resolved, err := repo.AutoResolve(ctx, cutoffDay, resolveDay, ladder)
	require.NoError(t, err)
	assert.Equal(t, 4, resolved, "stale×2 + graduating + archived logs closed; fresh spared")

	type itemState struct {
		stage   int
		due     string
		retired bool
	}
	getItem := func(id int64) itemState {
		var s itemState
		require.NoError(t, conn.QueryRow(
			`SELECT stage, due_on::text, retired_at IS NOT NULL FROM learning_items WHERE id = $1`, id).
			Scan(&s.stage, &s.due, &s.retired))
		return s
	}
	type logState struct {
		result   *string
		gradedAt *time.Time
	}
	getLog := func(itemID int64, askedOn string) logState {
		var s logState
		require.NoError(t, conn.QueryRow(
			`SELECT result, graded_at FROM review_logs WHERE item_id = $1 AND asked_on = $2::date`,
			itemID, askedOn).Scan(&s.result, &s.gradedAt))
		return s
	}

	// stale: both logs auto, graded_at NULL (§4), item advanced exactly
	// once: stage 0 -> 1, due = 07-07 + 7d.
	for _, askedOn := range []string{"2026-07-04", "2026-07-05"} {
		log := getLog(stale, askedOn)
		require.NotNil(t, log.result)
		assert.Equal(t, learning.ResultAuto, *log.result)
		assert.Nil(t, log.gradedAt, "auto resolution keeps graded_at NULL (§4)")
	}
	assert.Equal(t, itemState{stage: 1, due: "2026-07-14", retired: false}, getItem(stale),
		"two stale logs of one item collapse into a single advance")

	// fresh: untouched on both sides.
	log := getLog(fresh, "2026-07-06")
	assert.Nil(t, log.result, "a log inside the 48h window stays ungraded")
	assert.Equal(t, itemState{stage: 0, due: "2026-07-06", retired: false}, getItem(fresh))

	// graduating: 卒業 (採点ゼロでもラダー完走、D-17).
	assert.True(t, getItem(graduating).retired)

	// archived: log closed, terminal state untouched.
	log = getLog(archived, "2026-07-04")
	require.NotNil(t, log.result)
	assert.Equal(t, learning.ResultAuto, *log.result)
	assert.Equal(t, 1, getItem(archived).stage, "retired item state is never advanced")

	// Backpressure input after the drain: only fresh remains overdue.
	overdue, err = repo.CountOverdueActive(ctx, resolveDay)
	require.NoError(t, err)
	assert.Equal(t, baseline+1, overdue, "auto-resolve drains the backlog (§9: キューは勝手にドレイン)")

	// Re-run: nothing left to claim (冪等).
	resolved, err = repo.AutoResolve(ctx, cutoffDay, resolveDay, ladder)
	require.NoError(t, err)
	assert.Zero(t, resolved)
}

// TestLearningRepo_AutoVsManualGrade_RealPostgres pins §12-9: the 48h
// auto-resolve (radio) and a manual grade (server API, 後続タスク — its
// UPDATE shape is exercised raw here) contend for the same pending log,
// and exactly one side may win. The claim is an atomic UPDATE whose WHERE
// carries the not-yet-set checks; SELECT-then-UPDATE would lose this.
func TestLearningRepo_AutoVsManualGrade_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	repo := pgRepo.NewLearningRepo(conn)
	ctx := context.Background()

	ladder := []int{1, 7, 30}
	now := time.Date(2026, 7, 7, 4, 30, 0, 0, time.FixedZone("JST", 9*3600))
	resolveDay := learning.BroadcastDay(now)
	cutoffDay := learning.BroadcastDay(now.Add(-48 * time.Hour))

	// Drain foreign pending stale logs so the exact resolved counts below
	// hold even against a reused test database.
	_, err := repo.AutoResolve(ctx, cutoffDay, resolveDay, ladder)
	require.NoError(t, err)

	// --- sequential both orders: the loser must see zero rows ---
	// manual first, then auto: auto must not claim or advance.
	itemA := insertLearningItem(t, conn, f.articleID, 0, "2026-07-04", false)
	var logA int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, '2026-07-04') RETURNING id`,
		itemA, f.episodeID).Scan(&logA))
	res, err := conn.Exec(
		`UPDATE review_logs SET result = 'good', graded_at = now()
		 WHERE id = $1 AND result IS NULL AND graded_at IS NULL`, logA)
	require.NoError(t, err)
	n, _ := res.RowsAffected()
	require.EqualValues(t, 1, n)

	resolved, err := repo.AutoResolve(ctx, cutoffDay, resolveDay, ladder)
	require.NoError(t, err)
	assert.Zero(t, resolved, "a graded log is invisible to auto-resolve")
	var stage int
	require.NoError(t, conn.QueryRow(
		`SELECT stage FROM learning_items WHERE id = $1`, itemA).Scan(&stage))
	assert.Equal(t, 0, stage, "auto-resolve must not advance a manually graded item")

	// auto first, then manual: the grade UPDATE must match nothing.
	itemB := insertLearningItem(t, conn, f.articleID, 0, "2026-07-04", false)
	var logB int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, '2026-07-04') RETURNING id`,
		itemB, f.episodeID).Scan(&logB))
	resolved, err = repo.AutoResolve(ctx, cutoffDay, resolveDay, ladder)
	require.NoError(t, err)
	assert.Equal(t, 1, resolved)
	res, err = conn.Exec(
		`UPDATE review_logs SET result = 'good', graded_at = now()
		 WHERE id = $1 AND result IS NULL AND graded_at IS NULL`, logB)
	require.NoError(t, err)
	n, _ = res.RowsAffected()
	assert.Zero(t, n, "manual grade after auto resolution matches nothing (409 の素材)")

	// --- truly concurrent: one winner, whoever it is ---
	itemC := insertLearningItem(t, conn, f.articleID, 0, "2026-07-04", false)
	var logC int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, '2026-07-04') RETURNING id`,
		itemC, f.episodeID).Scan(&logC))

	var wg sync.WaitGroup
	var autoErr, manualErr error
	var manualWon int64
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, autoErr = repo.AutoResolve(ctx, cutoffDay, resolveDay, ladder)
	}()
	go func() {
		defer wg.Done()
		res, err := conn.Exec(
			`UPDATE review_logs SET result = 'good', graded_at = now()
			 WHERE id = $1 AND result IS NULL AND graded_at IS NULL`, logC)
		if err != nil {
			manualErr = err
			return
		}
		manualWon, _ = res.RowsAffected()
	}()
	wg.Wait()
	require.NoError(t, autoErr)
	require.NoError(t, manualErr)

	var result string
	var gradedAt *time.Time
	require.NoError(t, conn.QueryRow(
		`SELECT result, graded_at FROM review_logs WHERE id = $1`, logC).Scan(&result, &gradedAt))
	require.NoError(t, conn.QueryRow(
		`SELECT stage FROM learning_items WHERE id = $1`, itemC).Scan(&stage))

	switch result {
	case "good":
		assert.EqualValues(t, 1, manualWon)
		assert.NotNil(t, gradedAt)
		assert.Equal(t, 0, stage, "manual won: auto must not have advanced the item (二重適用なし)")
	case learning.ResultAuto:
		assert.Zero(t, manualWon, "auto won: the manual UPDATE must have matched nothing")
		assert.Nil(t, gradedAt)
		assert.Equal(t, 1, stage, "auto won: exactly one advance")
	default:
		t.Fatalf("log resolved to unexpected result %q", result)
	}
}
