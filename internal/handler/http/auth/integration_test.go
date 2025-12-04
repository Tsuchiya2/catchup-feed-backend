package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authservice "catchup-feed/internal/service/auth"

	"github.com/golang-jwt/jwt/v5"
)

// TestIntegration_ViewerLogin tests the complete viewer login flow.
// Setup: DEMO_USER=demo@example.com, DEMO_USER_PASSWORD=secure-demo-password-123
// POST /auth/token with demo credentials → 200 OK, JWT with role=viewer
func TestIntegration_ViewerLogin(t *testing.T) {
	// Setup environment
	t.Setenv("ADMIN_USER", "admin@example.com")
	t.Setenv("ADMIN_USER_PASSWORD", "secure-admin-password-123")
	t.Setenv("DEMO_USER", "demo@example.com")
	t.Setenv("DEMO_USER_PASSWORD", "secure-demo-password-123")
	t.Setenv("JWT_SECRET", "test-secret-key-for-jwt-signing-32chars")

	// Create provider and service
	provider := NewMultiUserAuthProvider(12, []string{"password", "123456"})
	authSvc := authservice.NewAuthService(provider, []string{"/auth/token"})

	// Create handler
	handler := TokenHandler(authSvc)

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Make request
	body := `{"email":"demo@example.com","password":"secure-demo-password-123"}`
	resp, err := http.Post(server.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Check status code
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Parse response
	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Decode JWT and verify role claim
	token, err := jwt.Parse(tokenResp.Token, func(t *jwt.Token) (interface{}, error) {
		return []byte("test-secret-key-for-jwt-signing-32chars"), nil
	})
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}
	if !token.Valid {
		t.Fatal("token is not valid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("claims type assertion failed")
	}

	// Verify role claim is "viewer"
	if role, ok := claims["role"].(string); !ok || role != RoleViewer {
		t.Errorf("role claim = %v, want %s", claims["role"], RoleViewer)
	}

	// Verify subject claim
	if sub, ok := claims["sub"].(string); !ok || sub != "demo@example.com" {
		t.Errorf("sub claim = %v, want demo@example.com", claims["sub"])
	}
}

// TestIntegration_AdminLogin tests the complete admin login flow.
// Setup: ADMIN_USER=admin@example.com, ADMIN_USER_PASSWORD=secure-admin-password-123
// POST /auth/token with admin credentials → 200 OK, JWT with role=admin
func TestIntegration_AdminLogin(t *testing.T) {
	// Setup environment
	t.Setenv("ADMIN_USER", "admin@example.com")
	t.Setenv("ADMIN_USER_PASSWORD", "secure-admin-password-123")
	t.Setenv("DEMO_USER", "demo@example.com")
	t.Setenv("DEMO_USER_PASSWORD", "secure-demo-password-123")
	t.Setenv("JWT_SECRET", "test-secret-key-for-jwt-signing-32chars")

	// Create provider and service
	provider := NewMultiUserAuthProvider(12, []string{"password", "123456"})
	authSvc := authservice.NewAuthService(provider, []string{"/auth/token"})

	// Create handler
	handler := TokenHandler(authSvc)

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Make request
	body := `{"email":"admin@example.com","password":"secure-admin-password-123"}`
	resp, err := http.Post(server.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Check status code
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Parse response
	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Decode JWT and verify role claim
	token, err := jwt.Parse(tokenResp.Token, func(t *jwt.Token) (interface{}, error) {
		return []byte("test-secret-key-for-jwt-signing-32chars"), nil
	})
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}
	if !token.Valid {
		t.Fatal("token is not valid")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("claims type assertion failed")
	}

	// Verify role claim is "admin"
	if role, ok := claims["role"].(string); !ok || role != RoleAdmin {
		t.Errorf("role claim = %v, want %s", claims["role"], RoleAdmin)
	}

	// Verify subject claim
	if sub, ok := claims["sub"].(string); !ok || sub != "admin@example.com" {
		t.Errorf("sub claim = %v, want admin@example.com", claims["sub"])
	}
}

// TestIntegration_ViewerReadOnlyAccess tests that viewer has read-only access.
// Login as viewer, get token
// GET /articles with viewer token → 200 OK
// GET /sources with viewer token → 200 OK
// POST /sources with viewer token → 403 Forbidden
func TestIntegration_ViewerReadOnlyAccess(t *testing.T) {
	// Setup environment
	t.Setenv("ADMIN_USER", "admin@example.com")
	t.Setenv("ADMIN_USER_PASSWORD", "secure-admin-password-123")
	t.Setenv("DEMO_USER", "demo@example.com")
	t.Setenv("DEMO_USER_PASSWORD", "secure-demo-password-123")
	t.Setenv("JWT_SECRET", "test-secret-key-for-jwt-signing-32chars")

	// Create provider and service
	provider := NewMultiUserAuthProvider(12, []string{"password", "123456"})
	authSvc := authservice.NewAuthService(provider, []string{"/auth/token"})

	// Get viewer token
	tokenHandler := TokenHandler(authSvc)
	tokenServer := httptest.NewServer(tokenHandler)
	defer tokenServer.Close()

	// Login as viewer
	loginBody := `{"email":"demo@example.com","password":"secure-demo-password-123"}`
	loginResp, err := http.Post(tokenServer.URL, "application/json", strings.NewReader(loginBody))
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}
	defer loginResp.Body.Close() //nolint:errcheck

	var tokenResp tokenResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	viewerToken := tokenResp.Token

	// Create a simple handler that returns 200 OK for authenticated requests
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`)) //nolint:errcheck
	})

	// Wrap with Authz middleware
	authzHandler := Authz(mockHandler)
	authzServer := httptest.NewServer(authzHandler)
	defer authzServer.Close()

	// Test GET /articles - should succeed
	t.Run("GET /articles should succeed", func(t *testing.T) {
		req, err := http.NewRequest("GET", authzServer.URL+"/articles", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+viewerToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /articles status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	// Test GET /sources - should succeed
	t.Run("GET /sources should succeed", func(t *testing.T) {
		req, err := http.NewRequest("GET", authzServer.URL+"/sources", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+viewerToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /sources status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	// Test POST /sources - should be forbidden
	t.Run("POST /sources should be forbidden", func(t *testing.T) {
		postBody := `{"url":"https://example.com/feed"}`
		req, err := http.NewRequest("POST", authzServer.URL+"/sources", strings.NewReader(postBody))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("POST /sources status = %d, want %d", resp.StatusCode, http.StatusForbidden)
		}
	})
}

// TestIntegration_AdminFullAccess tests that admin has full access.
// Login as admin, get token
// GET /articles → 200 OK
// POST /sources → should pass auth (may fail for other reasons like missing body)
func TestIntegration_AdminFullAccess(t *testing.T) {
	// Setup environment
	t.Setenv("ADMIN_USER", "admin@example.com")
	t.Setenv("ADMIN_USER_PASSWORD", "secure-admin-password-123")
	t.Setenv("DEMO_USER", "demo@example.com")
	t.Setenv("DEMO_USER_PASSWORD", "secure-demo-password-123")
	t.Setenv("JWT_SECRET", "test-secret-key-for-jwt-signing-32chars")

	// Create provider and service
	provider := NewMultiUserAuthProvider(12, []string{"password", "123456"})
	authSvc := authservice.NewAuthService(provider, []string{"/auth/token"})

	// Get admin token
	tokenHandler := TokenHandler(authSvc)
	tokenServer := httptest.NewServer(tokenHandler)
	defer tokenServer.Close()

	// Login as admin
	loginBody := `{"email":"admin@example.com","password":"secure-admin-password-123"}`
	loginResp, err := http.Post(tokenServer.URL, "application/json", strings.NewReader(loginBody))
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}
	defer loginResp.Body.Close() //nolint:errcheck

	var tokenResp tokenResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	adminToken := tokenResp.Token

	// Create a simple handler that returns 200 OK for authenticated requests
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`)) //nolint:errcheck
	})

	// Wrap with Authz middleware
	authzHandler := Authz(mockHandler)
	authzServer := httptest.NewServer(authzHandler)
	defer authzServer.Close()

	// Test GET /articles - should succeed
	t.Run("GET /articles should succeed", func(t *testing.T) {
		req, err := http.NewRequest("GET", authzServer.URL+"/articles", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET /articles status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	// Test POST /sources - should pass auth
	t.Run("POST /sources should pass auth", func(t *testing.T) {
		postBody := `{"url":"https://example.com/feed"}`
		req, err := http.NewRequest("POST", authzServer.URL+"/sources", strings.NewReader(postBody))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		// Should pass auth (200 OK from our mock handler)
		// In real scenario, it might fail with 400 or other business logic errors,
		// but it should NOT fail with 401/403
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			t.Errorf("POST /sources status = %d, should not be auth error", resp.StatusCode)
		}
	})

	// Test DELETE /sources - should pass auth
	t.Run("DELETE /sources/1 should pass auth", func(t *testing.T) {
		req, err := http.NewRequest("DELETE", authzServer.URL+"/sources/1", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		// Should pass auth
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			t.Errorf("DELETE /sources/1 status = %d, should not be auth error", resp.StatusCode)
		}
	})

	// Test PUT /articles - should pass auth
	t.Run("PUT /articles/1 should pass auth", func(t *testing.T) {
		putBody := `{"title":"Updated Title"}`
		req, err := http.NewRequest("PUT", authzServer.URL+"/articles/1", strings.NewReader(putBody))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		// Should pass auth
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			t.Errorf("PUT /articles/1 status = %d, should not be auth error", resp.StatusCode)
		}
	})
}

