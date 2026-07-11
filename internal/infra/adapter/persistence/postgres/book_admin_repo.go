package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// BookAdminRepo implements repository.BookAdminRepository (D-25 dashboard
// book management) on PostgreSQL. The book_ingest queries key jobs by the
// payload's file_path (the canonical-path identity, same semantics as the
// CLI ingest's books.file_path upsert).
type BookAdminRepo struct{ db *sql.DB }

func NewBookAdminRepo(db *sql.DB) repository.BookAdminRepository {
	return &BookAdminRepo{db: db}
}

// ListBooks returns every books row with its chunk count.
func (r *BookAdminRepo) ListBooks(ctx context.Context) ([]repository.BookRecord, error) {
	const query = `
SELECT b.id, b.title, b.file_path, b.imported_at,
       (SELECT count(*)::int FROM book_chunks c WHERE c.book_id = b.id) AS chunk_count
FROM books b
ORDER BY b.id ASC`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("ListBooks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []repository.BookRecord
	for rows.Next() {
		var b repository.BookRecord
		if err := rows.Scan(&b.ID, &b.Title, &b.FilePath, &b.ImportedAt, &b.ChunkCount); err != nil {
			return nil, fmt.Errorf("ListBooks: scan: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListBooks: rows: %w", err)
	}
	return out, nil
}

// LatestIngestStates returns the newest book_ingest job per file_path.
// DISTINCT ON with id DESC picks the latest enqueue — a re-upload's fresh
// pending job outranks the done/failed job of the previous ingest.
func (r *BookAdminRepo) LatestIngestStates(ctx context.Context) (map[string]repository.IngestJobState, error) {
	const query = `
SELECT DISTINCT ON (payload->>'file_path')
       payload->>'file_path', status, COALESCE(payload->>'title', '')
FROM jobs
WHERE kind = $1 AND payload->>'file_path' IS NOT NULL
ORDER BY payload->>'file_path', id DESC`
	rows, err := r.db.QueryContext(ctx, query, entity.JobKindBookIngest)
	if err != nil {
		return nil, fmt.Errorf("LatestIngestStates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]repository.IngestJobState)
	for rows.Next() {
		var (
			filePath string
			state    repository.IngestJobState
		)
		if err := rows.Scan(&filePath, &state.Status, &state.Title); err != nil {
			return nil, fmt.Errorf("LatestIngestStates: scan: %w", err)
		}
		out[filePath] = state
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("LatestIngestStates: rows: %w", err)
	}
	return out, nil
}

// HasPendingIngest reports whether a pending book_ingest job for the
// file_path exists.
func (r *BookAdminRepo) HasPendingIngest(ctx context.Context, filePath string) (bool, error) {
	const query = `
SELECT EXISTS (
    SELECT 1 FROM jobs
    WHERE kind = $1 AND status = $2 AND payload->>'file_path' = $3
)`
	var exists bool
	err := r.db.QueryRowContext(ctx, query, entity.JobKindBookIngest, entity.JobStatusPending, filePath).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("HasPendingIngest: %w", err)
	}
	return exists, nil
}

// CancelPendingIngest deletes pending book_ingest jobs for the file_path.
// Running jobs are left alone: the Mac worker owns them (their download of
// a just-deleted PDF fails and MarkFailed records why).
func (r *BookAdminRepo) CancelPendingIngest(ctx context.Context, filePath string) (int64, error) {
	const query = `
DELETE FROM jobs
WHERE kind = $1 AND status = $2 AND payload->>'file_path' = $3`
	res, err := r.db.ExecContext(ctx, query, entity.JobKindBookIngest, entity.JobStatusPending, filePath)
	if err != nil {
		return 0, fmt.Errorf("CancelPendingIngest: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("CancelPendingIngest: %w", err)
	}
	return n, nil
}

// DeleteBookByFilePath removes the books rows for the file_path together
// with everything referencing them, in one transaction. Neither book_chunks
// nor the learning tables cascade in the schema (plain REFERENCES), so the
// deletes are explicit, child-first: review_logs → learning_items →
// book_chunks → books.
func (r *BookAdminRepo) DeleteBookByFilePath(ctx context.Context, filePath string) (deleted bool, err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("DeleteBookByFilePath: begin: %w", err)
	}
	defer func() {
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
				err = fmt.Errorf("%w (rollback: %v)", err, rbErr)
			}
		}
	}()

	statements := []string{
		`DELETE FROM review_logs WHERE item_id IN (
    SELECT li.id FROM learning_items li
    JOIN books b ON b.id = li.book_id
    WHERE b.file_path = $1
)`,
		`DELETE FROM learning_items WHERE book_id IN (SELECT id FROM books WHERE file_path = $1)`,
		`DELETE FROM book_chunks WHERE book_id IN (SELECT id FROM books WHERE file_path = $1)`,
	}
	for _, stmt := range statements {
		if _, err = tx.ExecContext(ctx, stmt, filePath); err != nil {
			return false, fmt.Errorf("DeleteBookByFilePath: %w", err)
		}
	}

	res, err := tx.ExecContext(ctx, `DELETE FROM books WHERE file_path = $1`, filePath)
	if err != nil {
		return false, fmt.Errorf("DeleteBookByFilePath: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("DeleteBookByFilePath: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return false, fmt.Errorf("DeleteBookByFilePath: commit: %w", err)
	}
	return n > 0, nil
}
