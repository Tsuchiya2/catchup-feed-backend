// Package viewer provides the viewer-account management HTTP handlers
// (D-27): admin-only CRUD over the read-only friend accounts, following the
// flat-path convention (C-21: /viewers, /viewers/{id}, /viewers/{id}/active).
package viewer

import (
	"net/http"
	"strconv"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/pathutil"
)

// DTO mirrors the viewers schema (D-27). PasswordHash never leaves the
// persistence/usecase layers; Active is derived from DeactivatedAt for
// frontend convenience (same shape as the subscriber DTO).
type DTO struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Email         string     `json:"email"`
	Active        bool       `json:"active"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeactivatedAt *time.Time `json:"deactivated_at"`
}

func toDTO(v *entity.Viewer) DTO {
	return DTO{
		ID:            v.ID,
		Name:          v.Name,
		Email:         v.Email,
		Active:        v.IsActive(),
		CreatedAt:     v.CreatedAt,
		UpdatedAt:     v.UpdatedAt,
		DeactivatedAt: v.DeactivatedAt,
	}
}

// CreateRequest is the POST /viewers body. All fields required; the
// password is set by the admin (D-27 (2)) and bcrypt-hashed server-side.
type CreateRequest struct {
	Name     string `json:"name" example:"Alice"`
	Email    string `json:"email" example:"alice@example.com"`
	Password string `json:"password" example:"correct-horse-battery"`
}

// UpdateRequest is the PUT /viewers/{id} body. name / email are full
// replacements; password is optional — omitted (null) keeps the current
// password, present re-sets it.
type UpdateRequest struct {
	Name     string  `json:"name" example:"Alice"`
	Email    string  `json:"email" example:"alice@example.com"`
	Password *string `json:"password,omitempty" example:"new-password-123"`
}

// ActiveRequest is the PUT /viewers/{id}/active body.
type ActiveRequest struct {
	Active bool `json:"active" example:"false"`
}

// pathID extracts the positive integer {id} path value.
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, pathutil.ErrInvalidID
	}
	return id, nil
}
