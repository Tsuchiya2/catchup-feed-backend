package db

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/learning"
)

// bookReviewFixture creates a book with `chunks` chunks (positions 0..chunks-1)
// and a private episode, cleaning both up in reverse order.
type bookReviewFixture struct {
	bookID    int64
	episodeID int64
}

func newBookReviewFixture(t *testing.T, conn *sql.DB, chunks int) bookReviewFixture {
	t.Helper()
	nano := time.Now().UnixNano()

	var f bookReviewFixture
	require.NoError(t, conn.QueryRow(
		`INSERT INTO books (title, file_path) VALUES ('book_review test', $1) RETURNING id`,
		fmt.Sprintf("/books/br-%d.pdf", nano)).Scan(&f.bookID))
	for pos := 0; pos < chunks; pos++ {
		_, err := conn.Exec(
			`INSERT INTO book_chunks (book_id, position, content) VALUES ($1, $2, $3)`,
			f.bookID, pos, fmt.Sprintf("chunk-%d", pos))
		require.NoError(t, err)
	}
	require.NoError(t, conn.QueryRow(
		`INSERT INTO episodes (feed_kind, title, show_notes, audio_path, audio_bytes, duration_sec)
		 VALUES ('private', 'br test ep', '', $1, 1, 1) RETURNING id`,
		fmt.Sprintf("/data/episodes/br-%d.mp3", nano)).Scan(&f.episodeID))

	t.Cleanup(func() {
		_, _ = conn.Exec(`DELETE FROM segments WHERE episode_id = $1`, f.episodeID)
		_, _ = conn.Exec(`DELETE FROM episodes WHERE id = $1`, f.episodeID)
		_, _ = conn.Exec(`DELETE FROM book_chunks WHERE book_id = $1`, f.bookID)
		_, _ = conn.Exec(`DELETE FROM books WHERE id = $1`, f.bookID)
	})
	return f
}

// TestBookReviewRepo_ActiveAndChunks_RealPostgres covers ActiveBook (only the
// review_status='active' book, with its total chunk count) and NextChunks
// (from the cursor, ordered, limited) on a real database.
func TestBookReviewRepo_ActiveAndChunks_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newBookReviewFixture(t, conn, 5)
	repo := pgRepo.NewBookReviewRepo(conn)
	ctx := context.Background()

	// idle by default → no active book.
	_, ok, err := repo.ActiveBook(ctx)
	require.NoError(t, err)
	assert.False(t, ok, "a fresh (idle) book is not active")

	// Activate with a cursor at position 2.
	_, err = conn.Exec(`UPDATE books SET review_status = 'active', review_cursor = 2 WHERE id = $1`, f.bookID)
	require.NoError(t, err)

	book, ok, err := repo.ActiveBook(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, f.bookID, book.ID)
	assert.Equal(t, 2, book.Cursor)
	assert.Equal(t, 5, book.TotalChunks, "total_chunks counts every chunk of the book")

	// NextChunks from the cursor, limited to 2 → positions 2,3.
	chunks, err := repo.NextChunks(ctx, book.ID, book.Cursor, 2)
	require.NoError(t, err)
	require.Len(t, chunks, 2)
	assert.Equal(t, 2, chunks[0].Position)
	assert.Equal(t, "chunk-2", chunks[0].Content)
	assert.Equal(t, 3, chunks[1].Position)

	// From cursor 5 (end) → empty.
	tail, err := repo.NextChunks(ctx, book.ID, 5, 3)
	require.NoError(t, err)
	assert.Empty(t, tail, "no chunks at or past the end")
}

// TestBookReviewRepo_HasBookReviewOn_RealPostgres pins the §12-2 dedupe and its
// JST day boundary (§12-10): a book_review segment in a PRIVATE episode counts
// only for the JST broadcast day of its published_at, and only book_review
// (not quiz) segments of PRIVATE (not public) episodes count.
func TestBookReviewRepo_HasBookReviewOn_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newBookReviewFixture(t, conn, 3)
	repo := pgRepo.NewBookReviewRepo(conn)
	ctx := context.Background()

	day := time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)
	prevDay := day.AddDate(0, 0, -1)

	setPublishedAt := func(ts string) {
		_, err := conn.Exec(`UPDATE episodes SET published_at = $2::timestamptz WHERE id = $1`, f.episodeID, ts)
		require.NoError(t, err)
	}
	// Episode published 04:30 JST on 7/7 (= 19:30 UTC 7/6).
	setPublishedAt("2026-07-07T04:30:00+09:00")

	// No segment yet.
	has, err := repo.HasBookReviewOn(ctx, day)
	require.NoError(t, err)
	assert.False(t, has)

	// A quiz segment does not count.
	_, err = conn.Exec(
		`INSERT INTO segments (episode_id, position, kind, script) VALUES ($1, 1, 'quiz', 's')`, f.episodeID)
	require.NoError(t, err)
	has, err = repo.HasBookReviewOn(ctx, day)
	require.NoError(t, err)
	assert.False(t, has, "only book_review segments count")

	// The book_review segment counts for 7/7, not 7/6 (JST boundary).
	_, err = conn.Exec(
		`INSERT INTO segments (episode_id, position, kind, script) VALUES ($1, 2, 'book_review', 's')`, f.episodeID)
	require.NoError(t, err)
	has, err = repo.HasBookReviewOn(ctx, day)
	require.NoError(t, err)
	assert.True(t, has, "book_review on 7/7 JST")
	has, err = repo.HasBookReviewOn(ctx, prevDay)
	require.NoError(t, err)
	assert.False(t, has, "not the previous JST day")

	// 00:10 JST on 7/7 (UTC date 7/6) still belongs to 7/7 — a naive UTC date
	// comparison would misfile it as 7/6 (§12-10).
	setPublishedAt("2026-07-07T00:10:00+09:00")
	has, err = repo.HasBookReviewOn(ctx, day)
	require.NoError(t, err)
	assert.True(t, has, "00:10 JST belongs to the 7/7 broadcast day")
	has, err = repo.HasBookReviewOn(ctx, prevDay)
	require.NoError(t, err)
	assert.False(t, has, "UTC 日付比較なら 7/06 に化ける境界 (§12-10)")

	// A public episode's book_review must not count (feed_kind filter).
	setPublishedAt("2026-07-07T04:30:00+09:00")
	_, err = conn.Exec(`UPDATE episodes SET feed_kind = 'public' WHERE id = $1`, f.episodeID)
	require.NoError(t, err)
	has, err = repo.HasBookReviewOn(ctx, day)
	require.NoError(t, err)
	assert.False(t, has, "book_review lives on private episodes only (§10)")
}

