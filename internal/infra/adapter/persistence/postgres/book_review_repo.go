package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/learning"
	"catchup-feed/internal/repository"
)

// BookReviewRepo implements the §7.3 book_review persistence
// (repository.BookReviewRepository) on PostgreSQL. Day parameters are bound
// as YYYY-MM-DD text with a ::date cast (learning.FormatDay), same as the
// other learning repos (§12-10).
type BookReviewRepo struct{ db *sql.DB }

func NewBookReviewRepo(db *sql.DB) repository.BookReviewRepository {
	return &BookReviewRepo{db: db}
}

// ActiveBook returns the single active review book (§7.3). LIMIT 1 with a
// deterministic id order guards against a stray second active row (the
// "active は最大1冊" invariant lives in the admin API, not a DB constraint).
func (r *BookReviewRepo) ActiveBook(ctx context.Context) (repository.ActiveReviewBook, bool, error) {
	const query = `
SELECT b.id, b.title, b.review_cursor,
       (SELECT count(*)::int FROM book_chunks c WHERE c.book_id = b.id) AS total_chunks
FROM books b
WHERE b.review_status = 'active'
ORDER BY b.id ASC
LIMIT 1`
	var b repository.ActiveReviewBook
	err := r.db.QueryRowContext(ctx, query).Scan(&b.ID, &b.Title, &b.Cursor, &b.TotalChunks)
	if errors.Is(err, sql.ErrNoRows) {
		return repository.ActiveReviewBook{}, false, nil
	}
	if err != nil {
		return repository.ActiveReviewBook{}, false, fmt.Errorf("ActiveBook: %w", err)
	}
	return b, true, nil
}

// NextChunks returns the unreviewed chunks from position >= cursor (§7.3).
func (r *BookReviewRepo) NextChunks(ctx context.Context, bookID int64, cursor, limit int) ([]repository.BookReviewChunk, error) {
	const query = `
SELECT position, content FROM book_chunks
WHERE book_id = $1 AND position >= $2
ORDER BY position ASC
LIMIT $3`
	rows, err := r.db.QueryContext(ctx, query, bookID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("NextChunks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var chunks []repository.BookReviewChunk
	for rows.Next() {
		var c repository.BookReviewChunk
		if err := rows.Scan(&c.Position, &c.Content); err != nil {
			return nil, fmt.Errorf("NextChunks: %w", err)
		}
		chunks = append(chunks, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("NextChunks: %w", err)
	}
	return chunks, nil
}

// HasBookReviewOn is the §12-2 same-day rev guard for §7.3: a book_review
// segment committed in a private episode published on the JST day. The
// published_at (UTC timestamptz on a UTC-運用 Postgres) is compared against
// the JST day's two boundary instants, exactly as HasArticleItemCreatedOn —
// a naive UTC date comparison would misfile episodes generated between 00:00
// and 09:00 JST (radio runs 04:30 JST, §3.3, so this is the normal case).
func (r *BookReviewRepo) HasBookReviewOn(ctx context.Context, day time.Time) (bool, error) {
	const query = `
SELECT EXISTS (
    SELECT 1 FROM segments s
    JOIN episodes e ON e.id = s.episode_id
    WHERE s.kind = $1
      AND e.feed_kind = $2
      AND e.published_at >= ($3::date)::timestamp AT TIME ZONE 'Asia/Tokyo'
      AND e.published_at <  ($3::date + 1)::timestamp AT TIME ZONE 'Asia/Tokyo'
)`
	var exists bool
	err := r.db.QueryRowContext(ctx, query,
		entity.SegmentKindBookReview, entity.FeedKindPrivate, learning.FormatDay(day)).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("HasBookReviewOn: %w", err)
	}
	return exists, nil
}

// AdvanceCursor moves the cursor (and optionally finishes the book), guarded
// on the current cursor and active status so it is idempotent and race-safe
// (§7.3). Zero rows matched is the expected no-op — a same-day rev whose
// cursor already advanced, or a book deactivated/swapped mid-run — not an
// error.
func (r *BookReviewRepo) AdvanceCursor(ctx context.Context, bookID int64, fromCursor, newCursor int, finished bool) error {
	const query = `
UPDATE books
SET review_cursor = $3,
    review_status = CASE WHEN $4 THEN 'finished' ELSE review_status END
WHERE id = $1 AND review_cursor = $2 AND review_status = 'active'`
	if _, err := r.db.ExecContext(ctx, query, bookID, fromCursor, newCursor, finished); err != nil {
		return fmt.Errorf("AdvanceCursor: %w", err)
	}
	return nil
}
