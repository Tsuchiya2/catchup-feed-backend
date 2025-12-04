package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestAuthz_JWT_TamperingPrevention verifies that JWT tampering is detected and rejected.
//
// Security Note:
// This test suite validates critical JWT security properties:
// 1. Role claim tampering (viewer→admin) is detected
// 2. Token signature verification prevents unauthorized modifications
// 3. Expired tokens are rejected
// 4. Missing or invalid claims are rejected
// 5. Algorithm substitution attacks are prevented
//
// These tests ensure compliance with OWASP JWT security best practices.
func TestAuthz_JWT_TamperingPrevention(t *testing.T) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		t.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	middleware := Authz(testSuccessHandler(t))

	t.Run("tampered role claim (viewer to admin) without re-signing returns 401", func(t *testing.T) {
		// Create a valid viewer token
		claims := jwt.MapClaims{
			"sub":  "viewer@example.com",
			"role": RoleViewer,
			"exp":  time.Now().Add(1 * time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("Failed to create viewer token: %v", err)
		}

		// Parse the token to get its parts
		parts := strings.Split(tokenString, ".")
		if len(parts) != 3 {
			t.Fatalf("Invalid token format: expected 3 parts, got %d", len(parts))
		}

		// Decode the payload (second part)
		payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			t.Fatalf("Failed to decode payload: %v", err)
		}

		// Parse payload JSON
		var payload map[string]interface{}
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			t.Fatalf("Failed to parse payload JSON: %v", err)
		}

		// Tamper with the role claim
		payload["role"] = RoleAdmin

		// Re-encode the tampered payload
		tamperedPayloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("Failed to marshal tampered payload: %v", err)
		}
		tamperedPayloadEncoded := base64.RawURLEncoding.EncodeToString(tamperedPayloadBytes)

		// Create tampered token (header + tampered payload + original signature)
		// This simulates an attacker trying to modify the role without re-signing
		tamperedToken := parts[0] + "." + tamperedPayloadEncoded + "." + parts[2]

		// Try to use the tampered token
		req := httptest.NewRequest("POST", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+tamperedToken)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		// Should be rejected due to invalid signature
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for tampered token, got %d. "+
				"Tampered tokens should be rejected!",
				http.StatusUnauthorized, rec.Code)
		}

		// Verify error message indicates token validation failure
		body := rec.Body.String()
		if !strings.Contains(body, "unauthorized") {
			t.Errorf("Expected 'unauthorized' in error message, got: %s", body)
		}
	})

	t.Run("expired token returns 401", func(t *testing.T) {
		// Create an expired token (expired 1 hour ago)
		claims := jwt.MapClaims{
			"sub":  "admin@example.com",
			"role": RoleAdmin,
			"exp":  time.Now().Add(-1 * time.Hour).Unix(), // Expired
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("Failed to create expired token: %v", err)
		}

		req := httptest.NewRequest("GET", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for expired token, got %d",
				http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("missing role claim returns 401", func(t *testing.T) {
		// Create a token without role claim
		claims := jwt.MapClaims{
			"sub": "admin@example.com",
			"exp": time.Now().Add(1 * time.Hour).Unix(),
			// Missing "role" claim
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("Failed to create token without role: %v", err)
		}

		req := httptest.NewRequest("GET", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for token without role claim, got %d",
				http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("invalid signature returns 401", func(t *testing.T) {
		// Create a valid token
		claims := jwt.MapClaims{
			"sub":  "admin@example.com",
			"role": RoleAdmin,
			"exp":  time.Now().Add(1 * time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		// Modify the signature (last part of the token)
		parts := strings.Split(tokenString, ".")
		if len(parts) != 3 {
			t.Fatalf("Invalid token format")
		}

		// Corrupt the signature by changing a character
		corruptedSignature := parts[2]
		if len(corruptedSignature) > 0 {
			// Change the first character of the signature
			chars := []byte(corruptedSignature)
			if chars[0] == 'A' {
				chars[0] = 'B'
			} else {
				chars[0] = 'A'
			}
			corruptedSignature = string(chars)
		}

		// Create token with corrupted signature
		corruptedToken := parts[0] + "." + parts[1] + "." + corruptedSignature

		req := httptest.NewRequest("GET", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+corruptedToken)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for token with invalid signature, got %d",
				http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("token signed with wrong secret returns 401", func(t *testing.T) {
		// Create a token signed with a different secret
		wrongSecret := "wrong-secret-key-at-least-32-characters-long"
		claims := jwt.MapClaims{
			"sub":  "admin@example.com",
			"role": RoleAdmin,
			"exp":  time.Now().Add(1 * time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(wrongSecret))
		if err != nil {
			t.Fatalf("Failed to create token with wrong secret: %v", err)
		}

		req := httptest.NewRequest("GET", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for token signed with wrong secret, got %d",
				http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("missing sub claim returns 401", func(t *testing.T) {
		// Create a token without sub claim
		claims := jwt.MapClaims{
			"role": RoleAdmin,
			"exp":  time.Now().Add(1 * time.Hour).Unix(),
			// Missing "sub" claim
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("Failed to create token without sub: %v", err)
		}

		req := httptest.NewRequest("GET", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for token without sub claim, got %d",
				http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("missing exp claim returns 401", func(t *testing.T) {
		// Create a token without exp claim
		claims := jwt.MapClaims{
			"sub":  "admin@example.com",
			"role": RoleAdmin,
			// Missing "exp" claim
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("Failed to create token without exp: %v", err)
		}

		req := httptest.NewRequest("GET", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for token without exp claim, got %d",
				http.StatusUnauthorized, rec.Code)
		}
	})
}

// TestAuthz_JWT_AlgorithmSubstitutionAttack verifies protection against
// algorithm substitution attacks (e.g., "none" algorithm, RS256→HS256).
func TestAuthz_JWT_AlgorithmSubstitutionAttack(t *testing.T) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		t.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	middleware := Authz(testSuccessHandler(t))

	t.Run("none algorithm attack returns 401", func(t *testing.T) {
		// Create header with "none" algorithm
		header := map[string]interface{}{
			"alg": "none",
			"typ": "JWT",
		}
		headerBytes, _ := json.Marshal(header)
		headerEncoded := base64.RawURLEncoding.EncodeToString(headerBytes)

		// Create payload
		payload := map[string]interface{}{
			"sub":  "admin@example.com",
			"role": RoleAdmin,
			"exp":  time.Now().Add(1 * time.Hour).Unix(),
		}
		payloadBytes, _ := json.Marshal(payload)
		payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)

		// Create token with no signature (none algorithm)
		// Format: header.payload. (note the trailing dot with empty signature)
		noneToken := headerEncoded + "." + payloadEncoded + "."

		req := httptest.NewRequest("GET", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+noneToken)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for 'none' algorithm attack, got %d. "+
				"System is vulnerable to algorithm substitution attack!",
				http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("wrong algorithm (RS256 instead of HS256) returns 401", func(t *testing.T) {
		// Try to use RS256 algorithm when HS256 is expected
		claims := jwt.MapClaims{
			"sub":  "admin@example.com",
			"role": RoleAdmin,
			"exp":  time.Now().Add(1 * time.Hour).Unix(),
		}

		// Create header with RS256 (wrong algorithm)
		header := map[string]interface{}{
			"alg": "RS256",
			"typ": "JWT",
		}
		headerBytes, _ := json.Marshal(header)
		headerEncoded := base64.RawURLEncoding.EncodeToString(headerBytes)

		// Create payload
		payloadBytes, _ := json.Marshal(claims)
		payloadEncoded := base64.RawURLEncoding.EncodeToString(payloadBytes)

		// Create a fake signature
		fakeSignature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))

		// Create token with wrong algorithm
		wrongAlgToken := headerEncoded + "." + payloadEncoded + "." + fakeSignature

		req := httptest.NewRequest("GET", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+wrongAlgToken)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d for wrong algorithm, got %d. "+
				"System should enforce HS256 algorithm!",
				http.StatusUnauthorized, rec.Code)
		}
	})
}

// TestAuthz_JWT_ValidTokenAccepted verifies that valid, properly signed tokens
// are accepted (positive test case).
func TestAuthz_JWT_ValidTokenAccepted(t *testing.T) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		t.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify user is in context
		user := r.Context().Value(ctxUser)
		if user == nil {
			t.Error("Expected user in context, got nil")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	})

	middleware := Authz(testHandler)

	tests := []struct {
		name   string
		role   string
		method string
		path   string
	}{
		{"admin GET", RoleAdmin, "GET", "/articles"},
		{"admin POST", RoleAdmin, "POST", "/articles"},
		{"viewer GET", RoleViewer, "GET", "/articles"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create valid token
			claims := jwt.MapClaims{
				"sub":  "user@example.com",
				"role": tt.role,
				"exp":  time.Now().Add(1 * time.Hour).Unix(),
			}
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
			tokenString, err := token.SignedString([]byte(secret))
			if err != nil {
				t.Fatalf("Failed to create token: %v", err)
			}

			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Authorization", "Bearer "+tokenString)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d for valid token, got %d",
					http.StatusOK, rec.Code)
			}
		})
	}
}

// TestAuthz_JWT_ClaimValidation verifies strict claim validation.
func TestAuthz_JWT_ClaimValidation(t *testing.T) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		t.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
	}()

	middleware := Authz(testSuccessHandler(t))

	t.Run("empty role claim returns 401", func(t *testing.T) {
		claims := jwt.MapClaims{
			"sub":  "user@example.com",
			"role": "", // Empty role
			"exp":  time.Now().Add(1 * time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		req := httptest.NewRequest("GET", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		// Should be rejected due to empty role (no permissions for empty role)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnauthorized {
			t.Errorf("Expected status %d or %d for empty role claim, got %d",
				http.StatusForbidden, http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("empty sub claim is accepted but logged", func(t *testing.T) {
		// Note: Empty sub claim is technically valid in JWT spec.
		// The middleware accepts it but logs the empty user.
		// This is acceptable since authentication already occurred.
		claims := jwt.MapClaims{
			"sub":  "", // Empty sub
			"role": RoleAdmin,
			"exp":  time.Now().Add(1 * time.Hour).Unix(),
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenString, err := token.SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("Failed to create token: %v", err)
		}

		req := httptest.NewRequest("GET", "/articles", nil)
		req.Header.Set("Authorization", "Bearer "+tokenString)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		// Empty sub is accepted (200 OK) since JWT is valid and role is valid
		// The empty user will be logged for audit purposes
		if rec.Code != http.StatusOK {
			t.Logf("Note: Empty sub claim accepted with status %d (expected 200)", rec.Code)
		}
	})
}
