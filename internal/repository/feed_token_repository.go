package repository

import (
	"context"
	"time"

	"catchup-feed/internal/domain/entity"
)

// FeedTokenRepository persists subscription tokens (feed_tokens table, §4).
// Only SHA-256 hex hashes are stored (D-5). Revocation is an update of
// revoked_at; reissue is always a new row.
type FeedTokenRepository interface {
	// Create inserts a token row and sets token.ID.
	Create(ctx context.Context, token *entity.FeedToken) error
	// GetActiveByHash resolves a request token hash to a valid token:
	// revoked_at IS NULL and the owning subscriber is active (§5.2).
	// Returns nil when no such token exists (revoked, unknown, or the
	// subscriber was deactivated) — the caller cannot distinguish these,
	// by design.
	GetActiveByHash(ctx context.Context, tokenHash string) (*entity.FeedToken, error)
	// ListBySubscriber returns all tokens of a subscriber, newest first.
	ListBySubscriber(ctx context.Context, subscriberID int64) ([]*entity.FeedToken, error)
	// Revoke marks the token revoked as of t.
	Revoke(ctx context.Context, id int64, t time.Time) error
}
