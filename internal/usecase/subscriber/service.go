package subscriber

import (
	"context"
	"fmt"
	"strings"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// Input carries the subscriber fields shared by create and update. Name is
// required; Note / Email are optional (nil clears them on update).
type Input struct {
	Name  string
	Note  *string
	Email *string
}

// Service provides friend management use cases: subscriber CRUD and the
// token lifecycle. Deletion is always logical (C-8: tokens and access logs
// must survive for aggregation) and token revocation is irreversible —
// reissue is a new row (§5.2).
type Service struct {
	Subscribers repository.SubscriberRepository
	Tokens      repository.FeedTokenRepository
	// Now returns the current time; nil means time.Now. Injected for
	// deterministic tests of deactivation / revocation timestamps.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (in *Input) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return ErrNameRequired
	}
	return nil
}

// List returns all subscribers, active and deactivated, oldest first.
func (s *Service) List(ctx context.Context) ([]*entity.Subscriber, error) {
	subscribers, err := s.Subscribers.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list subscribers: %w", err)
	}
	return subscribers, nil
}

// Create registers a new friend. Name is required.
func (s *Service) Create(ctx context.Context, in Input) (*entity.Subscriber, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	subscriber := &entity.Subscriber{Name: in.Name, Note: in.Note, Email: in.Email}
	if err := s.Subscribers.Create(ctx, subscriber); err != nil {
		return nil, fmt.Errorf("create subscriber: %w", err)
	}
	return subscriber, nil
}

// Get returns the subscriber or ErrSubscriberNotFound.
func (s *Service) Get(ctx context.Context, id int64) (*entity.Subscriber, error) {
	subscriber, err := s.Subscribers.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get subscriber: %w", err)
	}
	if subscriber == nil {
		return nil, ErrSubscriberNotFound
	}
	return subscriber, nil
}

// Update rewrites name / note / email and returns the updated subscriber.
func (s *Service) Update(ctx context.Context, id int64, in Input) (*entity.Subscriber, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	subscriber, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	subscriber.Name = in.Name
	subscriber.Note = in.Note
	subscriber.Email = in.Email
	if err := s.Subscribers.Update(ctx, subscriber); err != nil {
		return nil, fmt.Errorf("update subscriber: %w", err)
	}
	return subscriber, nil
}

// Deactivate performs the logical deletion (C-8): the row survives so
// tokens and access logs keep aggregating; the subscriber's tokens stop
// verifying immediately (§5.2 checks subscriber activity). Idempotent —
// an already deactivated subscriber keeps its original timestamp.
func (s *Service) Deactivate(ctx context.Context, id int64) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if err := s.Subscribers.Deactivate(ctx, id, s.now()); err != nil {
		return fmt.Errorf("deactivate subscriber: %w", err)
	}
	return nil
}

// IssueToken generates a new feed token for an active subscriber and
// persists only its hash (D-5). The returned plaintext is shown to the
// admin exactly once — it is never stored and can never be re-derived;
// losing it means revoking and issuing a new token (§5.2).
func (s *Service) IssueToken(ctx context.Context, subscriberID int64) (token *entity.FeedToken, plaintext string, err error) {
	subscriber, err := s.Get(ctx, subscriberID)
	if err != nil {
		return nil, "", err
	}
	if !subscriber.IsActive() {
		return nil, "", ErrSubscriberDeactivated
	}
	plaintext, hash, err := entity.GenerateFeedToken()
	if err != nil {
		return nil, "", fmt.Errorf("issue token: %w", err)
	}
	token = &entity.FeedToken{SubscriberID: subscriberID, TokenHash: hash}
	if err := s.Tokens.Create(ctx, token); err != nil {
		return nil, "", fmt.Errorf("issue token: %w", err)
	}
	return token, plaintext, nil
}

// ListTokens returns all tokens of the subscriber, newest first. The
// entities carry only hashes; the HTTP layer must not expose even those
// (D-5: the DTO is limited to ID / dates / state).
func (s *Service) ListTokens(ctx context.Context, subscriberID int64) ([]*entity.FeedToken, error) {
	if _, err := s.Get(ctx, subscriberID); err != nil {
		return nil, err
	}
	tokens, err := s.Tokens.ListBySubscriber(ctx, subscriberID)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	return tokens, nil
}

// RevokeToken revokes the token (revoked_at update only, §5.2 — reissue is
// always a new row). Idempotent: revoking an already revoked token returns
// it unchanged with its original revocation timestamp.
func (s *Service) RevokeToken(ctx context.Context, id int64) (*entity.FeedToken, error) {
	token, err := s.Tokens.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("revoke token: %w", err)
	}
	if token == nil {
		return nil, ErrTokenNotFound
	}
	if token.IsRevoked() {
		return token, nil
	}
	now := s.now()
	if err := s.Tokens.Revoke(ctx, id, now); err != nil {
		return nil, fmt.Errorf("revoke token: %w", err)
	}
	token.RevokedAt = &now
	return token, nil
}
