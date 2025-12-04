package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/* ───────── Integration Tests for CORS Middleware ───────── */

// TestCORS_Integration_FullAuthFlow tests CORS with authentication flow
func TestCORS_Integration_FullAuthFlow(t *testing.T) {
	// Setup: Create CORS config with allowed origin
	validator := NewWhitelistValidator([]string{
		"http://localhost:3001",
	})

	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           86400,
		Validator:        validator,
		Logger:           &NoOpLogger{},
	}

	// Create a mock auth handler that checks for Authorization header
	authHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/token" && r.Method == "POST" {
			// Mock login endpoint
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"mock-jwt-token"}`)) //nolint:errcheck
			return
		}

		// Protected endpoint - check for Authorization header
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || !strings.HasPrefix(authHeader, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`)) //nolint:errcheck
			return
		}

		// Authorized
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"protected-data"}`)) //nolint:errcheck
	})

	// Apply CORS middleware
	handler := CORS(config)(authHandler)

	// Test 1: Preflight OPTIONS to /auth/token
	t.Run("preflight to auth endpoint", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/auth/token", nil)
		req.Header.Set("Origin", "http://localhost:3001")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "Content-Type")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Verify preflight response
		if rec.Code != http.StatusNoContent {
			t.Errorf("Expected status %d, got %d", http.StatusNoContent, rec.Code)
		}

		if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3001" {
			t.Errorf("Expected Access-Control-Allow-Origin 'http://localhost:3001', got %q", origin)
		}

		if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "POST") {
			t.Error("Expected Access-Control-Allow-Methods to contain POST")
		}
	})

	// Test 2: Actual POST to /auth/token (login)
	t.Run("login with CORS", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/auth/token", strings.NewReader(`{"email":"test@example.com","password":"password"}`))
		req.Header.Set("Origin", "http://localhost:3001")
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Verify response
		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}

		// Verify CORS headers
		if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3001" {
			t.Errorf("Expected Access-Control-Allow-Origin, got %q", origin)
		}

		// Verify response contains token
		body := rec.Body.String()
		if !strings.Contains(body, "mock-jwt-token") {
			t.Errorf("Expected token in response, got: %s", body)
		}
	})

	// Test 3: Preflight OPTIONS to protected endpoint
	t.Run("preflight to protected endpoint", func(t *testing.T) {
		req := httptest.NewRequest("OPTIONS", "/api/protected", nil)
		req.Header.Set("Origin", "http://localhost:3001")
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", "Authorization")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Verify preflight response
		if rec.Code != http.StatusNoContent {
			t.Errorf("Expected status %d, got %d", http.StatusNoContent, rec.Code)
		}

		if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
			t.Error("Expected Access-Control-Allow-Headers to contain Authorization")
		}
	})

	// Test 4: Actual GET to protected endpoint with Bearer token
	t.Run("protected request with token and CORS", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/protected", nil)
		req.Header.Set("Origin", "http://localhost:3001")
		req.Header.Set("Authorization", "Bearer mock-jwt-token")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Verify response
		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
		}

		// Verify CORS headers
		if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3001" {
			t.Errorf("Expected Access-Control-Allow-Origin, got %q", origin)
		}

		// Verify response contains protected data
		body := rec.Body.String()
		if !strings.Contains(body, "protected-data") {
			t.Errorf("Expected protected data in response, got: %s", body)
		}
	})

	// Test 5: Request from disallowed origin should fail CORS check
	t.Run("disallowed origin", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/protected", nil)
		req.Header.Set("Origin", "http://malicious.com")
		req.Header.Set("Authorization", "Bearer mock-jwt-token")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Verify no CORS headers
		if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "" {
			t.Errorf("Expected no CORS headers for disallowed origin, got %q", origin)
		}

		// Handler still processes request (browser blocks response)
		if rec.Code != http.StatusOK {
			t.Errorf("Expected status %d (handler still runs), got %d", http.StatusOK, rec.Code)
		}
	})
}

// TestCORS_Integration_MiddlewareChain tests CORS works with existing middleware
func TestCORS_Integration_MiddlewareChain(t *testing.T) {
	// Setup: Create multiple middleware layers
	validator := NewWhitelistValidator([]string{"http://localhost:3001"})
	corsConfig := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator:        validator,
		Logger:           &NoOpLogger{},
	}

	// Mock middleware that adds X-Request-ID
	requestIDMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Request-ID", "test-request-id-123")
			next.ServeHTTP(w, r)
		})
	}

	// Mock middleware that adds custom header
	customMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Custom-Header", "custom-value")
			next.ServeHTTP(w, r)
		})
	}

	// Final handler
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success")) //nolint:errcheck
	})

	// Apply middleware chain: CORS → RequestID → Custom → Handler
	handler := CORS(corsConfig)(requestIDMiddleware(customMiddleware(finalHandler)))

	// Test: Request with CORS
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:3001")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify CORS headers are present
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3001" {
		t.Errorf("Expected Access-Control-Allow-Origin, got %q", origin)
	}

	// Verify other middleware headers are also present
	if requestID := rec.Header().Get("X-Request-ID"); requestID != "test-request-id-123" {
		t.Errorf("Expected X-Request-ID from middleware, got %q", requestID)
	}

	if custom := rec.Header().Get("X-Custom-Header"); custom != "custom-value" {
		t.Errorf("Expected X-Custom-Header from middleware, got %q", custom)
	}

	// Verify response body
	if body := rec.Body.String(); body != "success" {
		t.Errorf("Expected body 'success', got %q", body)
	}
}

// TestCORS_Integration_MultipleOrigins tests CORS with multiple allowed origins
func TestCORS_Integration_MultipleOrigins(t *testing.T) {
	// Setup: Allow multiple origins
	validator := NewWhitelistValidator([]string{
		"http://localhost:3000",
		"http://localhost:3001",
		"https://example.com",
	})

	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator:        validator,
		Logger:           &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	testCases := []struct {
		origin   string
		expected bool
	}{
		{"http://localhost:3000", true},
		{"http://localhost:3001", true},
		{"https://example.com", true},
		{"http://localhost:3002", false},
		{"https://malicious.com", false},
	}

	for _, tc := range testCases {
		t.Run(tc.origin, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/test", nil)
			req.Header.Set("Origin", tc.origin)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			allowedOrigin := rec.Header().Get("Access-Control-Allow-Origin")

			if tc.expected {
				if allowedOrigin != tc.origin {
					t.Errorf("Expected Access-Control-Allow-Origin %q, got %q", tc.origin, allowedOrigin)
				}
			} else {
				if allowedOrigin != "" {
					t.Errorf("Expected no CORS headers for %q, got %q", tc.origin, allowedOrigin)
				}
			}
		})
	}
}

// TestCORS_Integration_PreflightCaching tests preflight cache behavior
func TestCORS_Integration_PreflightCaching(t *testing.T) {
	validator := NewWhitelistValidator([]string{"http://localhost:3001"})
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST", "PUT"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           86400, // 24 hours
		Validator:        validator,
		Logger:           &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send preflight request
	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	req.Header.Set("Access-Control-Request-Method", "POST")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify Max-Age header
	maxAge := rec.Header().Get("Access-Control-Max-Age")
	if maxAge != "86400" {
		t.Errorf("Expected Access-Control-Max-Age '86400', got %q", maxAge)
	}

	// In a real browser, subsequent requests within MaxAge duration
	// would skip preflight. We verify the server sets the correct header.
}

// TestCORS_Integration_ComplexHeaders tests CORS with complex header scenarios
func TestCORS_Integration_ComplexHeaders(t *testing.T) {
	validator := NewWhitelistValidator([]string{"http://localhost:3001"})
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-Request-ID", "X-Custom-Header"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator:        validator,
		Logger:           &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back the headers received
		w.Header().Set("X-Received-Headers", r.Header.Get("X-Custom-Header"))
		w.WriteHeader(http.StatusOK)
	}))

	// Test: Request with custom headers
	req := httptest.NewRequest("POST", "/api/test", strings.NewReader(`{"data":"test"}`))
	req.Header.Set("Origin", "http://localhost:3001")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("X-Custom-Header", "custom-value")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify CORS headers
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3001" {
		t.Errorf("Expected Access-Control-Allow-Origin, got %q", origin)
	}

	// Verify custom header was received by handler
	if received := rec.Header().Get("X-Received-Headers"); received != "custom-value" {
		t.Errorf("Expected custom header to be received, got %q", received)
	}
}

// TestCORS_Integration_ErrorHandling tests CORS with error responses
func TestCORS_Integration_ErrorHandling(t *testing.T) {
	validator := NewWhitelistValidator([]string{"http://localhost:3001"})
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator:        validator,
		Logger:           &NoOpLogger{},
	}

	// Handler that returns various error status codes
	errorHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/not-found":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`)) //nolint:errcheck
		case "/unauthorized":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`)) //nolint:errcheck
		case "/server-error":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal server error"}`)) //nolint:errcheck
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	handler := CORS(config)(errorHandler)

	testCases := []struct {
		path           string
		expectedStatus int
	}{
		{"/not-found", http.StatusNotFound},
		{"/unauthorized", http.StatusUnauthorized},
		{"/server-error", http.StatusInternalServerError},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			req.Header.Set("Origin", "http://localhost:3001")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Verify error status code
			if rec.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, rec.Code)
			}

			// Verify CORS headers are still present on error responses
			if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3001" {
				t.Errorf("Expected CORS headers on error response, got %q", origin)
			}
		})
	}
}

