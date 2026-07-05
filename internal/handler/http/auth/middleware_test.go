package auth

import (
	"encoding/base64"
	"encoding/json"
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

// adminClaims returns the claims TokenHandler issues for the administrator.
func adminClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub": testAdminUser,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(1 * time.Hour).Unix(),
	}
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

// TestAuthz_NonAdminSubjectForbidden is the regression test for C-20: a
// validly-signed token whose subject is not the administrator — e.g. a
// leftover viewer token from the old multi-user implementation — must be
// rejected with 403 on every admin endpoint.
func TestAuthz_NonAdminSubjectForbidden(t *testing.T) {
	setAuthzEnv(t)
	middleware := Authz(okHandler())

	legacyViewerToken := signToken(t, testJWTSecret, jwt.MapClaims{
		"sub":  "demo@example.com",
		"role": "viewer", // 旧実装のクレーム。現在は無視され、sub のみで判定される
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	})
	nonAdminToken := signToken(t, testJWTSecret, jwt.MapClaims{
		"sub": "friend@example.com",
		"exp": time.Now().Add(1 * time.Hour).Unix(),
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
	}

	for _, token := range []struct {
		name  string
		value string
	}{
		{"legacy viewer token", legacyViewerToken},
		{"non-admin subject token", nonAdminToken},
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

// TestAuthz_LegacyAdminTokenStillAccepted documents that extra legacy claims
// (role) are ignored: what matters is the signature and the subject.
func TestAuthz_LegacyAdminTokenStillAccepted(t *testing.T) {
	setAuthzEnv(t)
	middleware := Authz(okHandler())

	legacyAdminToken := signToken(t, testJWTSecret, jwt.MapClaims{
		"sub":  testAdminUser,
		"role": "admin",
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	})

	req := httptest.NewRequest(http.MethodGet, "/articles", nil)
	req.Header.Set("Authorization", "Bearer "+legacyAdminToken)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
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
