package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

const viewerColumns = "id, name, email, password_hash, created_at, updated_at, deactivated_at"

// pgUniqueViolation is the PostgreSQL error code for unique_violation.
const pgUniqueViolation = "23505"

// ViewerRepo persists read-only dashboard accounts (viewers table, D-27).
type ViewerRepo struct{ db *sql.DB }

func NewViewerRepo(db *sql.DB) repository.ViewerRepository {
	return &ViewerRepo{db: db}
}

func scanViewer(s scanner) (*entity.Viewer, error) {
	var viewer entity.Viewer
	if err := s.Scan(
		&viewer.ID, &viewer.Name, &viewer.Email, &viewer.PasswordHash,
		&viewer.CreatedAt, &viewer.UpdatedAt, &viewer.DeactivatedAt,
	); err != nil {
		return nil, err
	}
	return &viewer, nil
}

// mapViewerErr converts a unique_violation on viewers.email into the
// repository sentinel so the use case can answer 409 instead of 500.
func mapViewerErr(op string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return fmt.Errorf("%s: %w", op, repository.ErrDuplicateViewerEmail)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// Create inserts the viewer and sets viewer.ID / CreatedAt / UpdatedAt.
func (repo *ViewerRepo) Create(ctx context.Context, viewer *entity.Viewer) error {
	const query = `
INSERT INTO viewers (name, email, password_hash)
VALUES ($1, $2, $3)
RETURNING id, created_at, updated_at`
	err := repo.db.QueryRowContext(ctx, query,
		viewer.Name, viewer.Email, viewer.PasswordHash,
	).Scan(&viewer.ID, &viewer.CreatedAt, &viewer.UpdatedAt)
	if err != nil {
		return mapViewerErr("Create", err)
	}
	return nil
}

// Get returns the viewer, or nil when not found.
func (repo *ViewerRepo) Get(ctx context.Context, id int64) (*entity.Viewer, error) {
	query := `
SELECT ` + viewerColumns + `
FROM viewers
WHERE id = $1
LIMIT 1`
	viewer, err := scanViewer(repo.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get: %w", err)
	}
	return viewer, nil
}

// List returns all viewers (active and deactivated), oldest first.
func (repo *ViewerRepo) List(ctx context.Context) ([]*entity.Viewer, error) {
	query := `
SELECT ` + viewerColumns + `
FROM viewers
ORDER BY id ASC`
	rows, err := repo.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	viewers := make([]*entity.Viewer, 0, 10)
	for rows.Next() {
		viewer, err := scanViewer(rows)
		if err != nil {
			return nil, fmt.Errorf("List: %w", err)
		}
		viewers = append(viewers, viewer)
	}
	return viewers, rows.Err()
}

// Update rewrites name / email / password_hash and bumps updated_at.
func (repo *ViewerRepo) Update(ctx context.Context, viewer *entity.Viewer) error {
	const query = `
UPDATE viewers SET
       name          = $1,
       email         = $2,
       password_hash = $3,
       updated_at    = now()
WHERE id = $4`
	res, err := repo.db.ExecContext(ctx, query,
		viewer.Name, viewer.Email, viewer.PasswordHash, viewer.ID,
	)
	if err != nil {
		return mapViewerErr("Update", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("Update: no rows affected")
	}
	return nil
}

// Deactivate marks the viewer inactive as of t (idempotent).
func (repo *ViewerRepo) Deactivate(ctx context.Context, id int64, t time.Time) error {
	const query = `
UPDATE viewers SET deactivated_at = $1, updated_at = now()
WHERE id = $2 AND deactivated_at IS NULL`
	if _, err := repo.db.ExecContext(ctx, query, t, id); err != nil {
		return fmt.Errorf("Deactivate: %w", err)
	}
	return nil
}

// Reactivate clears deactivated_at (idempotent).
func (repo *ViewerRepo) Reactivate(ctx context.Context, id int64) error {
	const query = `
UPDATE viewers SET deactivated_at = NULL, updated_at = now()
WHERE id = $1 AND deactivated_at IS NOT NULL`
	if _, err := repo.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("Reactivate: %w", err)
	}
	return nil
}

// Delete removes the viewer row physically.
func (repo *ViewerRepo) Delete(ctx context.Context, id int64) error {
	const query = `DELETE FROM viewers WHERE id = $1`
	res, err := repo.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("Delete: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("Delete: no rows affected")
	}
	return nil
}

// GetActiveByEmail returns the active viewer with the given email, or nil.
func (repo *ViewerRepo) GetActiveByEmail(ctx context.Context, email string) (*entity.Viewer, error) {
	query := `
SELECT ` + viewerColumns + `
FROM viewers
WHERE email = $1 AND deactivated_at IS NULL
LIMIT 1`
	viewer, err := scanViewer(repo.db.QueryRowContext(ctx, query, email))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetActiveByEmail: %w", err)
	}
	return viewer, nil
}
