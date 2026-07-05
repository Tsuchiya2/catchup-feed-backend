package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

const feedTokenColumns = "id, subscriber_id, token_hash, created_at, revoked_at"

// FeedTokenRepo persists subscription tokens (§4). Only SHA-256 hex hashes
// are stored (D-5); the SQL always uses placeholders, never interpolation.
type FeedTokenRepo struct{ db *sql.DB }

func NewFeedTokenRepo(db *sql.DB) repository.FeedTokenRepository {
	return &FeedTokenRepo{db: db}
}

func scanFeedToken(s scanner) (*entity.FeedToken, error) {
	var token entity.FeedToken
	if err := s.Scan(
		&token.ID, &token.SubscriberID, &token.TokenHash,
		&token.CreatedAt, &token.RevokedAt,
	); err != nil {
		return nil, err
	}
	return &token, nil
}

// Create inserts a token row and sets token.ID / CreatedAt.
func (repo *FeedTokenRepo) Create(ctx context.Context, token *entity.FeedToken) error {
	const query = `
INSERT INTO feed_tokens (subscriber_id, token_hash)
VALUES ($1, $2)
RETURNING id, created_at`
	err := repo.db.QueryRowContext(ctx, query,
		token.SubscriberID, token.TokenHash,
	).Scan(&token.ID, &token.CreatedAt)
	if err != nil {
		return fmt.Errorf("Create: %w", err)
	}
	return nil
}

// Get returns the token by ID (revoked or not), or nil when not found.
func (repo *FeedTokenRepo) Get(ctx context.Context, id int64) (*entity.FeedToken, error) {
	query := `
SELECT ` + feedTokenColumns + `
FROM feed_tokens
WHERE id = $1
LIMIT 1`
	token, err := scanFeedToken(repo.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get: %w", err)
	}
	return token, nil
}

// GetActiveByHash implements the §5.2 verification lookup: the token must
// not be revoked and its subscriber must still be active. Returns nil when
// no such token exists. A DB roundtrip per feed request is fine at this
// scale (§5.2).
func (repo *FeedTokenRepo) GetActiveByHash(ctx context.Context, tokenHash string) (*entity.FeedToken, error) {
	const query = `
SELECT t.id, t.subscriber_id, t.token_hash, t.created_at, t.revoked_at
FROM feed_tokens t
INNER JOIN subscribers s ON s.id = t.subscriber_id
WHERE t.token_hash = $1
  AND t.revoked_at IS NULL
  AND s.deactivated_at IS NULL
LIMIT 1`
	token, err := scanFeedToken(repo.db.QueryRowContext(ctx, query, tokenHash))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetActiveByHash: %w", err)
	}
	return token, nil
}

// ListBySubscriber returns all tokens of a subscriber, newest first.
func (repo *FeedTokenRepo) ListBySubscriber(ctx context.Context, subscriberID int64) ([]*entity.FeedToken, error) {
	query := `
SELECT ` + feedTokenColumns + `
FROM feed_tokens
WHERE subscriber_id = $1
ORDER BY id DESC`
	rows, err := repo.db.QueryContext(ctx, query, subscriberID)
	if err != nil {
		return nil, fmt.Errorf("ListBySubscriber: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tokens := make([]*entity.FeedToken, 0, 10)
	for rows.Next() {
		token, err := scanFeedToken(rows)
		if err != nil {
			return nil, fmt.Errorf("ListBySubscriber: %w", err)
		}
		tokens = append(tokens, token)
	}
	return tokens, rows.Err()
}

// Revoke marks the token revoked as of t (idempotent: an already revoked
// token keeps its original timestamp). Reissue is always a new row (§5.2).
func (repo *FeedTokenRepo) Revoke(ctx context.Context, id int64, t time.Time) error {
	const query = `
UPDATE feed_tokens SET revoked_at = $1
WHERE id = $2 AND revoked_at IS NULL`
	if _, err := repo.db.ExecContext(ctx, query, t, id); err != nil {
		return fmt.Errorf("Revoke: %w", err)
	}
	return nil
}
