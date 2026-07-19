package repository

import (
	"context"
	"errors"
	"time"

	"catchup-feed/internal/domain/entity"
)

// ErrDuplicateViewerEmail is returned by Create / Update when the email
// collides with another viewer (viewers.email UNIQUE). The use case maps it
// to a client-facing conflict error.
var ErrDuplicateViewerEmail = errors.New("viewer email already exists")

// ViewerRepository persists read-only dashboard accounts (viewers table,
// D-27). Unlike subscribers, viewers support both logical deactivation
// (login/browse blocked immediately) and physical deletion.
type ViewerRepository interface {
	// Create inserts the viewer and sets viewer.ID / CreatedAt / UpdatedAt.
	// Returns ErrDuplicateViewerEmail on an email collision.
	Create(ctx context.Context, viewer *entity.Viewer) error
	// Get returns the viewer, or nil when not found.
	Get(ctx context.Context, id int64) (*entity.Viewer, error)
	// List returns all viewers (active and deactivated), oldest first.
	List(ctx context.Context) ([]*entity.Viewer, error)
	// Update rewrites name / email / password_hash and bumps updated_at.
	// Returns ErrDuplicateViewerEmail on an email collision.
	Update(ctx context.Context, viewer *entity.Viewer) error
	// Deactivate marks the viewer inactive as of t (idempotent: an already
	// deactivated viewer keeps its original timestamp). Login and browsing
	// are blocked on the next request (D-27 (4): DB re-check per request).
	Deactivate(ctx context.Context, id int64, t time.Time) error
	// Reactivate clears deactivated_at (idempotent).
	Reactivate(ctx context.Context, id int64) error
	// Delete removes the viewer row physically.
	Delete(ctx context.Context, id int64) error
	// GetActiveByEmail returns the active (deactivated_at IS NULL) viewer
	// with the given email, or nil when no such viewer exists. Used for
	// login and for the per-request re-validation in the auth middleware.
	GetActiveByEmail(ctx context.Context, email string) (*entity.Viewer, error)
}
