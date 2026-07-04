package subscriber_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	hsub "catchup-feed/internal/handler/http/subscriber"
	subUC "catchup-feed/internal/usecase/subscriber"
)

/* ───────── stubs ───────── */

type stubSubscriberRepo struct {
	byID map[int64]*entity.Subscriber
}

func (s *stubSubscriberRepo) Create(_ context.Context, sub *entity.Subscriber) error {
	sub.ID = 42
	sub.CreatedAt = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	s.byID[sub.ID] = sub
	return nil
}

func (s *stubSubscriberRepo) Get(_ context.Context, id int64) (*entity.Subscriber, error) {
	return s.byID[id], nil
}

func (s *stubSubscriberRepo) List(_ context.Context) ([]*entity.Subscriber, error) {
	out := make([]*entity.Subscriber, 0, len(s.byID))
	for _, sub := range s.byID {
		out = append(out, sub)
	}
	return out, nil
}

func (s *stubSubscriberRepo) Update(context.Context, *entity.Subscriber) error { return nil }

func (s *stubSubscriberRepo) Deactivate(_ context.Context, id int64, t time.Time) error {
	if sub, ok := s.byID[id]; ok && sub.DeactivatedAt == nil {
		sub.DeactivatedAt = &t
	}
	return nil
}

type stubTokenRepo struct {
	byID   map[int64]*entity.FeedToken
	nextID int64
}

func (s *stubTokenRepo) Create(_ context.Context, token *entity.FeedToken) error {
	s.nextID++
	token.ID = s.nextID
	token.CreatedAt = time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	s.byID[token.ID] = token
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
	if token, ok := s.byID[id]; ok && token.RevokedAt == nil {
		token.RevokedAt = &t
	}
	return nil
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
}

func newFixture() (subUC.Service, *stubSubscriberRepo, *stubTokenRepo) {
	subs := &stubSubscriberRepo{byID: map[int64]*entity.Subscriber{}}
	tokens := &stubTokenRepo{byID: map[int64]*entity.FeedToken{}}
	return subUC.Service{Subscribers: subs, Tokens: tokens, Now: fixedNow}, subs, tokens
}

func addSubscriber(subs *stubSubscriberRepo, id int64, deactivated bool) {
	sub := &entity.Subscriber{ID: id, Name: "友人A", CreatedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
	if deactivated {
		t := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
		sub.DeactivatedAt = &t
	}
	subs.byID[id] = sub
}

func requestWithID(method, target, id, body string) *http.Request {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	if id != "" {
		r.SetPathValue("id", id)
	}
	return r
}

/* ───────── subscriber CRUD ───────── */

func TestCreateHandler(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{name: "created", body: `{"name":"友人A","email":"a@example.com"}`, wantCode: http.StatusCreated},
		{name: "name required", body: `{"email":"a@example.com"}`, wantCode: http.StatusBadRequest},
		{name: "invalid json", body: `{`, wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newFixture()
			rr := httptest.NewRecorder()
			hsub.CreateHandler{Svc: svc}.ServeHTTP(rr, requestWithID(http.MethodPost, "/subscribers", "", tt.body))
			require.Equal(t, tt.wantCode, rr.Code)

			if tt.wantCode == http.StatusCreated {
				var got hsub.DTO
				require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
				assert.Equal(t, int64(42), got.ID)
				assert.Equal(t, "友人A", got.Name)
				assert.True(t, got.Active)
			}
		})
	}
}

func TestGetHandler(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		wantCode int
	}{
		{name: "found", id: "1", wantCode: http.StatusOK},
		{name: "not found", id: "99", wantCode: http.StatusNotFound},
		{name: "invalid id", id: "abc", wantCode: http.StatusBadRequest},
		{name: "non-positive id", id: "0", wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, subs, _ := newFixture()
			addSubscriber(subs, 1, false)
			rr := httptest.NewRecorder()
			hsub.GetHandler{Svc: svc}.ServeHTTP(rr, requestWithID(http.MethodGet, "/subscribers/"+tt.id, tt.id, ""))
			assert.Equal(t, tt.wantCode, rr.Code)
		})
	}
}

func TestUpdateHandler(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		body     string
		wantCode int
	}{
		{name: "updated", id: "1", body: `{"name":"改名","note":"感想ほしい"}`, wantCode: http.StatusOK},
		{name: "not found", id: "99", body: `{"name":"x"}`, wantCode: http.StatusNotFound},
		{name: "name required", id: "1", body: `{"note":"only"}`, wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, subs, _ := newFixture()
			addSubscriber(subs, 1, false)
			rr := httptest.NewRecorder()
			hsub.UpdateHandler{Svc: svc}.ServeHTTP(rr, requestWithID(http.MethodPut, "/subscribers/"+tt.id, tt.id, tt.body))
			require.Equal(t, tt.wantCode, rr.Code)

			if tt.wantCode == http.StatusOK {
				var got hsub.DTO
				require.NoError(t, json.NewDecoder(rr.Body).Decode(&got))
				assert.Equal(t, "改名", got.Name)
				require.NotNil(t, got.Note)
				assert.Equal(t, "感想ほしい", *got.Note)
				assert.Nil(t, got.Email, "omitted field is cleared (full replacement)")
			}
		})
	}
}

// TestDeleteHandler pins C-8: DELETE deactivates instead of deleting the row.
func TestDeleteHandler(t *testing.T) {
	svc, subs, _ := newFixture()
	addSubscriber(subs, 1, false)

	rr := httptest.NewRecorder()
	hsub.DeleteHandler{Svc: svc}.ServeHTTP(rr, requestWithID(http.MethodDelete, "/subscribers/1", "1", ""))
	require.Equal(t, http.StatusNoContent, rr.Code)

	require.Contains(t, subs.byID, int64(1), "row survives logical deletion")
	assert.NotNil(t, subs.byID[1].DeactivatedAt)

	rr = httptest.NewRecorder()
	hsub.DeleteHandler{Svc: svc}.ServeHTTP(rr, requestWithID(http.MethodDelete, "/subscribers/99", "99", ""))
	assert.Equal(t, http.StatusNotFound, rr.Code)
}
