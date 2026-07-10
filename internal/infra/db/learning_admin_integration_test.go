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

// insertTestBook creates a book with the given review state and n chunks,
// registering cleanup (chunks first — the FK has no cascade).
func insertTestBook(t *testing.T, conn *sql.DB, title, status string, cursor, chunks int) int64 {
	t.Helper()
	var id int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO books (title, file_path, review_status, review_cursor)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		title, fmt.Sprintf("/books/admin-%s-%d.pdf", title, time.Now().UnixNano()),
		status, cursor).Scan(&id))
	for i := range chunks {
		_, err := conn.Exec(
			`INSERT INTO book_chunks (book_id, position, content) VALUES ($1, $2, 'chunk')`, id, i)
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(`DELETE FROM book_chunks WHERE book_id = $1`, id)
		_, _ = conn.Exec(`DELETE FROM books WHERE id = $1`, id)
	})
	return id
}

// TestLearningAdminRepo_PendingAndGrade_RealPostgres proves the §8.1
// grading loop on a real database: the pending queue (oldest asking
// first), the 一発確定 grade transaction (§6.1 遷移+§12-9 atomic claim),
// the 404/409 material, and the terminal-item stance (log closes, retired
// state untouched).
func TestLearningAdminRepo_PendingAndGrade_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	admin := pgRepo.NewLearningAdminRepo(conn)
	ctx := context.Background()
	ladder := []int{1, 7, 30}
	gradedOn := learning.BroadcastDay(time.Date(2026, 7, 7, 8, 0, 0, 0, time.FixedZone("JST", 9*3600)))

	addLog := func(itemID int64, askedOn string) int64 {
		var id int64
		require.NoError(t, conn.QueryRow(
			`INSERT INTO review_logs (item_id, episode_id, asked_on) VALUES ($1, $2, $3::date) RETURNING id`,
			itemID, f.episodeID, askedOn).Scan(&id))
		return id
	}

	// Three ungraded askings across two days + one already graded (must
	// not appear in pending).
	itemOld := insertLearningItem(t, conn, f.articleID, 0, "2026-07-06", false)
	itemNew := insertLearningItem(t, conn, f.articleID, 1, "2026-07-07", false)
	itemFinal := insertLearningItem(t, conn, f.articleID, 2, "2026-07-07", false)
	logOld := addLog(itemOld, "2026-07-06")
	logNew := addLog(itemNew, "2026-07-07")
	logFinal := addLog(itemFinal, "2026-07-07")
	itemGraded := insertLearningItem(t, conn, f.articleID, 0, "2026-07-06", false)
	logGraded := addLog(itemGraded, "2026-07-06")
	_, err := conn.Exec(`UPDATE review_logs SET result = 'good', graded_at = now() WHERE id = $1`, logGraded)
	require.NoError(t, err)

	// --- pending: 古い出題から、採点済みは載らない ---
	pending, err := admin.ListPendingReviews(ctx)
	require.NoError(t, err)
	var mine []repository.PendingReview
	for _, p := range pending {
		if p.LogID == logOld || p.LogID == logNew || p.LogID == logFinal || p.LogID == logGraded {
			mine = append(mine, p)
		}
	}
	require.Len(t, mine, 3, "the graded log must not be pending")
	assert.Equal(t, logOld, mine[0].LogID, "oldest asking first (asked_on ASC)")
	assert.Equal(t, "2026-07-06", learning.FormatDay(mine[0].AskedOn))
	assert.Equal(t, "c", mine[0].Concept)
	assert.Equal(t, "q", mine[0].Question)
	assert.Equal(t, "a", mine[0].Answer)

	// --- grade good: stage 0→1, due = 採点日 + ladder[1] ---
	out, err := admin.GradeReview(ctx, logOld, learning.ResultGood, gradedOn, ladder)
	require.NoError(t, err)
	assert.Equal(t, repository.GradeOutcome{
		ItemID: itemOld, Stage: 1, DueOn: gradedOn.AddDate(0, 0, 7), Retired: false,
	}, out)
	var result string
	var gradedAt *time.Time
	require.NoError(t, conn.QueryRow(
		`SELECT result, graded_at FROM review_logs WHERE id = $1`, logOld).Scan(&result, &gradedAt))
	assert.Equal(t, "good", result)
	assert.NotNil(t, gradedAt, "manual grades record graded_at (auto keeps it NULL, §4)")

	// --- 一発確定: 再採点は 409 素材、状態は動かない ---
	_, err = admin.GradeReview(ctx, logOld, learning.ResultForgot, gradedOn, ladder)
	assert.ErrorIs(t, err, repository.ErrReviewLogGraded)
	var stage int
	var due string
	require.NoError(t, conn.QueryRow(
		`SELECT stage, due_on::text FROM learning_items WHERE id = $1`, itemOld).Scan(&stage, &due))
	assert.Equal(t, 1, stage, "a rejected re-grade must not move the item")
	assert.Equal(t, "2026-07-14", due)

	// --- grade forgot: stage リセット、due = 採点日 + 1(文字どおり翌日) ---
	out, err = admin.GradeReview(ctx, logNew, learning.ResultForgot, gradedOn, ladder)
	require.NoError(t, err)
	assert.Equal(t, repository.GradeOutcome{
		ItemID: itemNew, Stage: 0, DueOn: gradedOn.AddDate(0, 0, 1), Retired: false,
	}, out)

	// --- grade good on the final rung: 卒業 ---
	out, err = admin.GradeReview(ctx, logFinal, learning.ResultGood, gradedOn, ladder)
	require.NoError(t, err)
	assert.True(t, out.Retired, "completing the ladder retires the item (卒業)")
	var retiredAt *time.Time
	require.NoError(t, conn.QueryRow(
		`SELECT retired_at FROM learning_items WHERE id = $1`, itemFinal).Scan(&retiredAt))
	assert.NotNil(t, retiredAt)

	// --- 存在しない log は 404 素材 ---
	_, err = admin.GradeReview(ctx, int64(1<<60), learning.ResultGood, gradedOn, ladder)
	assert.ErrorIs(t, err, repository.ErrReviewLogNotFound)

	// --- retired item: 残った未採点ログの採点はログを閉じ、状態は不変 ---
	itemRetired := insertLearningItem(t, conn, f.articleID, 1, "2026-07-06", true)
	logRetired := addLog(itemRetired, "2026-07-06")
	out, err = admin.GradeReview(ctx, logRetired, learning.ResultForgot, gradedOn, ladder)
	require.NoError(t, err)
	assert.Equal(t, itemRetired, out.ItemID)
	assert.True(t, out.Retired)
	assert.Equal(t, 1, out.Stage, "terminal items keep their state (AutoResolve と同じ流儀)")
	require.NoError(t, conn.QueryRow(
		`SELECT result FROM review_logs WHERE id = $1`, logRetired).Scan(&result))
	assert.Equal(t, "forgot", result, "the log still records the verdict")
}

