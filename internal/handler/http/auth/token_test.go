package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authservice "catchup-feed/internal/service/auth"
	viewerUC "catchup-feed/internal/usecase/viewer"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestAuthService builds an AuthService backed by the real bcrypt
// provider with test credentials in the environment.
func newTestAuthService(t *testing.T) *authservice.AuthService {
	t.Helper()
	setAdminEnv(t, testAdminUser, testHash(t, testPassword))
	t.Setenv("JWT_SECRET", testJWTSecret)
	return authservice.NewAuthService(NewAdminAuthProvider())
}

// stubViewerAuthenticator is a canned ViewerAuthenticator: emails in creds
// authenticate with the mapped password; anything else fails with the
// interface-contract sentinel viewerUC.ErrInvalidCredentials (deactivated
// viewers simply aren't in the map, mirroring GetActiveByEmail returning no
// row). A non-nil err simulates an infrastructure failure (DB down).
type stubViewerAuthenticator struct {
	creds map[string]string
	err   error
}

func (s *stubViewerAuthenticator) Authenticate(_ context.Context, email, password string) error {
	if s.err != nil {
		return s.err
	}
	if want, ok := s.creds[email]; ok && want == password {
		return nil
	}
	return viewerUC.ErrInvalidCredentials
}

func TestTokenHandler(t *testing.T) {
	const (
		viewerEmail    = "friend@example.com"
		viewerPassword = "viewer-password-1"
	)
	viewers := &stubViewerAuthenticator{creds: map[string]string{viewerEmail: viewerPassword}}

	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{
			name:     "valid credentials",
			body:     `{"email":"` + testAdminUser + `","password":"` + testPassword + `"}`,
			wantCode: http.StatusOK,
		},
		{
			name:     "wrong password",
			body:     `{"email":"` + testAdminUser + `","password":"wrong-password-123"}`,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "wrong email",
			body:     `{"email":"someone@example.com","password":"` + testPassword + `"}`,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "empty credentials",
			body:     `{"email":"","password":""}`,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "invalid json",
			body:     `{not json`,
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "viewer login succeeds (D-27)",
			body:     `{"email":"` + viewerEmail + `","password":"` + viewerPassword + `"}`,
			wantCode: http.StatusOK,
		},
		{
			name:     "viewer wrong password",
			body:     `{"email":"` + viewerEmail + `","password":"wrong-password"}`,
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "deactivated viewer is rejected",
			// stub の creds に載っていない = アクティブ viewer が引けない
			body:     `{"email":"deactivated@example.com","password":"whatever-123"}`,
			wantCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := TokenHandler(newTestAuthService(t), viewers)

			req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)
			if tt.wantCode != http.StatusOK {
				assert.NotContains(t, rec.Body.String(), "token\":")
			}
		})
	}
}

// TestTokenHandler_NilViewerAuthenticator verifies admin-only issuance when
// no viewer store is wired: viewer-style credentials are rejected.
func TestTokenHandler_NilViewerAuthenticator(t *testing.T) {
	handler := TokenHandler(newTestAuthService(t), nil)

	body := `{"email":"friend@example.com","password":"viewer-password-1"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestTokenHandler_ViewerLookupFailure verifies that an infrastructure
// failure during the viewer fallback (DB down) still answers a generic 401
// — never a 500 that would distinguish it from a credential mismatch — and
// issues no token. (ログ側は reason=viewer_lookup_failed で区別される。)
func TestTokenHandler_ViewerLookupFailure(t *testing.T) {
	viewers := &stubViewerAuthenticator{err: errors.New("db down")}
	handler := TokenHandler(newTestAuthService(t), viewers)

	body := `{"email":"friend@example.com","password":"viewer-password-1"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, rec.Body.String(), "token\":")
}

// TestTokenHandler_IssuedClaims verifies that the issued admin JWT carries
// sub/iat/exp plus role=admin (D-27) and passes the admin-only middleware.
func TestTokenHandler_IssuedClaims(t *testing.T) {
	handler := TokenHandler(newTestAuthService(t), nil)

	body := `{"email":"` + testAdminUser + `","password":"` + testPassword + `"}`
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)

	tok, err := jwt.Parse(resp.Token, func(parsed *jwt.Token) (interface{}, error) {
		require.Equal(t, jwt.SigningMethodHS256.Alg(), parsed.Method.Alg())
		return []byte(testJWTSecret), nil
	})
	require.NoError(t, err)
	require.True(t, tok.Valid)

	claims, ok := tok.Claims.(jwt.MapClaims)
	require.True(t, ok)

	assert.Equal(t, testAdminUser, claims["sub"])
	assert.Equal(t, RoleAdmin, claims["role"], "admin tokens must carry role=admin (D-27)")

	exp, ok := claims["exp"].(float64)
	require.True(t, ok)
	assert.InDelta(t, time.Now().Add(tokenTTL).Unix(), int64(exp), 5)

	// 発行されたトークンは Authz を通過できる
	middleware := Authz(okHandler())
	protected := httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	protected.Header.Set("Authorization", "Bearer "+resp.Token)
	protectedRec := httptest.NewRecorder()
	middleware.ServeHTTP(protectedRec, protected)
	assert.Equal(t, http.StatusOK, protectedRec.Code)
}
