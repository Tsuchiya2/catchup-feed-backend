package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authservice "catchup-feed/internal/service/auth"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newIntegrationServer wires TokenHandler and an Authz-protected mux the
// same way cmd/server does: /auth/token is public, everything else requires
// the administrator's JWT.
func newIntegrationServer(t *testing.T) http.Handler {
	t.Helper()
	setAdminEnv(t, testAdminUser, testHash(t, testPassword))
	t.Setenv("JWT_SECRET", testJWTSecret)

	protected := http.NewServeMux()
	protected.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("protected"))
	})

	// Mirror cmd/server's two-level routing: a public mux owns /auth/*, and
	// the root mux delegates those exact paths to it while sending everything
	// else to the Authz-protected mux. This structure matters for method
	// matching — the POST-only logout route must 405 other methods instead of
	// falling through to the catch-all.
	publicMux := http.NewServeMux()
	publicMux.Handle("/auth/token", TokenHandler(authservice.NewAuthService(NewAdminAuthProvider())))
	// logout is POST-only so a reflected GET (<img src=".../auth/logout">)
	// cannot force-logout a victim.
	publicMux.Handle("POST /auth/logout", LogoutHandler())

	mux := http.NewServeMux()
	mux.Handle("/auth/token", publicMux)
	mux.Handle("/auth/logout", publicMux)
	mux.Handle("/", Authz(protected))
	return mux
}

// login posts credentials and returns the response recorder.
func login(t *testing.T, server http.Handler, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/auth/token", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

// TestIntegration_AdminLoginFlow covers the full path: bcrypt login →
// issued JWT → access to a protected endpoint.
func TestIntegration_AdminLoginFlow(t *testing.T) {
	server := newIntegrationServer(t)

	rec := login(t, server, testAdminUser, testPassword)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)

	req := httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	req.Header.Set("Authorization", "Bearer "+resp.Token)
	protectedRec := httptest.NewRecorder()
	server.ServeHTTP(protectedRec, req)

	assert.Equal(t, http.StatusOK, protectedRec.Code)
	assert.Equal(t, "protected", protectedRec.Body.String())
}

func TestIntegration_LoginRejectsBadCredentials(t *testing.T) {
	server := newIntegrationServer(t)

	tests := []struct {
		name     string
		email    string
		password string
	}{
		{"wrong password", testAdminUser, "totally-wrong-password"},
		{"unknown user", "demo@example.com", testPassword},
		{"empty credentials", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := login(t, server, tt.email, tt.password)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.NotContains(t, rec.Body.String(), "token\":")
		})
	}
}

// TestIntegration_LegacyViewerTokenRejected replays the old multi-user
// scenario end to end: a viewer-style token signed with the current secret
// must never reach the admin API (403), and an unsigned tamper attempt must
// fail signature validation (401).
func TestIntegration_LegacyViewerTokenRejected(t *testing.T) {
	server := newIntegrationServer(t)

	viewerToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  "demo@example.com",
		"role": "viewer",
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	}).SignedString([]byte(testJWTSecret))
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)

	tampered := tamperSub(t, viewerToken, testAdminUser)
	req = httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	req.Header.Set("Authorization", "Bearer "+tampered)
	rec = httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestIntegration_ProtectedWithoutToken(t *testing.T) {
	server := newIntegrationServer(t)

	req := httptest.NewRequest(http.MethodGet, "/subscribers", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestIntegration_LogoutMethodRestriction verifies /auth/logout is POST-only,
// wired the same way cmd/server registers it. A reflected GET
// (<img src=".../auth/logout">) must be rejected with 405 and must NOT expire
// the auth cookie, closing the GET-CSRF force-logout vector. POST still
// clears the cookie with 204.
func TestIntegration_LogoutMethodRestriction(t *testing.T) {
	server := newIntegrationServer(t)

	tests := []struct {
		name          string
		method        string
		wantCode      int
		wantSetCookie bool
	}{
		{name: "GET is rejected without clearing cookie", method: http.MethodGet, wantCode: http.StatusMethodNotAllowed, wantSetCookie: false},
		{name: "HEAD is rejected", method: http.MethodHead, wantCode: http.StatusMethodNotAllowed, wantSetCookie: false},
		{name: "POST clears the cookie", method: http.MethodPost, wantCode: http.StatusNoContent, wantSetCookie: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/auth/logout", nil)
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)

			c := findAuthCookie(t, rec)
			if tt.wantSetCookie {
				require.NotNil(t, c, "POST logout must emit an expiring Set-Cookie")
				assert.Empty(t, c.Value)
				assert.True(t, c.MaxAge < 0, "logout cookie must delete (Max-Age<=0)")
			} else {
				assert.Nil(t, c, "rejected method must not emit a Set-Cookie (no forced logout)")
			}

			// stdlib ServeMux advertises the allowed methods on 405 for the
			// reflected-GET vector (the CSRF path). HEAD is handled specially
			// by ServeMux and may omit Allow, so only assert it for GET.
			if tt.method == http.MethodGet {
				assert.Contains(t, rec.Header().Get("Allow"), http.MethodPost)
			}
		})
	}
}
