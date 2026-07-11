package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
)

// authCookie builds a request cookie carrying the given JWT value.
func authCookie(value string) *http.Cookie {
	return &http.Cookie{Name: authCookieName, Value: value}
}

// TestAuthz_CookieAuthentication verifies the cookie path feeds the identical
// HS256/exp/sub verification as the Bearer path: a valid admin cookie passes,
// and the same invalid/tampered tokens are rejected regardless of transport
// (D-22).
func TestAuthz_CookieAuthentication(t *testing.T) {
	setAuthzEnv(t)

	tests := []struct {
		name     string
		cookie   func(t *testing.T) *http.Cookie
		wantCode int
	}{
		{
			name:     "valid admin cookie",
			cookie:   func(t *testing.T) *http.Cookie { return authCookie(signToken(t, testJWTSecret, adminClaims())) },
			wantCode: http.StatusOK,
		},
		{
			name:     "empty cookie value falls through to missing token",
			cookie:   func(*testing.T) *http.Cookie { return authCookie("") },
			wantCode: http.StatusUnauthorized,
		},
		{
			name:     "malformed cookie token",
			cookie:   func(*testing.T) *http.Cookie { return authCookie("not.a.token") },
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "expired cookie token",
			cookie: func(t *testing.T) *http.Cookie {
				claims := adminClaims()
				claims["exp"] = time.Now().Add(-1 * time.Hour).Unix()
				return authCookie(signToken(t, testJWTSecret, claims))
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "cookie signed with wrong secret",
			cookie: func(t *testing.T) *http.Cookie {
				return authCookie(signToken(t, "attacker-controlled-secret-32-chars!!", adminClaims()))
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "cookie alg none",
			cookie: func(t *testing.T) *http.Cookie {
				signed, err := jwt.NewWithClaims(jwt.SigningMethodNone, adminClaims()).
					SignedString(jwt.UnsafeAllowNoneSignatureType)
				assert.NoError(t, err)
				return authCookie(signed)
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "cookie tampered sub without re-signing",
			cookie: func(t *testing.T) *http.Cookie {
				claims := adminClaims()
				claims["sub"] = "friend@example.com"
				legit := signToken(t, testJWTSecret, claims)
				return authCookie(tamperSub(t, legit, testAdminUser))
			},
			wantCode: http.StatusUnauthorized,
		},
		{
			name: "cookie valid signature but non-admin sub",
			cookie: func(t *testing.T) *http.Cookie {
				claims := adminClaims()
				claims["sub"] = "friend@example.com"
				return authCookie(signToken(t, testJWTSecret, claims))
			},
			wantCode: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := Authz(okHandler())
			req := httptest.NewRequest(http.MethodGet, "/articles", nil)
			req.AddCookie(tt.cookie(t))
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}

// TestAuthz_TokenPrecedence verifies the cookie takes precedence over the
// Bearer header when both are present, and that a missing/empty cookie falls
// back to the Bearer header (D-22 precedence: cookie -> Bearer).
func TestAuthz_TokenPrecedence(t *testing.T) {
	setAuthzEnv(t)

	validAdmin := signToken(t, testJWTSecret, adminClaims())
	invalid := "not.a.token"

	tests := []struct {
		name        string
		cookieValue string // "" => no cookie added
		setCookie   bool
		bearer      string // "" => no Authorization header
		wantCode    int
	}{
		{
			name:        "valid cookie, no bearer",
			cookieValue: validAdmin, setCookie: true,
			wantCode: http.StatusOK,
		},
		{
			name:     "no cookie, valid bearer (fallback)",
			bearer:   "Bearer " + validAdmin,
			wantCode: http.StatusOK,
		},
		{
			name:        "valid cookie wins over invalid bearer",
			cookieValue: validAdmin, setCookie: true,
			bearer:   "Bearer " + invalid,
			wantCode: http.StatusOK,
		},
		{
			name:        "invalid cookie is used, not the valid bearer",
			cookieValue: invalid, setCookie: true,
			bearer:   "Bearer " + validAdmin,
			wantCode: http.StatusUnauthorized,
		},
		{
			name:        "empty cookie falls back to valid bearer",
			cookieValue: "", setCookie: true,
			bearer:   "Bearer " + validAdmin,
			wantCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := Authz(okHandler())
			req := httptest.NewRequest(http.MethodGet, "/articles", nil)
			if tt.setCookie {
				req.AddCookie(authCookie(tt.cookieValue))
			}
			if tt.bearer != "" {
				req.Header.Set("Authorization", tt.bearer)
			}
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			assert.Equal(t, tt.wantCode, rec.Code)
		})
	}
}
