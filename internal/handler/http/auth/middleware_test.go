package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testJWTSecret = "test-secret-key-at-least-32-characters-long"

// setAuthzEnv configures everything Authz reads from the environment.
// Authz captures env at construction time, so tests must call this before
// building the middleware.
func setAuthzEnv(t *testing.T) {
	t.Helper()
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv(EnvAdminUser, testAdminUser)
}

// signToken builds an HS256 token with the given claims.
func signToken(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return signed
}

// adminClaims returns the claims TokenHandler issues for the administrator
// (D-27: tokens carry a role claim).
func adminClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub":  testAdminUser,
		"role": RoleAdmin,
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	}
}

// viewerClaims returns the claims TokenHandler issues for a viewer.
func viewerClaims(email string) jwt.MapClaims {
	return jwt.MapClaims{
		"sub":  email,
		"role": RoleViewer,
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	}
}

// stubViewerVerifier is a canned ViewerVerifier for middleware tests.
type stubViewerVerifier struct {
	active map[string]bool
	err    error
}

func (s *stubViewerVerifier) IsActiveViewer(_ context.Context, email string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.active[email], nil
}

// tamperSub swaps the sub claim in an already-signed token without
// re-signing it, simulating a claim-tampering attack.
func tamperSub(t *testing.T, token, newSub string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(payload, &claims))
	claims["sub"] = newSub
	altered, err := json.Marshal(claims)
	require.NoError(t, err)

	parts[1] = base64.RawURLEncoding.EncodeToString(altered)
	return strings.Join(parts, ".")
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})
}

