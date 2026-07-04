package subscriber_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	hsub "catchup-feed/internal/handler/http/subscriber"
)

const testBaseURL = "https://radio.example.com"

/* ───────── issue (D-5: plaintext exactly once) ───────── */

func TestIssueTokenHandler(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		setup    func(*stubSubscriberRepo)
		wantCode int
	}{
		{
			name:     "issues token for active subscriber",
			id:       "1",
			setup:    func(s *stubSubscriberRepo) { addSubscriber(s, 1, false) },
			wantCode: http.StatusCreated,
		},
		{
			name:     "subscriber not found",
			id:       "99",
			setup:    func(*stubSubscriberRepo) {},
			wantCode: http.StatusNotFound,
		},
		{
			name:     "deactivated subscriber conflicts",
			id:       "2",
			setup:    func(s *stubSubscriberRepo) { addSubscriber(s, 2, true) },
			wantCode: http.StatusConflict,
		},
		{
			name:     "invalid id",
			id:       "abc",
			setup:    func(*stubSubscriberRepo) {},
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, subs, tokens := newFixture()
			tt.setup(subs)

			rr := httptest.NewRecorder()
			handler := hsub.IssueTokenHandler{Svc: svc, PublicBaseURL: testBaseURL}
			handler.ServeHTTP(rr, requestWithID(http.MethodPost, "/subscribers/"+tt.id+"/tokens", tt.id, ""))
			require.Equal(t, tt.wantCode, rr.Code)

			if tt.wantCode != http.StatusCreated {
				return
			}

			var got hsub.IssuedTokenDTO
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))

			// D-5: the issue response is the one and only exposure of the
			// plaintext token and the subscription URL.
			assert.NotEmpty(t, got.Token)
			assert.Equal(t, testBaseURL+"/feeds/"+got.Token+"/feed.xml", got.FeedURL)
			assert.True(t, got.Active)
			assert.Equal(t, int64(1), got.SubscriberID)

			// Only the hash is persisted — and the hash never leaks back out.
			stored := tokens.byID[got.ID]
			require.NotNil(t, stored)
			assert.Equal(t, entity.HashFeedToken(got.Token), stored.TokenHash)
			assert.NotContains(t, rr.Body.String(), stored.TokenHash)
		})
	}
}

/* ───────── list (D-5: no plaintext, no hash) ───────── */

// TestListTokensHandler_NeverExposesSecrets pins D-5: the token list carries
// ID / dates / state only. Neither the plaintext (which the server no longer
// has) nor the stored hash may appear, on any later retrieval.
func TestListTokensHandler_NeverExposesSecrets(t *testing.T) {
	svc, subs, tokens := newFixture()
	addSubscriber(subs, 1, false)

	// Issue once: the plaintext exists only in this response.
	issueRec := httptest.NewRecorder()
	hsub.IssueTokenHandler{Svc: svc, PublicBaseURL: testBaseURL}.
		ServeHTTP(issueRec, requestWithID(http.MethodPost, "/subscribers/1/tokens", "1", ""))
	require.Equal(t, http.StatusCreated, issueRec.Code)
	var issued hsub.IssuedTokenDTO
	require.NoError(t, json.Unmarshal(issueRec.Body.Bytes(), &issued))

	// List: the same token comes back without any secret material.
	listRec := httptest.NewRecorder()
	hsub.ListTokensHandler{Svc: svc}.
		ServeHTTP(listRec, requestWithID(http.MethodGet, "/subscribers/1/tokens", "1", ""))
	require.Equal(t, http.StatusOK, listRec.Code)

	body := listRec.Body.String()
	assert.NotContains(t, body, issued.Token, "plaintext must never reappear after issue (D-5)")
	assert.NotContains(t, body, tokens.byID[issued.ID].TokenHash, "hash must never leave the DB layer (D-5)")
	assert.NotContains(t, body, "feed_url", "subscription URL is issue-time only (D-5)")

	var got []map[string]any
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.ElementsMatch(t,
		[]string{"id", "subscriber_id", "active", "created_at", "revoked_at"},
		keysOf(got[0]),
		"token list exposes exactly ID, owner, state and dates")
}

