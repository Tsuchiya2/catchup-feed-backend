package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testSetupEnv sets up environment variables for testing and returns a cleanup function
func testSetupEnv(t *testing.T) func() {
	t.Helper()
	if err := os.Setenv("JWT_SECRET", "test-secret-key-at-least-32-characters-long-for-testing"); err != nil {
		t.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	return func() {
		if err := os.Unsetenv("JWT_SECRET"); err != nil {
			t.Errorf("Failed to unset JWT_SECRET: %v", err)
		}
	}
}

// testSuccessHandler returns a simple test handler that writes "success"
func testSuccessHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("success")); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}
}

// TestAuthz_PublicEndpoints verifies that public endpoints are accessible without JWT tokens.
func TestAuthz_PublicEndpoints(t *testing.T) {
	// Setup
	cleanup := testSetupEnv(t)
	defer cleanup()

	publicEndpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"health check", "GET", "/health"},
		{"readiness probe", "GET", "/ready"},
		{"liveness probe", "GET", "/live"},
		{"metrics endpoint", "GET", "/metrics"},
		{"swagger ui", "GET", "/swagger/"},
		{"swagger doc", "GET", "/swagger/index.html"},
		{"auth token", "POST", "/auth/token"},
	}

	middleware := Authz(testSuccessHandler(t))

	for _, tt := range publicEndpoints {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d for public endpoint %s, got %d",
					http.StatusOK, tt.path, rec.Code)
			}

			if rec.Body.String() != "success" {
				t.Errorf("Expected body 'success' for public endpoint %s, got %q",
					tt.path, rec.Body.String())
			}
		})
	}
}

// TestAuthz_ProtectedEndpoints_WithoutToken verifies that protected endpoints
// return 401 Unauthorized when no JWT token is provided.
func TestAuthz_ProtectedEndpoints_WithoutToken(t *testing.T) {
	// Setup
	cleanup := testSetupEnv(t)
	defer cleanup()

	protectedEndpoints := []struct {
		name   string
		method string
		path   string
	}{
		// Articles endpoints
		{"GET articles list", "GET", "/articles"},
		{"GET articles search", "GET", "/articles/search"},
		{"POST articles", "POST", "/articles"},
		{"PUT articles", "PUT", "/articles/123"},
		{"DELETE articles", "DELETE", "/articles/123"},

		// Sources endpoints
		{"GET sources list", "GET", "/sources"},
		{"GET sources search", "GET", "/sources/search"},
		{"POST sources", "POST", "/sources"},
		{"PUT sources", "PUT", "/sources/123"},
		{"DELETE sources", "DELETE", "/sources/123"},
	}

	middleware := Authz(testSuccessHandler(t))

	for _, tt := range protectedEndpoints {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("Expected status %d for protected endpoint %s %s without token, got %d",
					http.StatusUnauthorized, tt.method, tt.path, rec.Code)
			}
		})
	}
}

// TestAuthz_ProtectedEndpoints_WithInvalidToken verifies that protected endpoints
// return 401 Unauthorized when an invalid JWT token is provided.
func TestAuthz_ProtectedEndpoints_WithInvalidToken(t *testing.T) {
	// Setup
	cleanup := testSetupEnv(t)
	defer cleanup()

	invalidTokens := []struct {
		name  string
		token string
	}{
		{"missing bearer prefix", "invalid-token"},
		{"bearer without token", "Bearer "},
		{"malformed token", "Bearer not.a.valid.token"},
		{"empty bearer", "Bearer"},
	}

	middleware := Authz(testSuccessHandler(t))

	for _, tt := range invalidTokens {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/articles", nil)
			req.Header.Set("Authorization", tt.token)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("Expected status %d for invalid token, got %d",
					http.StatusUnauthorized, rec.Code)
			}
		})
	}
}

// TestAuthz_ProtectedEndpoints_WithExpiredToken verifies that protected endpoints
// return 401 Unauthorized when an expired JWT token is provided.
func TestAuthz_ProtectedEndpoints_WithExpiredToken(t *testing.T) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		t.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("JWT_SECRET"); err != nil {
			t.Errorf("Failed to unset JWT_SECRET: %v", err)
		}
	}()

	// Create expired token (expired 1 hour ago)
	claims := jwt.MapClaims{
		"sub":  "admin",
		"role": "admin",
		"exp":  time.Now().Add(-1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("Failed to create test token: %v", err)
	}

	middleware := Authz(testSuccessHandler(t))

	req := httptest.NewRequest("GET", "/articles", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d for expired token, got %d",
			http.StatusUnauthorized, rec.Code)
	}
}

// TestAuthz_ProtectedEndpoints_WithNonAdminRole verifies that protected endpoints
// return 403 Forbidden when a valid JWT token with non-admin role is provided.
func TestAuthz_ProtectedEndpoints_WithNonAdminRole(t *testing.T) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		t.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("JWT_SECRET"); err != nil {
			t.Errorf("Failed to unset JWT_SECRET: %v", err)
		}
	}()

	// Create valid token with non-admin role
	claims := jwt.MapClaims{
		"sub":  "user",
		"role": "user", // Non-admin role
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("Failed to create test token: %v", err)
	}

	middleware := Authz(testSuccessHandler(t))

	req := httptest.NewRequest("GET", "/articles", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status %d for non-admin role, got %d",
			http.StatusForbidden, rec.Code)
	}
}

