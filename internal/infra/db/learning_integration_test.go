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
	"catchup-feed/internal/repository"
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
// real database: due selection order; the load-bearing idempotency — a
// same-day rev re-run selects the same items (same-day pending logs do not
// exclude) and review_logs collapses on UNIQUE (item_id, asked_on); and the
// §6.3 exclusion — items with a pending log from a previous day disappear
// from selection until auto-resolve closes the log.
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

	// --- §6.3 親裁定: 未採点ログが残る項目は翌朝の選定に載らない ---
	// (同日 rev では上の再選定のとおり除外されない — 除外条件は
	// asked_on < day のみ。)
	nextDay := day.AddDate(0, 0, 1) // 2026-07-08
	tomorrow, err := repo.ListDue(ctx, nextDay, 50)
	require.NoError(t, err)
	for _, item := range tomorrow {
		assert.NotContains(t, gotIDs, item.ID,
			"an item with a pending (ungraded) log from a previous day must not be re-selected")
	}

	// --- 自動解決でログが閉じた後は、遷移後の期日で再び選定対象 ---
	// 7/10 朝の自動解決(cutoff 7/08 ≧ asked_on 7/07)で両ログが閉じ、
	// overdue: stage1→2 due 8/09、dueToday: stage0→1 due 7/17 に前進する。
	resolveDay := day.AddDate(0, 0, 3) // 2026-07-10
	_, err = repo.AutoResolve(ctx, day.AddDate(0, 0, 1), resolveDay, []int{1, 7, 30})
	require.NoError(t, err)

	reappeared, err := repo.ListDue(ctx, learning.BroadcastDay(time.Date(2026, 8, 9, 4, 30, 0, 0, time.UTC)), 50)
	require.NoError(t, err)
	byID := make(map[int64]learning.Item, len(reappeared))
	for _, item := range reappeared {
		byID[item.ID] = item
	}
	got, ok := byID[overdue]
	require.True(t, ok, "once the pending log is auto-resolved the item is selectable again")
	assert.Equal(t, 2, got.Stage)
	assert.Equal(t, "2026-08-09", learning.FormatDay(got.DueOn))
	_, ok = byID[dueToday]
	assert.True(t, ok, "dueToday reappears at its post-transition due date (7/17 <= 8/09)")
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

// TestLearningRepo_HasArticleItemCreatedOn_RealPostgres pins the JST day
// boundary (§12-10) of the same-day rev-re-run dedupe (§12-2): created_at
// is a UTC timestamptz, so an item created 23:30 JST belongs to that JST
// day (14:30 UTC, same date) while one created 00:10 JST belongs to the
// NEXT JST day even though its UTC date is still the previous one — a
// naive UTC date comparison would misfile it. Far-past fixed days keep the
// test independent of items other tests create with created_at = now().
func TestLearningRepo_HasArticleItemCreatedOn_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	repo := pgRepo.NewLearningRepo(conn)
	ctx := context.Background()

	day5 := time.Date(2031, 3, 5, 0, 0, 0, 0, time.UTC)
	day6 := day5.AddDate(0, 0, 1)

	for _, d := range []time.Time{day5, day6} {
		exists, err := repo.HasArticleItemCreatedOn(ctx, d)
		require.NoError(t, err)
		assert.False(t, exists, "no items yet on %s", learning.FormatDay(d))
	}

	itemID := insertLearningItem(t, conn, f.articleID, 0, "2031-03-06", false)
	setCreatedAt := func(ts string) {
		_, err := conn.Exec(`UPDATE learning_items SET created_at = $2::timestamptz WHERE id = $1`, itemID, ts)
		require.NoError(t, err)
	}

	// 23:30 JST = 14:30 UTC same date → belongs to 3/05.
	setCreatedAt("2031-03-05T23:30:00+09:00")
	exists, err := repo.HasArticleItemCreatedOn(ctx, day5)
	require.NoError(t, err)
	assert.True(t, exists)
	exists, err = repo.HasArticleItemCreatedOn(ctx, day6)
	require.NoError(t, err)
	assert.False(t, exists)

	// 00:10 JST next day = 15:10 UTC on the PREVIOUS date → belongs to 3/06.
	setCreatedAt("2031-03-06T00:10:00+09:00")
	exists, err = repo.HasArticleItemCreatedOn(ctx, day5)
	require.NoError(t, err)
	assert.False(t, exists, "UTC 日付比較なら 3/05 に化ける境界 (§12-10)")
	exists, err = repo.HasArticleItemCreatedOn(ctx, day6)
	require.NoError(t, err)
	assert.True(t, exists)

	// kind='book' items never count: the dedupe guards ARTICLE generation
	// only (book items are generated by their own §5.3 path, 手順6).
	var bookItem int64
	require.NoError(t, conn.QueryRow(`
		INSERT INTO learning_items (kind, book_id, concept, question, answer, provider, stage, due_on, created_at)
		VALUES ('book', $1, 'c', 'q', 'a', 'ollama', 0, '2031-03-06', '2031-03-05T12:00:00+09:00'::timestamptz)
		RETURNING id`, f.bookID).Scan(&bookItem))
	exists, err = repo.HasArticleItemCreatedOn(ctx, day5)
	require.NoError(t, err)
	assert.False(t, exists, "book items must not trip the article dedupe")
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