func TestListTokensHandler_UnknownSubscriber(t *testing.T) {
	svc, _, _ := newFixture()
	rr := httptest.NewRecorder()
	hsub.ListTokensHandler{Svc: svc}.
		ServeHTTP(rr, requestWithID(http.MethodGet, "/subscribers/99/tokens", "99", ""))
	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

/* ───────── revoke (§5.2: irreversible, idempotent) ───────── */

func TestRevokeTokenHandler(t *testing.T) {
	svc, _, tokens := newFixture()
	tokens.byID[5] = &entity.FeedToken{
		ID: 5, SubscriberID: 1, TokenHash: "stored-hash",
		CreatedAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	}
	tokens.nextID = 5

	// first revocation
	rr := httptest.NewRecorder()
	hsub.RevokeTokenHandler{Svc: svc}.ServeHTTP(rr, requestWithID(http.MethodDelete, "/tokens/5", "5", ""))
	require.Equal(t, http.StatusOK, rr.Code)

	var first hsub.RevokedTokenDTO
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &first))
	assert.False(t, first.Active)
	require.NotNil(t, first.RevokedAt)
	assert.Equal(t, fixedNow(), first.RevokedAt.UTC())
	assert.Contains(t, first.Note, "irreversible", "§5.2: response states revocation cannot be undone")
	assert.NotContains(t, rr.Body.String(), "stored-hash", "hash never leaves the DB layer (D-5)")

	// second revocation is idempotent: same timestamp, still 200
	rr2 := httptest.NewRecorder()
	hsub.RevokeTokenHandler{Svc: svc}.ServeHTTP(rr2, requestWithID(http.MethodDelete, "/tokens/5", "5", ""))
	require.Equal(t, http.StatusOK, rr2.Code)
	var second hsub.RevokedTokenDTO
	require.NoError(t, json.Unmarshal(rr2.Body.Bytes(), &second))
	require.NotNil(t, second.RevokedAt)
	assert.Equal(t, first.RevokedAt.UTC(), second.RevokedAt.UTC(), "idempotent revoke keeps original timestamp")
}

func TestRevokeTokenHandler_Errors(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		wantCode int
	}{
		{name: "unknown token", id: "99", wantCode: http.StatusNotFound},
		{name: "invalid id", id: "x", wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newFixture()
			rr := httptest.NewRecorder()
			hsub.RevokeTokenHandler{Svc: svc}.ServeHTTP(rr, requestWithID(http.MethodDelete, "/tokens/"+tt.id, tt.id, ""))
			assert.Equal(t, tt.wantCode, rr.Code)
		})
	}
}

/* ───────── routing sanity ───────── */

// TestRegister_RequiresJWT pins that every friend-management route rejects
// anonymous requests: the routes are wrapped in auth.Authz at registration.
func TestRegister_RequiresJWT(t *testing.T) {
	svc, subs, _ := newFixture()
	addSubscriber(subs, 1, false)

	mux := http.NewServeMux()
	hsub.Register(mux, svc, testBaseURL)

	routes := []struct{ method, path string }{
		{http.MethodGet, "/subscribers"},
		{http.MethodPost, "/subscribers"},
		{http.MethodGet, "/subscribers/1"},
		{http.MethodPut, "/subscribers/1"},
		{http.MethodDelete, "/subscribers/1"},
		{http.MethodPost, "/subscribers/1/tokens"},
		{http.MethodGet, "/subscribers/1/tokens"},
		{http.MethodDelete, "/tokens/1"},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(`{"name":"x"}`))
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusUnauthorized, rr.Code, "missing JWT must be rejected")
		})
	}
}