// TestCORS_Integration_DifferentContentTypes tests CORS with various content types
func TestCORS_Integration_DifferentContentTypes(t *testing.T) {
	validator := NewWhitelistValidator([]string{"http://localhost:3001"})
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator:        validator,
		Logger:           &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType := r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success")) //nolint:errcheck
	}))

	contentTypes := []string{
		"application/json",
		"application/x-www-form-urlencoded",
		"text/plain",
		"multipart/form-data",
	}

	for _, ct := range contentTypes {
		t.Run(ct, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/test", strings.NewReader("data"))
			req.Header.Set("Origin", "http://localhost:3001")
			req.Header.Set("Content-Type", ct)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Verify CORS headers
			if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3001" {
				t.Errorf("Expected CORS headers for content-type %s, got %q", ct, origin)
			}

			// Verify content type was received
			if receivedCT := rec.Header().Get("Content-Type"); receivedCT != ct {
				t.Errorf("Expected content-type %s, got %s", ct, receivedCT)
			}
		})
	}
}

// TestCORS_Integration_IPv6Origin tests CORS with IPv6 addresses
func TestCORS_Integration_IPv6Origin(t *testing.T) {
	validator := NewWhitelistValidator([]string{
		"http://[::1]:8080",
		"https://[2001:db8::1]:443",
	})

	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator:        validator,
		Logger:           &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	testCases := []struct {
		origin   string
		expected bool
	}{
		{"http://[::1]:8080", true},
		{"https://[2001:db8::1]:443", true},
		{"http://[::1]:9000", false}, // Different port
	}

	for _, tc := range testCases {
		t.Run(tc.origin, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/test", nil)
			req.Header.Set("Origin", tc.origin)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			allowedOrigin := rec.Header().Get("Access-Control-Allow-Origin")

			if tc.expected {
				if allowedOrigin != tc.origin {
					t.Errorf("Expected Access-Control-Allow-Origin %q, got %q", tc.origin, allowedOrigin)
				}
			} else {
				if allowedOrigin != "" {
					t.Errorf("Expected no CORS headers for %q, got %q", tc.origin, allowedOrigin)
				}
			}
		})
	}
}