// TestLearningRepo_AutoResolve_CrossRunSingleAdvance_RealPostgres pins the
// B-1 fix (§6.3): a never-graded item that was asked on two consecutive
// days owns two pending logs whose 48h windows expire on DIFFERENT
// mornings. Each morning's run claims one log, but only the first may
// advance the item — without the due guard the ladder [1,7,30] would
// degrade to [1,1,30] for ungraded items (stage2 on the second morning).
func TestLearningRepo_AutoResolve_CrossRunSingleAdvance_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	repo := pgRepo.NewLearningRepo(conn)
	ctx := context.Background()
	ladder := []int{1, 7, 30}

	// stage0 item asked 7/05 and (re-asked, pre-B-2 data shape) 7/06,
	// never graded.
	item := insertLearningItem(t, conn, f.articleID, 0, "2026-07-05", false)
	for _, askedOn := range []string{"2026-07-05", "2026-07-06"} {
		_, err := conn.Exec(
			`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, $3::date)`,
			item, f.episodeID, askedOn)
		require.NoError(t, err)
	}

	itemState := func() (stage int, due string) {
		require.NoError(t, conn.QueryRow(
			`SELECT stage, due_on::text FROM learning_items WHERE id = $1`, item).Scan(&stage, &due))
		return stage, due
	}
	logResult := func(askedOn string) *string {
		var result *string
		require.NoError(t, conn.QueryRow(
			`SELECT result FROM review_logs WHERE item_id = $1 AND asked_on = $2::date`,
			item, askedOn).Scan(&result))
		return result
	}

	// Run A: 7/07 morning (cutoff 7/05) — claims the 7/05 log only,
	// advances stage0→1, due 7/07+7 = 7/14.
	_, err := repo.AutoResolve(ctx,
		learning.BroadcastDay(time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)),
		learning.BroadcastDay(time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)), ladder)
	require.NoError(t, err)
	stage, due := itemState()
	assert.Equal(t, 1, stage)
	assert.Equal(t, "2026-07-14", due)
	require.NotNil(t, logResult("2026-07-05"))
	assert.Equal(t, learning.ResultAuto, *logResult("2026-07-05"))
	assert.Nil(t, logResult("2026-07-06"), "the younger log is still inside its 48h window")

	// Run B: 7/08 morning (cutoff 7/06) — claims the 7/06 log, but the
	// item's due_on (7/14) is already past the resolve day: the log closes
	// and the item must NOT advance again.
	_, err = repo.AutoResolve(ctx,
		learning.BroadcastDay(time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC)),
		learning.BroadcastDay(time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC)), ladder)
	require.NoError(t, err)
	require.NotNil(t, logResult("2026-07-06"))
	assert.Equal(t, learning.ResultAuto, *logResult("2026-07-06"), "the second log is still closed")
	stage, due = itemState()
	assert.Equal(t, 1, stage, "cross-run: the second auto must not double-advance (7日段の想起を消さない)")
	assert.Equal(t, "2026-07-14", due)
}