// TestBookReviewRepo_AdvanceCursor_RealPostgres pins the guarded, idempotent
// cursor advance (§7.3): it moves only when the current cursor and active
// status match, sets finished on demand, and is a no-op on a stale cursor or a
// non-active book (same-day rev / mid-run deactivate safety).
func TestBookReviewRepo_AdvanceCursor_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newBookReviewFixture(t, conn, 10)
	repo := pgRepo.NewBookReviewRepo(conn)
	ctx := context.Background()

	read := func() (int, string) {
		var cursor int
		var status string
		require.NoError(t, conn.QueryRow(
			`SELECT review_cursor, review_status FROM books WHERE id = $1`, f.bookID).Scan(&cursor, &status))
		return cursor, status
	}

	_, err := conn.Exec(`UPDATE books SET review_status = 'active', review_cursor = 0 WHERE id = $1`, f.bookID)
	require.NoError(t, err)

	// Advance 0 -> 3, not finished.
	require.NoError(t, repo.AdvanceCursor(ctx, f.bookID, 0, 3, false))
	cursor, status := read()
	assert.Equal(t, 3, cursor)
	assert.Equal(t, "active", status)

	// Stale fromCursor (0, but it is now 3) → no-op (same-day rev safety).
	require.NoError(t, repo.AdvanceCursor(ctx, f.bookID, 0, 6, false))
	cursor, _ = read()
	assert.Equal(t, 3, cursor, "guarded WHERE makes a stale advance a no-op (§7.3 冪等)")

	// Correct fromCursor 3 -> 6.
	require.NoError(t, repo.AdvanceCursor(ctx, f.bookID, 3, 6, false))
	cursor, _ = read()
	assert.Equal(t, 6, cursor)

	// Deactivated mid-run → advance is a no-op (does not resurrect progress).
	_, err = conn.Exec(`UPDATE books SET review_status = 'idle' WHERE id = $1`, f.bookID)
	require.NoError(t, err)
	require.NoError(t, repo.AdvanceCursor(ctx, f.bookID, 6, 9, false))
	cursor, status = read()
	assert.Equal(t, 6, cursor, "a paused book does not advance")
	assert.Equal(t, "idle", status)

	// Re-activate and finish (10 -> finished).
	_, err = conn.Exec(`UPDATE books SET review_status = 'active', review_cursor = 7 WHERE id = $1`, f.bookID)
	require.NoError(t, err)
	require.NoError(t, repo.AdvanceCursor(ctx, f.bookID, 7, 10, true))
	cursor, status = read()
	assert.Equal(t, 10, cursor)
	assert.Equal(t, "finished", status, "reaching the end sets finished (§7.3)")
}

// TestBookReviewRepo_ActiveBook_FormatDayBinding is a tiny guard that the day
// binding used by HasBookReviewOn matches learning.FormatDay's ::date contract
// (defense against a future refactor swapping the bind form, §12-10).
func TestBookReviewRepo_ActiveBook_FormatDayBinding(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))
	f := newBookReviewFixture(t, conn, 1)
	repo := pgRepo.NewBookReviewRepo(conn)
	ctx := context.Background()

	_, err := conn.Exec(`UPDATE episodes SET published_at = '2026-07-07T04:30:00+09:00'::timestamptz WHERE id = $1`, f.episodeID)
	require.NoError(t, err)
	_, err = conn.Exec(`INSERT INTO segments (episode_id, position, kind, script) VALUES ($1, 1, 'book_review', 's')`, f.episodeID)
	require.NoError(t, err)

	has, err := repo.HasBookReviewOn(ctx, learning.BroadcastDay(time.Date(2026, 7, 7, 4, 30, 0, 0, time.UTC)))
	require.NoError(t, err)
	assert.True(t, has)
}
