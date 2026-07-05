package subscriber_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	subUC "catchup-feed/internal/usecase/subscriber"
)

/* ───────── stubs ───────── */

type stubSubscriberRepo struct {
	byID          map[int64]*entity.Subscriber
	created       *entity.Subscriber
	updated       *entity.Subscriber
	deactivatedID int64
	deactivatedAt time.Time
	err           error
}

func (s *stubSubscriberRepo) Create(_ context.Context, sub *entity.Subscriber) error {
	if s.err != nil {
		return s.err
	}
	sub.ID = 42
	sub.CreatedAt = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	s.created = sub
	return nil
}

func (s *stubSubscriberRepo) Get(_ context.Context, id int64) (*entity.Subscriber, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.byID[id], nil
}

func (s *stubSubscriberRepo) List(_ context.Context) ([]*entity.Subscriber, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]*entity.Subscriber, 0, len(s.byID))
	for _, sub := range s.byID {
		out = append(out, sub)
	}
	return out, nil
}

func (s *stubSubscriberRepo) Update(_ context.Context, sub *entity.Subscriber) error {
	s.updated = sub
	return nil
}

func (s *stubSubscriberRepo) Deactivate(_ context.Context, id int64, t time.Time) error {
	s.deactivatedID = id
	s.deactivatedAt = t
	return nil
}

type stubTokenRepo struct {
	byID       map[int64]*entity.FeedToken
	created    *entity.FeedToken
	revokedID  int64
	revokedAt  time.Time
	revokeCall int
	createErr  error
}

func (s *stubTokenRepo) Create(_ context.Context, token *entity.FeedToken) error {
	if s.createErr != nil {
		return s.createErr
	}
	token.ID = 7
	token.CreatedAt = time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	s.created = token
	return nil
}

func (s *stubTokenRepo) Get(_ context.Context, id int64) (*entity.FeedToken, error) {
	return s.byID[id], nil
}

func (s *stubTokenRepo) GetActiveByHash(context.Context, string) (*entity.FeedToken, error) {
	return nil, errors.New("not used")
}

func (s *stubTokenRepo) ListBySubscriber(_ context.Context, subscriberID int64) ([]*entity.FeedToken, error) {
	out := make([]*entity.FeedToken, 0, len(s.byID))
	for _, t := range s.byID {
		if t.SubscriberID == subscriberID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *stubTokenRepo) Revoke(_ context.Context, id int64, t time.Time) error {
	s.revokeCall++
	s.revokedID = id
	s.revokedAt = t
	return nil
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
}

func newService(subs *stubSubscriberRepo, tokens *stubTokenRepo) subUC.Service {
	return subUC.Service{Subscribers: subs, Tokens: tokens, Now: fixedNow}
}

func activeSubscriber(id int64) *entity.Subscriber {
	return &entity.Subscriber{ID: id, Name: "友人A", CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
}

func deactivatedSubscriber(id int64) *entity.Subscriber {
	t := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	sub := activeSubscriber(id)
	sub.DeactivatedAt = &t
	return sub
}

/* ───────── subscriber CRUD ───────── */

func TestService_Create_Validation(t *testing.T) {
	tests := []struct {
		name    string
		input   subUC.Input
		wantErr error
	}{
		{name: "name required", input: subUC.Input{Name: ""}, wantErr: subUC.ErrNameRequired},
		{name: "whitespace-only name rejected", input: subUC.Input{Name: "   "}, wantErr: subUC.ErrNameRequired},
		{name: "valid", input: subUC.Input{Name: "友人A"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newService(&stubSubscriberRepo{}, &stubTokenRepo{})
			created, err := svc.Create(context.Background(), tt.input)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, int64(42), created.ID)
			assert.True(t, created.IsActive())
		})
	}
}

func TestService_Get_NotFound(t *testing.T) {
	svc := newService(&stubSubscriberRepo{byID: map[int64]*entity.Subscriber{}}, &stubTokenRepo{})
	_, err := svc.Get(context.Background(), 99)
	assert.ErrorIs(t, err, subUC.ErrSubscriberNotFound)
}

func TestService_Update(t *testing.T) {
	note := "配信時間の感想がほしい"
	email := "a@example.com"

	tests := []struct {
		name    string
		id      int64
		input   subUC.Input
		wantErr error
	}{
		{name: "not found", id: 99, input: subUC.Input{Name: "x"}, wantErr: subUC.ErrSubscriberNotFound},
		{name: "name required", id: 1, input: subUC.Input{Name: ""}, wantErr: subUC.ErrNameRequired},
		{name: "rewrites fields", id: 1, input: subUC.Input{Name: "改名", Note: &note, Email: &email}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subs := &stubSubscriberRepo{byID: map[int64]*entity.Subscriber{1: activeSubscriber(1)}}
			svc := newService(subs, &stubTokenRepo{})
			updated, err := svc.Update(context.Background(), tt.id, tt.input)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, "改名", updated.Name)
			assert.Equal(t, &note, updated.Note)
			assert.Equal(t, &email, updated.Email)
			require.NotNil(t, subs.updated)
		})
	}
}

func TestService_Deactivate(t *testing.T) {
	subs := &stubSubscriberRepo{byID: map[int64]*entity.Subscriber{1: activeSubscriber(1)}}
	svc := newService(subs, &stubTokenRepo{})

	require.NoError(t, svc.Deactivate(context.Background(), 1))
	assert.Equal(t, int64(1), subs.deactivatedID)
	assert.Equal(t, fixedNow(), subs.deactivatedAt)

	err := svc.Deactivate(context.Background(), 99)
	assert.ErrorIs(t, err, subUC.ErrSubscriberNotFound)
}

/* ───────── token lifecycle ───────── */