// TestLearningRepo_AutoVsManualGrade_RealPostgres pins §12-9: the 48h
// auto-resolve (radio) and a manual grade (the real §8.1 grade
// transaction, LearningAdminRepo.GradeReview) contend for the same pending
// log, and exactly one side may win. Both claims are atomic UPDATEs whose
// WHERE carries the not-yet-set checks; SELECT-then-UPDATE would lose
// this.
func TestLearningRepo_AutoVsManualGrade_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	repo := pgRepo.NewLearningRepo(conn)
	admin := pgRepo.NewLearningAdminRepo(conn)
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
	// manual first, then auto: auto must not claim the log or advance the
	// item a second time.
	itemA := insertLearningItem(t, conn, f.articleID, 0, "2026-07-04", false)
	var logA int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, '2026-07-04') RETURNING id`,
		itemA, f.episodeID).Scan(&logA))
	out, err := admin.GradeReview(ctx, logA, learning.ResultGood, resolveDay, ladder)
	require.NoError(t, err)
	assert.Equal(t, 1, out.Stage, "the manual grade advances the item (§6.1)")

	resolved, err := repo.AutoResolve(ctx, cutoffDay, resolveDay, ladder)
	require.NoError(t, err)
	assert.Zero(t, resolved, "a graded log is invisible to auto-resolve")
	var stage int
	require.NoError(t, conn.QueryRow(
		`SELECT stage FROM learning_items WHERE id = $1`, itemA).Scan(&stage))
	assert.Equal(t, 1, stage, "auto-resolve must not advance a manually graded item again")

	// auto first, then manual: the grade claim must match nothing → 409.
	itemB := insertLearningItem(t, conn, f.articleID, 0, "2026-07-04", false)
	var logB int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, '2026-07-04') RETURNING id`,
		itemB, f.episodeID).Scan(&logB))
	resolved, err = repo.AutoResolve(ctx, cutoffDay, resolveDay, ladder)
	require.NoError(t, err)
	assert.Equal(t, 1, resolved)
	_, err = admin.GradeReview(ctx, logB, learning.ResultGood, resolveDay, ladder)
	assert.ErrorIs(t, err, repository.ErrReviewLogGraded,
		"manual grade after auto resolution is the 409 (§8.1 一発確定: auto も採点済み)")
	require.NoError(t, conn.QueryRow(
		`SELECT stage FROM learning_items WHERE id = $1`, itemB).Scan(&stage))
	assert.Equal(t, 1, stage, "the rejected grade must not advance the item a second time")

	// --- truly concurrent: one winner, whoever it is ---
	itemC := insertLearningItem(t, conn, f.articleID, 0, "2026-07-04", false)
	var logC int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, '2026-07-04') RETURNING id`,
		itemC, f.episodeID).Scan(&logC))

	var wg sync.WaitGroup
	var autoErr, manualErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, autoErr = repo.AutoResolve(ctx, cutoffDay, resolveDay, ladder)
	}()
	go func() {
		defer wg.Done()
		_, manualErr = admin.GradeReview(ctx, logC, learning.ResultGood, resolveDay, ladder)
	}()
	wg.Wait()
	require.NoError(t, autoErr)

	var result string
	var gradedAt *time.Time
	require.NoError(t, conn.QueryRow(
		`SELECT result, graded_at FROM review_logs WHERE id = $1`, logC).Scan(&result, &gradedAt))
	require.NoError(t, conn.QueryRow(
		`SELECT stage FROM learning_items WHERE id = $1`, itemC).Scan(&stage))

	// Either way the item advanced exactly once (stage 0 → 1): good and
	// auto share the same transition (D-17), so a double application would
	// show as stage 2.
	assert.Equal(t, 1, stage, "exactly one advance regardless of the winner (二重適用なし)")
	switch result {
	case "good":
		require.NoError(t, manualErr, "manual won: the grade must have succeeded")
		assert.NotNil(t, gradedAt, "manual grades record graded_at")
	case learning.ResultAuto:
		assert.ErrorIs(t, manualErr, repository.ErrReviewLogGraded,
			"auto won: the concurrent manual grade falls into the same 409")
		assert.Nil(t, gradedAt, "auto resolutions keep graded_at NULL (§4)")
	default:
		t.Fatalf("log resolved to unexpected result %q", result)
	}
}

// TestLearningRepo_WeeklyReviewMaterial_RealPostgres proves the §7.4 material
// query against a real database: the created-in-window concept list (any
// kind), the ladder-completion graduation count (stage >= ladderLen, manual
// archives excluded), the JST-day window boundaries, and the forgot-in-window
// reintroduction pick.
func TestLearningRepo_WeeklyReviewMaterial_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	repo := pgRepo.NewLearningRepo(conn)
	ctx := context.Background()

	now := time.Now().UTC()
	fromDay := learning.WeeklyReviewWindowStart(now)
	const ladderLen = 3
	today := learning.FormatDay(learning.BroadcastDay(now))

	exec := func(q string, args ...any) {
		_, err := conn.Exec(q, args...)
		require.NoError(t, err)
	}

	// --- concepts: created in-window, any kind, oldest first ---
	exec(`INSERT INTO learning_items (kind, article_id, concept, question, answer, provider, stage, due_on, created_at)
	      VALUES ('article',$1,'CONCEPT-A','q','a','gemini',0,$2::date, now())`, f.articleID, today)
	exec(`INSERT INTO learning_items (kind, article_id, concept, question, answer, provider, stage, due_on, created_at)
	      VALUES ('article',$1,'CONCEPT-B','q','a','groq',0,$2::date, now())`, f.articleID, today)
	// book item is kind-agnostic in the concept list (private-only, §10).
	exec(`INSERT INTO learning_items (kind, book_id, concept, question, answer, provider, stage, due_on, created_at)
	      VALUES ('book',$1,'BOOK-CONCEPT','q','a','ollama',0,$2::date, now())`, f.bookID, today)
	// created before the window → excluded from concepts.
	exec(`INSERT INTO learning_items (kind, article_id, concept, question, answer, provider, stage, due_on, created_at)
	      VALUES ('article',$1,'OLD-CONCEPT','q','a','gemini',0,$2::date, now() - interval '10 days')`, f.articleID, today)

	// --- graduation: retired in-window at stage == ladderLen → counted ---
	exec(`INSERT INTO learning_items (kind, article_id, concept, question, answer, provider, stage, due_on, created_at, retired_at)
	      VALUES ('article',$1,'GRADUATED','q','a','gemini',3,$2::date, now() - interval '20 days', now())`, f.articleID, today)
	// manual archive in-window at stage < ladderLen → NOT counted.
	exec(`INSERT INTO learning_items (kind, article_id, concept, question, answer, provider, stage, due_on, created_at, retired_at)
	      VALUES ('article',$1,'MANUAL','q','a','gemini',1,$2::date, now() - interval '20 days', now())`, f.articleID, today)
	// graduation retired before the window → NOT counted.
	exec(`INSERT INTO learning_items (kind, article_id, concept, question, answer, provider, stage, due_on, created_at, retired_at)
	      VALUES ('article',$1,'OLD-GRAD','q','a','gemini',3,$2::date, now() - interval '40 days', now() - interval '10 days')`, f.articleID, today)

	// --- reintroduction: a forgot grade in-window (item created out of window
	// so it does not also appear in the concept list) ---
	var forgotItem int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO learning_items (kind, article_id, concept, question, answer, provider, stage, due_on, created_at)
		 VALUES ('article',$1,'FORGOTTEN','q','a','gemini',0,$2::date, now() - interval '20 days') RETURNING id`,
		f.articleID, today).Scan(&forgotItem))
	exec(`INSERT INTO review_logs (item_id, episode_id, asked_on, result, graded_at)
	      VALUES ($1,$2,$3::date,'forgot', now())`, forgotItem, f.episodeID, today)

	m, err := repo.WeeklyReviewMaterial(ctx, fromDay, ladderLen)
	require.NoError(t, err)

	assert.Equal(t, []string{"CONCEPT-A", "CONCEPT-B", "BOOK-CONCEPT"}, m.Concepts,
		"created-in-window concepts, any kind, oldest first")
	assert.NotContains(t, m.Concepts, "OLD-CONCEPT", "created before the window is excluded")
	assert.NotContains(t, m.Concepts, "FORGOTTEN", "reintroduced item was created out of window")
	assert.Equal(t, 1, m.GraduatedCount,
		"only ladder completion counts (GRADUATED); MANUAL (stage<len) and OLD-GRAD (out of window) excluded")
	assert.Equal(t, "FORGOTTEN", m.Reintroduced, "the in-window forgot item's concept")

	// --- empty window (future start) → nothing to say ---
	future := learning.BroadcastDay(now).AddDate(0, 0, 1)
	empty, err := repo.WeeklyReviewMaterial(ctx, future, ladderLen)
	require.NoError(t, err)
	assert.True(t, empty.IsEmpty(), "a window with no rows yields empty material")
}