// TestLearningAdminRepo_Items_RealPostgres pins the tracker query: the
// active/retired split, orders, and the minimal history summary.
func TestLearningAdminRepo_Items_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	admin := pgRepo.NewLearningAdminRepo(conn)
	ctx := context.Background()

	itemA := insertLearningItem(t, conn, f.articleID, 1, "2026-07-05", false) // asked twice
	itemB := insertLearningItem(t, conn, f.articleID, 0, "2026-07-09", false) // never asked
	itemR := insertLearningItem(t, conn, f.articleID, 3, "2026-07-01", true)  // retired

	for _, askedOn := range []string{"2026-07-04", "2026-07-06"} {
		_, err := conn.Exec(
			`INSERT INTO review_logs (item_id, episode_id, asked_on, result, graded_at)
			 VALUES ($1, $2, $3::date, 'good', now())`, itemA, f.episodeID, askedOn)
		require.NoError(t, err)
	}

	active, err := admin.ListItems(ctx, false)
	require.NoError(t, err)
	byID := map[int64]repository.LearningItemSummary{}
	var order []int64
	for _, s := range active {
		byID[s.ID] = s
		if s.ID == itemA || s.ID == itemB {
			order = append(order, s.ID)
		}
	}
	_, hasRetired := byID[itemR]
	assert.False(t, hasRetired, "retired items are not in the active list")
	assert.Equal(t, []int64{itemA, itemB}, order, "active list is due_on ASC")

	a := byID[itemA]
	assert.Equal(t, 2, a.TimesAsked)
	require.NotNil(t, a.LastResult)
	assert.Equal(t, "good", *a.LastResult)
	require.NotNil(t, a.LastAskedOn)
	assert.Equal(t, "2026-07-06", learning.FormatDay(*a.LastAskedOn))

	b := byID[itemB]
	assert.Equal(t, 0, b.TimesAsked)
	assert.Nil(t, b.LastResult)
	assert.Nil(t, b.LastAskedOn)

	retired, err := admin.ListItems(ctx, true)
	require.NoError(t, err)
	found := false
	for _, s := range retired {
		assert.NotNil(t, s.RetiredAt, "retired list carries only archived items")
		if s.ID == itemR {
			found = true
		}
	}
	assert.True(t, found)
}