// TestIntegration_AdminOnlyDeployment tests admin-only deployment scenario.
// Setup: Only ADMIN_USER set (no DEMO_USER)
// Admin login works
// Demo credentials rejected with 401
func TestIntegration_AdminOnlyDeployment(t *testing.T) {
	// Setup environment - only admin user
	t.Setenv("ADMIN_USER", "admin@example.com")
	t.Setenv("ADMIN_USER_PASSWORD", "secure-admin-password-123")
	// DEMO_USER is NOT set
	t.Setenv("JWT_SECRET", "test-secret-key-for-jwt-signing-32chars")

	// Create provider and service
	provider := NewMultiUserAuthProvider(12, []string{"password", "123456"})
	authSvc := authservice.NewAuthService(provider, []string{"/auth/token"})

	// Create handler
	handler := TokenHandler(authSvc)

	// Create test server
	server := httptest.NewServer(handler)
	defer server.Close()

	// Test 1: Admin login should work
	t.Run("Admin login should succeed", func(t *testing.T) {
		body := `{"email":"admin@example.com","password":"secure-admin-password-123"}`
		resp, err := http.Post(server.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			t.Errorf("admin login status = %d, want %d", resp.StatusCode, http.StatusOK)
		}

		// Verify token contains admin role
		var tokenResp tokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		token, err := jwt.Parse(tokenResp.Token, func(t *jwt.Token) (interface{}, error) {
			return []byte("test-secret-key-for-jwt-signing-32chars"), nil
		})
		if err != nil {
			t.Fatalf("failed to parse token: %v", err)
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			t.Fatal("claims type assertion failed")
		}

		if role, ok := claims["role"].(string); !ok || role != RoleAdmin {
			t.Errorf("role claim = %v, want %s", claims["role"], RoleAdmin)
		}
	})

	// Test 2: Demo credentials should be rejected (no DEMO_USER configured)
	t.Run("Demo login should fail", func(t *testing.T) {
		body := `{"email":"demo@example.com","password":"secure-demo-password-123"}`
		resp, err := http.Post(server.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("demo login status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})

	// Test 3: Invalid credentials should be rejected
	t.Run("Invalid credentials should fail", func(t *testing.T) {
		body := `{"email":"invalid@example.com","password":"wrong-password"}`
		resp, err := http.Post(server.URL, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("invalid login status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
		}
	})
}

// TestIntegration_ViewerCannotAccessAdminEndpoints tests that viewer cannot access admin-only endpoints.
func TestIntegration_ViewerCannotAccessAdminEndpoints(t *testing.T) {
	// Setup environment
	t.Setenv("ADMIN_USER", "admin@example.com")
	t.Setenv("ADMIN_USER_PASSWORD", "secure-admin-password-123")
	t.Setenv("DEMO_USER", "demo@example.com")
	t.Setenv("DEMO_USER_PASSWORD", "secure-demo-password-123")
	t.Setenv("JWT_SECRET", "test-secret-key-for-jwt-signing-32chars")

	// Create provider and service
	provider := NewMultiUserAuthProvider(12, []string{"password", "123456"})
	authSvc := authservice.NewAuthService(provider, []string{"/auth/token"})

	// Get viewer token
	tokenHandler := TokenHandler(authSvc)
	tokenServer := httptest.NewServer(tokenHandler)
	defer tokenServer.Close()

	// Login as viewer
	loginBody := `{"email":"demo@example.com","password":"secure-demo-password-123"}`
	loginResp, err := http.Post(tokenServer.URL, "application/json", strings.NewReader(loginBody))
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}
	defer loginResp.Body.Close() //nolint:errcheck

	var tokenResp tokenResponse
	if err := json.NewDecoder(loginResp.Body).Decode(&tokenResp); err != nil {
		t.Fatalf("failed to decode login response: %v", err)
	}
	viewerToken := tokenResp.Token

	// Create a simple handler
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`)) //nolint:errcheck
	})

	// Wrap with Authz middleware
	authzHandler := Authz(mockHandler)
	authzServer := httptest.NewServer(authzHandler)
	defer authzServer.Close()

	// Test POST /articles - should be forbidden
	t.Run("POST /articles should be forbidden", func(t *testing.T) {
		postBody := `{"title":"New Article"}`
		req, err := http.NewRequest("POST", authzServer.URL+"/articles", strings.NewReader(postBody))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("POST /articles status = %d, want %d", resp.StatusCode, http.StatusForbidden)
		}
	})

	// Test DELETE /sources - should be forbidden
	t.Run("DELETE /sources/1 should be forbidden", func(t *testing.T) {
		req, err := http.NewRequest("DELETE", authzServer.URL+"/sources/1", nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+viewerToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("DELETE /sources/1 status = %d, want %d", resp.StatusCode, http.StatusForbidden)
		}
	})

	// Test PUT /articles - should be forbidden
	t.Run("PUT /articles/1 should be forbidden", func(t *testing.T) {
		putBody := `{"title":"Updated Title"}`
		req, err := http.NewRequest("PUT", authzServer.URL+"/articles/1", strings.NewReader(putBody))
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to make request: %v", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("PUT /articles/1 status = %d, want %d", resp.StatusCode, http.StatusForbidden)
		}
	})
}

// TestIntegration_ExpiredToken tests that expired tokens are rejected.
func TestIntegration_ExpiredToken(t *testing.T) {
	// Setup environment
	t.Setenv("JWT_SECRET", "test-secret-key-for-jwt-signing-32chars")

	// Create a simple handler
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`)) //nolint:errcheck
	})

	// Wrap with Authz middleware
	authzHandler := Authz(mockHandler)
	server := httptest.NewServer(authzHandler)
	defer server.Close()

	// Create an expired token (exp = 0, which is in the past)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  "test@example.com",
		"role": RoleAdmin,
		"exp":  0, // Expired
	})

	signed, err := token.SignedString([]byte("test-secret-key-for-jwt-signing-32chars"))
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	// Make request with expired token
	req, err := http.NewRequest("GET", server.URL+"/articles", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+signed)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Should be unauthorized
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expired token status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestIntegration_MissingToken tests that requests without token are rejected.
func TestIntegration_MissingToken(t *testing.T) {
	// Setup environment
	t.Setenv("JWT_SECRET", "test-secret-key-for-jwt-signing-32chars")

	// Create a simple handler
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`)) //nolint:errcheck
	})

	// Wrap with Authz middleware
	authzHandler := Authz(mockHandler)
	server := httptest.NewServer(authzHandler)
	defer server.Close()

	// Make request without token
	req, err := http.NewRequest("GET", server.URL+"/articles", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Should be unauthorized
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("missing token status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestIntegration_InvalidToken tests that invalid tokens are rejected.
func TestIntegration_InvalidToken(t *testing.T) {
	// Setup environment
	t.Setenv("JWT_SECRET", "test-secret-key-for-jwt-signing-32chars")

	// Create a simple handler
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`)) //nolint:errcheck
	})

	// Wrap with Authz middleware
	authzHandler := Authz(mockHandler)
	server := httptest.NewServer(authzHandler)
	defer server.Close()

	// Make request with invalid token
	req, err := http.NewRequest("GET", server.URL+"/articles", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer invalid.token.here")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to make request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	// Should be unauthorized
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("invalid token status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// TestIntegration_PublicEndpointsNoAuth tests that public endpoints don't require auth.
func TestIntegration_PublicEndpointsNoAuth(t *testing.T) {
	// Setup environment
	t.Setenv("JWT_SECRET", "test-secret-key-for-jwt-signing-32chars")

	// Create a simple handler
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"success"}`)) //nolint:errcheck
	})

	// Wrap with Authz middleware
	authzHandler := Authz(mockHandler)
	server := httptest.NewServer(authzHandler)
	defer server.Close()

	publicEndpoints := []string{
		"/health",
		"/ready",
		"/live",
		"/metrics",
		"/swagger/index.html",
		"/auth/token",
	}

	for _, endpoint := range publicEndpoints {
		t.Run("Public endpoint: "+endpoint, func(t *testing.T) {
			// Make request without token
			req, err := http.NewRequest("GET", server.URL+endpoint, nil)
			if err != nil {
				t.Fatalf("failed to create request: %v", err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("failed to make request: %v", err)
			}
			defer resp.Body.Close() //nolint:errcheck

			// Should succeed without auth
			if resp.StatusCode != http.StatusOK {
				t.Errorf("%s status = %d, want %d", endpoint, resp.StatusCode, http.StatusOK)
			}
		})
	}
}
