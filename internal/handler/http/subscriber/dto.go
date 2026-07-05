// Package subscriber provides the friend-management HTTP handlers (§5.1):
// subscriber CRUD with logical deletion (C-8) and the feed token lifecycle
// (§5.2, D-5). All routes are admin-only JWT routes.
package subscriber

import (
	"net/http"
	"strconv"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/pathutil"
)

// DTO mirrors the §4 subscribers schema. Deletion is logical (C-8), so the
// list carries both active and deactivated friends; Active is derived from
// DeactivatedAt for frontend convenience.
type DTO struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Note          *string    `json:"note"`
	Email         *string    `json:"email"`
	Active        bool       `json:"active"`
	CreatedAt     time.Time  `json:"created_at"`
	DeactivatedAt *time.Time `json:"deactivated_at"`
}

func toDTO(s *entity.Subscriber) DTO {
	return DTO{
		ID:            s.ID,
		Name:          s.Name,
		Note:          s.Note,
		Email:         s.Email,
		Active:        s.IsActive(),
		CreatedAt:     s.CreatedAt,
		DeactivatedAt: s.DeactivatedAt,
	}
}

// TokenDTO is the list/revoke view of a feed token. It deliberately
// carries neither the plaintext nor the hash (D-5): the plaintext exists
// only in the issue response (IssuedTokenDTO) and the hash never leaves
// the persistence layer. Only ID, dates and derived state are exposed.
type TokenDTO struct {
	ID           int64      `json:"id"`
	SubscriberID int64      `json:"subscriber_id"`
	Active       bool       `json:"active"`
	CreatedAt    time.Time  `json:"created_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
}

func toTokenDTO(t *entity.FeedToken) TokenDTO {
	return TokenDTO{
		ID:           t.ID,
		SubscriberID: t.SubscriberID,
		Active:       !t.IsRevoked(),
		CreatedAt:    t.CreatedAt,
		RevokedAt:    t.RevokedAt,
	}
}

// IssuedTokenDTO is the one-time issue response (D-5): Token (plaintext)
// and FeedURL are returned exactly once, from POST /subscribers/{id}/tokens.
// They are never persisted and can never be retrieved again — a lost URL
// means revoking this token and issuing a new one (§5.2).
type IssuedTokenDTO struct {
	TokenDTO
	// Token is the base64url plaintext. Shown once, never again (D-5).
	Token string `json:"token"`
	// FeedURL is the ready-to-paste podcast subscription URL. Shown once,
	// never again (D-5).
	FeedURL string `json:"feed_url"`
}

// RevokedTokenDTO is the revoke response. Note pins §5.2: revocation is
// revoked_at only and irreversible; restoring access is always a new token.
type RevokedTokenDTO struct {
	TokenDTO
	Note string `json:"note"`
}

// revokeNote is returned by DELETE /tokens/{id} to make the §5.2 semantics
// explicit to the dashboard.
const revokeNote = "revocation is irreversible; issue a new token to restore access (§5.2)"

// pathID extracts the positive integer {id} path value.
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, pathutil.ErrInvalidID
	}
	return id, nil
}