// TestAuthz_ProtectedEndpoints_WithValidToken verifies that protected endpoints
// are accessible with a valid JWT token.
func TestAuthz_ProtectedEndpoints_WithValidToken(t *testing.T) {
	// Setup
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	if err := os.Setenv("JWT_SECRET", secret); err != nil {
		t.Fatalf("Failed to set JWT_SECRET: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("JWT_SECRET"); err != nil {
			t.Errorf("Failed to unset JWT_SECRET: %v", err)
		}
	}()

	// Create valid admin token
	claims := jwt.MapClaims{
		"sub":  "admin",
		"role": "admin",
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("Failed to create test token: %v", err)
	}

	protectedEndpoints := []struct {
		name   string
		method string
		path   string
	}{
		// Articles endpoints - ALL methods including GET
		{"GET articles list", "GET", "/articles"},
		{"GET articles search", "GET", "/articles/search"},
		{"POST articles", "POST", "/articles"},
		{"PUT articles", "PUT", "/articles/123"},
		{"DELETE articles", "DELETE", "/articles/123"},

		// Sources endpoints - ALL methods including GET
		{"GET sources list", "GET", "/sources"},
		{"GET sources search", "GET", "/sources/search"},
		{"POST sources", "POST", "/sources"},
		{"PUT sources", "PUT", "/sources/123"},
		{"DELETE sources", "DELETE", "/sources/123"},
	}

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify user is in context
		user := r.Context().Value(ctxUser)
		if user == nil {
			t.Error("Expected user in context, got nil")
		}
		if user != "admin" {
			t.Errorf("Expected user 'admin' in context, got %v", user)
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("success")); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	})

	middleware := Authz(testHandler)

	for _, tt := range protectedEndpoints {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Authorization", "Bearer "+tokenString)
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d for %s %s with valid token, got %d",
					http.StatusOK, tt.method, tt.path, rec.Code)
			}

			if rec.Body.String() != "success" {
				t.Errorf("Expected body 'success' for %s %s, got %q",
					tt.method, tt.path, rec.Body.String())
			}
		})
	}
}

// TestAuthz_GET_RequiresAuthentication is a focused test specifically for
// CVE-CATCHUP-2024-002: Authorization Bypass for GET Requests.
//
// This test verifies that GET requests to protected endpoints now require
// authentication, fixing the security vulnerability where GET requests
// bypassed JWT validation.
func TestAuthz_GET_RequiresAuthentication(t *testing.T) {
	// Setup
	cleanup := testSetupEnv(t)
	defer cleanup()

	middleware := Authz(testSuccessHandler(t))

	tests := []struct {
		name         string
		path         string
		withAuth     bool
		expectedCode int
	}{
		// Without authentication - should fail
		{"GET articles without auth", "/articles", false, http.StatusUnauthorized},
		{"GET articles/search without auth", "/articles/search", false, http.StatusUnauthorized},
		{"GET sources without auth", "/sources", false, http.StatusUnauthorized},
		{"GET sources/search without auth", "/sources/search", false, http.StatusUnauthorized},

		// With authentication - should succeed
		{"GET articles with auth", "/articles", true, http.StatusOK},
		{"GET articles/search with auth", "/articles/search", true, http.StatusOK},
		{"GET sources with auth", "/sources", true, http.StatusOK},
		{"GET sources/search with auth", "/sources/search", true, http.StatusOK},

		// Public endpoints - should succeed without auth
		{"GET health without auth", "/health", false, http.StatusOK},
		{"GET metrics without auth", "/metrics", false, http.StatusOK},
	}

	// Create valid admin token for authenticated tests
	secret := "test-secret-key-at-least-32-characters-long-for-testing"
	claims := jwt.MapClaims{
		"sub":  "admin",
		"role": "admin",
		"exp":  time.Now().Add(1 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("Failed to create test token: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			if tt.withAuth {
				req.Header.Set("Authorization", "Bearer "+tokenString)
			}
			rec := httptest.NewRecorder()

			middleware.ServeHTTP(rec, req)

			if rec.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d", tt.expectedCode, rec.Code)
			}
		})
	}
}

// TestIsPublicEndpoint verifies the IsPublicEndpoint function correctly
// identifies public and protected endpoints.
func TestIsPublicEndpoint(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		public bool
	}{
		// Public endpoints
		{"health check", "/health", true},
		{"readiness probe", "/ready", true},
		{"liveness probe", "/live", true},
		{"metrics", "/metrics", true},
		{"swagger root", "/swagger/", true},
		{"swagger doc", "/swagger/index.html", true},
		{"swagger resource", "/swagger/swagger-ui.css", true},
		{"auth token", "/auth/token", true},

		// Protected endpoints
		{"articles list", "/articles", false},
		{"articles search", "/articles/search", false},
		{"article detail", "/articles/123", false},
		{"sources list", "/sources", false},
		{"sources search", "/sources/search", false},
		{"source detail", "/sources/123", false},

		// Edge cases
		{"root path", "/", false},
		{"unknown path", "/unknown", false},
		{"admin path", "/admin", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPublicEndpoint(tt.path)
			if result != tt.public {
				t.Errorf("IsPublicEndpoint(%q) = %v, want %v", tt.path, result, tt.public)
			}
		})
	}
}