// TestService_IssueToken pins D-5: only the hash is persisted, and the
// returned plaintext hashes to exactly what was stored.
func TestService_IssueToken(t *testing.T) {
	tests := []struct {
		name    string
		byID    map[int64]*entity.Subscriber
		id      int64
		wantErr error
	}{
		{
			name: "issues for active subscriber",
			byID: map[int64]*entity.Subscriber{1: activeSubscriber(1)},
			id:   1,
		},
		{
			name:    "subscriber not found",
			byID:    map[int64]*entity.Subscriber{},
			id:      99,
			wantErr: subUC.ErrSubscriberNotFound,
		},
		{
			name:    "deactivated subscriber conflicts",
			byID:    map[int64]*entity.Subscriber{2: deactivatedSubscriber(2)},
			id:      2,
			wantErr: subUC.ErrSubscriberDeactivated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := &stubTokenRepo{}
			svc := newService(&stubSubscriberRepo{byID: tt.byID}, tokens)

			token, plaintext, err := svc.IssueToken(context.Background(), tt.id)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, tokens.created, "no token row must be created on error")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, token)
			assert.Equal(t, tt.id, token.SubscriberID)
			assert.NotEmpty(t, plaintext)
			// D-5: what is persisted is the hash of the returned plaintext,
			// never the plaintext itself.
			require.NotNil(t, tokens.created)
			assert.Equal(t, entity.HashFeedToken(plaintext), tokens.created.TokenHash)
			assert.NotEqual(t, plaintext, tokens.created.TokenHash)
		})
	}
}

func TestService_RevokeToken(t *testing.T) {
	t.Run("revokes an active token at Now", func(t *testing.T) {
		tokens := &stubTokenRepo{byID: map[int64]*entity.FeedToken{
			5: {ID: 5, SubscriberID: 1, TokenHash: "hash"},
		}}
		svc := newService(&stubSubscriberRepo{}, tokens)

		revoked, err := svc.RevokeToken(context.Background(), 5)
		require.NoError(t, err)
		require.NotNil(t, revoked.RevokedAt)
		assert.Equal(t, fixedNow(), *revoked.RevokedAt)
		assert.Equal(t, int64(5), tokens.revokedID)
	})

	t.Run("idempotent: already revoked keeps original timestamp", func(t *testing.T) {
		original := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
		tokens := &stubTokenRepo{byID: map[int64]*entity.FeedToken{
			5: {ID: 5, SubscriberID: 1, TokenHash: "hash", RevokedAt: &original},
		}}
		svc := newService(&stubSubscriberRepo{}, tokens)

		revoked, err := svc.RevokeToken(context.Background(), 5)
		require.NoError(t, err)
		require.NotNil(t, revoked.RevokedAt)
		assert.Equal(t, original, *revoked.RevokedAt, "original revocation time survives")
		assert.Zero(t, tokens.revokeCall, "no second UPDATE is issued")
	})

	t.Run("unknown token", func(t *testing.T) {
		svc := newService(&stubSubscriberRepo{}, &stubTokenRepo{byID: map[int64]*entity.FeedToken{}})
		_, err := svc.RevokeToken(context.Background(), 99)
		assert.ErrorIs(t, err, subUC.ErrTokenNotFound)
	})
}

func TestService_ListTokens_UnknownSubscriber(t *testing.T) {
	svc := newService(&stubSubscriberRepo{byID: map[int64]*entity.Subscriber{}}, &stubTokenRepo{})
	_, err := svc.ListTokens(context.Background(), 99)
	assert.ErrorIs(t, err, subUC.ErrSubscriberNotFound)
}

/* ───────── email validation (§5 持ち越し / C-11) ───────── */

func TestService_EmailValidation(t *testing.T) {
	strPtr := func(s string) *string { return &s }

	tests := []struct {
		name    string
		email   *string
		wantErr error
	}{
		{name: "nil email is allowed (clears the address)", email: nil},
		{name: "plain address", email: strPtr("friend@example.com")},
		{name: "address with plus tag", email: strPtr("friend+pulse@example.co.jp")},
		{name: "empty string rejected (use null to clear)", email: strPtr(""), wantErr: subUC.ErrInvalidEmail},
		{name: "missing @ rejected", email: strPtr("friend.example.com"), wantErr: subUC.ErrInvalidEmail},
		{name: "display-name form rejected", email: strPtr("Friend <friend@example.com>"), wantErr: subUC.ErrInvalidEmail},
		{name: "surrounding whitespace rejected", email: strPtr(" friend@example.com "), wantErr: subUC.ErrInvalidEmail},
		{name: "dotless domain rejected (goes into SMTP RCPT TO)", email: strPtr("user@localhost"), wantErr: subUC.ErrInvalidEmail},
		{name: "header injection rejected", email: strPtr("a@example.com\r\nBcc: x@example.com"), wantErr: subUC.ErrInvalidEmail},
		{name: "overlong address rejected", email: strPtr(strings.Repeat("a", 250) + "@e.com"), wantErr: subUC.ErrInvalidEmail},
	}

	for _, tt := range tests {
		t.Run("create/"+tt.name, func(t *testing.T) {
			svc := newService(&stubSubscriberRepo{}, &stubTokenRepo{})
			_, err := svc.Create(context.Background(), subUC.Input{Name: "友人A", Email: tt.email})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
		})
		t.Run("update/"+tt.name, func(t *testing.T) {
			subs := &stubSubscriberRepo{byID: map[int64]*entity.Subscriber{1: activeSubscriber(1)}}
			svc := newService(subs, &stubTokenRepo{})
			_, err := svc.Update(context.Background(), 1, subUC.Input{Name: "友人A", Email: tt.email})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}