// TestLearningAdminRepo_Retire_RealPostgres pins §8.1: retired_at セット
// のみ、冪等(再実行は元の retired_at を返す)、absent id は not-found.
func TestLearningAdminRepo_Retire_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newLearningFixture(t, conn)
	admin := pgRepo.NewLearningAdminRepo(conn)
	ctx := context.Background()

	item := insertLearningItem(t, conn, f.articleID, 1, "2026-07-05", false)

	first, err := admin.RetireItem(ctx, item)
	require.NoError(t, err)
	assert.False(t, first.IsZero())

	var stage int
	var due string
	require.NoError(t, conn.QueryRow(
		`SELECT stage, due_on::text FROM learning_items WHERE id = $1`, item).Scan(&stage, &due))
	assert.Equal(t, 1, stage, "retire sets retired_at only")
	assert.Equal(t, "2026-07-05", due)

	again, err := admin.RetireItem(ctx, item)
	require.NoError(t, err)
	assert.True(t, first.Equal(again), "re-retiring returns the original retired_at (冪等)")

	_, err = admin.RetireItem(ctx, int64(1<<60))
	assert.ErrorIs(t, err, repository.ErrLearningItemNotFound)
}

// TestLearningAdminRepo_Books_RealPostgres proves the D-20 book
// management: the list with chunk totals, the activate swap (existing
// active demoted in the same operation), finished 再読, and deactivate's
// pause semantics (cursor kept, idempotent, finished untouched).
func TestLearningAdminRepo_Books_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	admin := pgRepo.NewLearningAdminRepo(conn)
	ctx := context.Background()

	bookA := insertTestBook(t, conn, "A", "active", 5, 3)
	bookB := insertTestBook(t, conn, "B", "idle", 3, 2) // paused mid-read: cursor 3
	bookF := insertTestBook(t, conn, "F", "finished", 4, 4)

	// --- list: §7.3 state + total chunks(進捗率の素材) ---
	books, err := admin.ListBooks(ctx)
	require.NoError(t, err)
	byID := map[int64]repository.ReviewBook{}
	for _, b := range books {
		byID[b.ID] = b
	}
	assert.Equal(t, repository.ReviewBook{ID: bookA, Title: "A", ReviewStatus: "active", ReviewCursor: 5, TotalChunks: 3}, byID[bookA])
	assert.Equal(t, repository.ReviewBook{ID: bookB, Title: "B", ReviewStatus: "idle", ReviewCursor: 3, TotalChunks: 2}, byID[bookB])
	assert.Equal(t, repository.ReviewBook{ID: bookF, Title: "F", ReviewStatus: "finished", ReviewCursor: 4, TotalChunks: 4}, byID[bookF])

	status := func(id int64) string {
		var s string
		require.NoError(t, conn.QueryRow(`SELECT review_status FROM books WHERE id = $1`, id).Scan(&s))
		return s
	}

	// --- activate B(idle→active): A(既存 active)は同一操作で idle に
	// 落ち、B は一時停止からの再開なのでカーソルを保持する ---
	got, err := admin.ActivateBook(ctx, bookB)
	require.NoError(t, err)
	assert.Equal(t, "active", got.ReviewStatus)
	assert.Equal(t, 3, got.ReviewCursor, "idle→activate keeps the cursor (一時停止からの再開, D-20)")
	assert.Equal(t, "idle", status(bookA), "the previous active book is demoted (入れ替えを1操作で)")

	// --- activate finished(finished→active): 再読開始なのでカーソルを
	// 0 にリセットする(親裁定 2026-07-07)。末尾カーソルのまま active に
	// すると radio が即 finished に戻す ---
	got, err = admin.ActivateBook(ctx, bookF)
	require.NoError(t, err)
	assert.Equal(t, "active", got.ReviewStatus)
	assert.Equal(t, 0, got.ReviewCursor, "finished→activate resets the cursor to 0 (再読開始)")
	assert.Equal(t, "idle", status(bookB))

	// --- deactivate: active→idle、カーソル保持、冪等 ---
	// bookF は直前の finished→activate で cursor 0 にリセット済み。
	got, err = admin.DeactivateBook(ctx, bookF)
	require.NoError(t, err)
	assert.Equal(t, "idle", got.ReviewStatus)
	assert.Equal(t, 0, got.ReviewCursor, "pause keeps the cursor (D-20)")
	got, err = admin.DeactivateBook(ctx, bookF)
	require.NoError(t, err)
	assert.Equal(t, "idle", got.ReviewStatus, "deactivate is idempotent")

	// --- deactivate a finished book: 読了マーカーは消えない ---
	_, err = conn.Exec(`UPDATE books SET review_status = 'finished' WHERE id = $1`, bookF)
	require.NoError(t, err)
	got, err = admin.DeactivateBook(ctx, bookF)
	require.NoError(t, err)
	assert.Equal(t, "finished", got.ReviewStatus, "finished is already inactive; deactivate must not erase it")

	// --- 存在しない id ---
	_, err = admin.ActivateBook(ctx, int64(1<<60))
	assert.ErrorIs(t, err, repository.ErrBookNotFound)
	_, err = admin.DeactivateBook(ctx, int64(1<<60))
	assert.ErrorIs(t, err, repository.ErrBookNotFound)
}

