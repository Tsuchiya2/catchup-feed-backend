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

// newTestAuthService builds an AuthService backed by the real bcrypt
// provider with test credentials in the environment.
func newTestAuthService(t *testing.T) *authservice.AuthService {
	t.Helper()
	setAdminEnv(t, testAdminUser, testHash(t, testPassword))
	t.Setenv("JWT_SECRET", testJWTSecret)
	return authservice.NewAuthService(NewAdminAuthProvider())
}

func TestTokenHandler(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := TokenHandler(newTestAuthService(t))

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

// TestTokenHandler_IssuedClaims verifies that the issued JWT carries only
// sub/iat/exp — in particular no role claim (C-20: 単一管理者化).
func TestTokenHandler_IssuedClaims(t *testing.T) {
	handler := TokenHandler(newTestAuthService(t))

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
	assert.NotContains(t, claims, "role", "single-admin tokens must not carry a role claim")

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