func TestAuthz_PublicEndpointsBypassAuth(t *testing.T) {
	setAuthzEnv(t)
	middleware := Authz(okHandler())

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"health check", http.MethodGet, "/health"},
		{"readiness probe", http.MethodGet, "/ready"},
		{"liveness probe", http.MethodGet, "/live"},
		{"swagger ui", http.MethodGet, "/swagger/index.html"},
		{"auth token", http.MethodPost, "/auth/token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

// TestAuthz_TokenValidation covers rejection of missing, malformed, forged
// and tampered tokens (C-20: 不正・改ざんクレームの拒否).
func TestAuthz_TokenValidation(t *testing.T) {
	setAuthzEnv(t)

	tests := []struct {
		name       string
		authHeader func(t *testing.T) string
		wantCode   int
	}{
		{
			name:       "no token",
			authHeader: func(*testing.T) string { return "" },
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "missing bearer prefix",
			authHeader: func(t *testing.T) string { return signToken(t, testJWTSecret, adminClaims()) },
			wantCode:   http.StatusUnauthorized,
		},
		{
			name:       "malformed token",
			authHeader: func(*testing.T) string { return "Bearer not.a.token" },
			wantCode:   http.StatusUnauthorized,
		},
		{
			name: "expired token",
			authHeader: func(t *testing.T) string {
				claims := adminClaims()
				claims["exp"] = time.Now().Add(-1 * time.Hour).Unix()
				return "Bearer " + signToken(t, testJWTSecret, claims)
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "missing exp claim",
			authHeader: func(t *testing.T) string {
				return "Bearer " + signToken(t, testJWTSecret, jwt.MapClaims{"sub": testAdminUser})
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "missing sub claim",
			authHeader: func(t *testing.T) string {
				claims := adminClaims()
				delete(claims, "sub")
				return "Bearer " + signToken(t, testJWTSecret, claims)
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "empty sub claim",
			authHeader: func(t *testing.T) string {
				claims := adminClaims()
				claims["sub"] = ""
				return "Bearer " + signToken(t, testJWTSecret, claims)
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "token signed with wrong secret",
			authHeader: func(t *testing.T) string {
				return "Bearer " + signToken(t, "attacker-controlled-secret-32-chars!!", adminClaims())
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "alg none token",
			authHeader: func(t *testing.T) string {
				tok := jwt.NewWithClaims(jwt.SigningMethodNone, adminClaims())
				signed, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
				require.NoError(t, err)
				return "Bearer " + signed
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "alg substitution to HS512",
			authHeader: func(t *testing.T) string {
				signed, err := jwt.NewWithClaims(jwt.SigningMethodHS512, adminClaims()).
					SignedString([]byte(testJWTSecret))
				require.NoError(t, err)
				return "Bearer " + signed
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "tampered sub claim without re-signing",
			authHeader: func(t *testing.T) string {
				claims := adminClaims()
				claims["sub"] = "friend@example.com"
				legit := signToken(t, testJWTSecret, claims)
				return "Bearer " + tamperSub(t, legit, testAdminUser)
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "valid admin token",
			authHeader: func(t *testing.T) string {
				return "Bearer " + signToken(t, testJWTSecret, adminClaims())
			},
			wantCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := Authz(okHandler())
			req := httptest.NewRequest(http.MethodGet, "/articles", nil)
			if header := tt.authHeader(t); header != "" {
				req.Header.Set("Authorization", header)
			}
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}

// TestAuthz_NonAdminForbidden is the C-20 regression test re-read for the
// two-role world (D-27): on the admin-only middleware, a validly-signed
// token must be rejected with 403 when its role is missing (pre-D-27
// legacy token), unknown, or viewer — and when role=admin but the subject
// is not the administrator.
func TestAuthz_NonAdminForbidden(t *testing.T) {
	setAuthzEnv(t)
	middleware := Authz(okHandler())

	viewerToken := signToken(t, testJWTSecret, viewerClaims("demo@example.com"))
	roleLessToken := signToken(t, testJWTSecret, jwt.MapClaims{
		"sub": testAdminUser, // 旧トークン: 正しい管理者 sub でも role なしは 403
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})
	unknownRoleToken := signToken(t, testJWTSecret, jwt.MapClaims{
		"sub":  testAdminUser,
		"role": "superadmin",
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	})
	nonAdminSubjectToken := signToken(t, testJWTSecret, jwt.MapClaims{
		"sub":  "friend@example.com",
		"role": RoleAdmin,
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	})

	adminEndpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/articles"},
		{http.MethodPost, "/articles"},
		{http.MethodGet, "/sources"},
		{http.MethodDelete, "/sources/1"},
		{http.MethodGet, "/subscribers"},
		{http.MethodPost, "/subscribers/1/tokens"},
		{http.MethodDelete, "/tokens/1"},
		{http.MethodGet, "/access-logs"},
		{http.MethodGet, "/viewers"},
		{http.MethodPost, "/viewers"},
	}

	for _, token := range []struct {
		name  string
		value string
	}{
		{"viewer role token", viewerToken},
		{"role-less legacy token", roleLessToken},
		{"unknown role token", unknownRoleToken},
		{"non-admin subject token", nonAdminSubjectToken},
	} {
		for _, ep := range adminEndpoints {
			t.Run(token.name+" "+ep.method+" "+ep.path, func(t *testing.T) {
				req := httptest.NewRequest(ep.method, ep.path, nil)
				req.Header.Set("Authorization", "Bearer "+token.value)
				rec := httptest.NewRecorder()

				middleware.ServeHTTP(rec, req)

				assert.Equal(t, http.StatusForbidden, rec.Code)
			})
		}
	}
}

// TestAuthzWithViewer covers the role-aware outer middleware (D-27): the
// admin passes everywhere, the viewer only reaches the read-only allowlist
// after the per-request DB re-validation, and a deactivated / deleted
// viewer's still-valid JWT is rejected immediately.
func TestAuthzWithViewer(t *testing.T) {
	const viewerEmail = "friend@example.com"

	tests := []struct {
		name     string
		verifier ViewerVerifier
		claims   jwt.MapClaims
		method   string
		path     string
		wantCode int
	}{
		{
			name:     "viewer allowed on GET /sources",
			verifier: &stubViewerVerifier{active: map[string]bool{viewerEmail: true}},
			claims:   viewerClaims(viewerEmail),
			method:   http.MethodGet, path: "/sources",
			wantCode: http.StatusOK,
		},
		{
			name:     "viewer allowed on GET /auth/me",
			verifier: &stubViewerVerifier{active: map[string]bool{viewerEmail: true}},
			claims:   viewerClaims(viewerEmail),
			method:   http.MethodGet, path: "/auth/me",
			wantCode: http.StatusOK,
		},
		{
			name:     "viewer forbidden on GET /sources/search",
			verifier: &stubViewerVerifier{active: map[string]bool{viewerEmail: true}},
			claims:   viewerClaims(viewerEmail),
			method:   http.MethodGet, path: "/sources/search",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "viewer forbidden on POST /sources",
			verifier: &stubViewerVerifier{active: map[string]bool{viewerEmail: true}},
			claims:   viewerClaims(viewerEmail),
			method:   http.MethodPost, path: "/sources",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "viewer forbidden on GET /subscribers",
			verifier: &stubViewerVerifier{active: map[string]bool{viewerEmail: true}},
			claims:   viewerClaims(viewerEmail),
			method:   http.MethodGet, path: "/subscribers",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "viewer forbidden on viewer management API",
			verifier: &stubViewerVerifier{active: map[string]bool{viewerEmail: true}},
			claims:   viewerClaims(viewerEmail),
			method:   http.MethodPost, path: "/viewers",
			wantCode: http.StatusForbidden,
		},
		{
			name: "deactivated viewer with valid JWT is cut off immediately",
			// DB 再検証が false を返す = 無効化済み or 物理削除済み(D-27 (4))
			verifier: &stubViewerVerifier{active: map[string]bool{}},
			claims:   viewerClaims(viewerEmail),
			method:   http.MethodGet, path: "/sources",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "viewer re-validation failure fails closed",
			verifier: &stubViewerVerifier{err: errors.New("db down")},
			claims:   viewerClaims(viewerEmail),
			method:   http.MethodGet, path: "/sources",
			wantCode: http.StatusInternalServerError,
		},
		{
			name:     "role-less legacy token forbidden even on allowlisted route",
			verifier: &stubViewerVerifier{active: map[string]bool{viewerEmail: true}},
			claims: jwt.MapClaims{
				"sub": viewerEmail,
				"exp": time.Now().Add(1 * time.Hour).Unix(),
			},
			method: http.MethodGet, path: "/sources",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "unknown role forbidden even on allowlisted route",
			verifier: &stubViewerVerifier{active: map[string]bool{viewerEmail: true}},
			claims: jwt.MapClaims{
				"sub":  viewerEmail,
				"role": "editor",
				"exp":  time.Now().Add(1 * time.Hour).Unix(),
			},
			method: http.MethodGet, path: "/sources",
			wantCode: http.StatusForbidden,
		},
		{
			name:     "admin passes admin routes",
			verifier: &stubViewerVerifier{active: map[string]bool{}},
			claims:   adminClaims(),
			method:   http.MethodPost, path: "/sources",
			wantCode: http.StatusOK,
		},
		{
			name:     "admin passes viewer-allowlisted routes too",
			verifier: &stubViewerVerifier{active: map[string]bool{}},
			claims:   adminClaims(),
			method:   http.MethodGet, path: "/sources",
			wantCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAuthzEnv(t)
			middleware := AuthzWithViewer(tt.verifier)(okHandler())

			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Authorization", "Bearer "+signToken(t, testJWTSecret, tt.claims))
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}

// TestViewerAllowed pins the allowlist matching semantics, in particular
// the single-trailing-slash normalization: "/sources/" is treated as
// "/sources" (mirroring ServeMux redirect behaviour), while everything
// else — subpaths, double slashes, other methods — stays denied.
func TestViewerAllowed(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		{"GET /sources", http.MethodGet, "/sources", true},
		{"GET /sources with single trailing slash", http.MethodGet, "/sources/", true},
		{"GET /auth/me", http.MethodGet, "/auth/me", true},
		{"GET /auth/me with single trailing slash", http.MethodGet, "/auth/me/", true},
		{"double trailing slash is not normalized", http.MethodGet, "/sources//", false},
		{"subpath of allowlisted route", http.MethodGet, "/sources/search", false},
		{"other method on allowlisted path", http.MethodPost, "/sources", false},
		{"HEAD is not GET", http.MethodHead, "/sources", false},
		{"root path", http.MethodGet, "/", false},
		{"unlisted route", http.MethodGet, "/subscribers", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, viewerAllowed(tt.method, tt.path))
		})
	}
}

// TestAuthzWithViewer_IdentityInContext verifies the viewer's subject and
// role reach downstream handlers (GET /sources uses the role to force the
// active-only listing; /auth/me echoes both).
func TestAuthzWithViewer_IdentityInContext(t *testing.T) {
	setAuthzEnv(t)
	const viewerEmail = "friend@example.com"

	var gotSub, gotRole string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSub = SubjectFromContext(r.Context())
		gotRole = RoleFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	verifier := &stubViewerVerifier{active: map[string]bool{viewerEmail: true}}
	middleware := AuthzWithViewer(verifier)(inner)

	req := httptest.NewRequest(http.MethodGet, "/sources", nil)
	req.Header.Set("Authorization", "Bearer "+signToken(t, testJWTSecret, viewerClaims(viewerEmail)))
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, viewerEmail, gotSub)
	assert.Equal(t, RoleViewer, gotRole)
}

// TestAuthz_FailsClosedWithoutAdminUser verifies that a server booted
// without ADMIN_USER rejects every protected request instead of matching an
// empty subject.
func TestAuthz_FailsClosedWithoutAdminUser(t *testing.T) {
	t.Setenv("JWT_SECRET", testJWTSecret)
	t.Setenv(EnvAdminUser, "")
	middleware := Authz(okHandler())

	token := signToken(t, testJWTSecret, jwt.MapClaims{
		"sub": "",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestAuthz_FailsClosedWithoutJWTSecret verifies that a server booted
// without JWT_SECRET rejects every protected request: an empty HS256 key
// would let anyone forge a validly signed token, so the middleware must
// never hand an empty secret to signature validation.
func TestAuthz_FailsClosedWithoutJWTSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "")
	t.Setenv(EnvAdminUser, testAdminUser)
	middleware := Authz(okHandler())

	// 攻撃者は空鍵で「正しく署名された」管理者トークンを作れてしまう
	forged := signToken(t, "", adminClaims())

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	req.Header.Set("Authorization", "Bearer "+forged)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// TestAuthz_UserInContext verifies the authenticated subject is propagated
// to downstream handlers.
func TestAuthz_UserInContext(t *testing.T) {
	setAuthzEnv(t)

	var gotUser any
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = r.Context().Value(ctxUser)
		w.WriteHeader(http.StatusOK)
	})
	middleware := Authz(inner)

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	req.Header.Set("Authorization", "Bearer "+signToken(t, testJWTSecret, adminClaims()))
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, testAdminUser, gotUser)
}