// TestLearningAdminRepo_ConcurrentActivate_RealPostgres pins the §7.3
// invariant under contention: two truly concurrent activates of DIFFERENT
// books must never leave two active rows. The invariant lives in the
// application layer (no DB constraint by design), carried by the advisory
// lock in ActivateBook — plain row locking cannot see a row that turned
// active after the demote statement took its snapshot.
func TestLearningAdminRepo_ConcurrentActivate_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	admin := pgRepo.NewLearningAdminRepo(conn)
	ctx := context.Background()

	bookX := insertTestBook(t, conn, "X", "idle", 0, 0)
	bookY := insertTestBook(t, conn, "Y", "idle", 0, 0)

	// Several rounds: the race window is small, one shot could pass by
	// luck. Assumes tests in this package run sequentially and clean up
	// their books, so no foreign 'active' rows exist.
	for round := range 10 {
		var wg sync.WaitGroup
		var errX, errY error
		wg.Add(2)
		go func() { defer wg.Done(); _, errX = admin.ActivateBook(ctx, bookX) }()
		go func() { defer wg.Done(); _, errY = admin.ActivateBook(ctx, bookY) }()
		wg.Wait()
		require.NoError(t, errX)
		require.NoError(t, errY)

		var active int
		require.NoError(t, conn.QueryRow(
			`SELECT count(*) FROM books WHERE review_status = 'active'`).Scan(&active))
		require.Equal(t, 1, active,
			"round %d: concurrent activates must leave exactly one active book (§7.3 active 最大1冊)", round)

		// Reset for the next round.
		_, err := conn.Exec(`UPDATE books SET review_status = 'idle' WHERE id IN ($1, $2)`, bookX, bookY)
		require.NoError(t, err)
	}
}
