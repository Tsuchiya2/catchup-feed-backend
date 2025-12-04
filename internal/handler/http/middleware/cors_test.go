package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// mockOriginValidator is a mock implementation of OriginValidator for testing
type mockOriginValidator struct {
	allowed bool
	origins []string
}

func (m *mockOriginValidator) IsAllowed(origin string) bool {
	return m.allowed
}

func (m *mockOriginValidator) GetAllowedOrigins() []string {
	return m.origins
}

// mockCORSLogger is a mock implementation of CORSLogger for testing
type mockCORSLogger struct {
	infoCount  int
	warnCount  int
	debugCount int
	lastMsg    string
	lastFields map[string]interface{}
}

func (m *mockCORSLogger) Info(msg string, fields map[string]interface{}) {
	m.infoCount++
	m.lastMsg = msg
	m.lastFields = fields
}

func (m *mockCORSLogger) Warn(msg string, fields map[string]interface{}) {
	m.warnCount++
	m.lastMsg = msg
	m.lastFields = fields
}

func (m *mockCORSLogger) Debug(msg string, fields map[string]interface{}) {
	m.debugCount++
	m.lastMsg = msg
	m.lastFields = fields
}

// TestCORS_PreflightRequest_AllowedOrigin tests that
// preflight requests from allowed origins return 204 with correct headers
func TestCORS_PreflightRequest_AllowedOrigin(t *testing.T) {
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST", "PUT"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: true,
			origins: []string{"http://localhost:3000"},
		},
		Logger: &NoOpLogger{},
	}

	nextHandlerCalled := false
	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify status code
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d", http.StatusNoContent, rec.Code)
	}

	// Verify CORS headers
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3000" {
		t.Errorf("Expected Access-Control-Allow-Origin 'http://localhost:3000', got %q", origin)
	}

	if creds := rec.Header().Get("Access-Control-Allow-Credentials"); creds != "true" {
		t.Errorf("Expected Access-Control-Allow-Credentials 'true', got %q", creds)
	}

	methods := rec.Header().Get("Access-Control-Allow-Methods")
	if !strings.Contains(methods, "GET") || !strings.Contains(methods, "POST") {
		t.Errorf("Expected Access-Control-Allow-Methods to contain GET and POST, got %q", methods)
	}

	headers := rec.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(headers, "Content-Type") || !strings.Contains(headers, "Authorization") {
		t.Errorf("Expected Access-Control-Allow-Headers to contain Content-Type and Authorization, got %q", headers)
	}

	if maxAge := rec.Header().Get("Access-Control-Max-Age"); maxAge != "3600" {
		t.Errorf("Expected Access-Control-Max-Age '3600', got %q", maxAge)
	}

	// Verify next handler was NOT called (preflight should return immediately)
	if nextHandlerCalled {
		t.Error("Next handler should not be called for preflight requests")
	}
}

// TestCORS_PreflightRequest_DisallowedOrigin tests that
// preflight requests from disallowed origins do not get CORS headers
func TestCORS_PreflightRequest_DisallowedOrigin(t *testing.T) {
	logger := &mockCORSLogger{}
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: false,
			origins: []string{"http://localhost:3000"},
		},
		Logger: logger,
	}

	nextHandlerCalled := false
	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	req.Header.Set("Origin", "http://malicious.com")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify CORS headers are NOT set
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Errorf("Expected no Access-Control-Allow-Origin header, got %q", origin)
	}

	if methods := rec.Header().Get("Access-Control-Allow-Methods"); methods != "" {
		t.Errorf("Expected no Access-Control-Allow-Methods header, got %q", methods)
	}

	// Verify warning was logged
	if logger.warnCount != 1 {
		t.Errorf("Expected 1 warning log, got %d", logger.warnCount)
	}

	if !strings.Contains(logger.lastMsg, "origin not allowed") {
		t.Errorf("Expected warning about disallowed origin, got: %s", logger.lastMsg)
	}

	// For OPTIONS with disallowed origin, the next handler is still called
	// (but browser will block the response)
	if !nextHandlerCalled {
		t.Error("Next handler should still be called for disallowed preflight")
	}
}

// TestCORS_ActualRequest_AllowedOrigin tests that
// actual requests from allowed origins get CORS headers and call next handler
func TestCORS_ActualRequest_AllowedOrigin(t *testing.T) {
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: true,
			origins: []string{"http://localhost:3000"},
		},
		Logger: &NoOpLogger{},
	}

	nextHandlerCalled := false
	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success")) //nolint:errcheck
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify status code (from next handler)
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	// Verify CORS headers
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3000" {
		t.Errorf("Expected Access-Control-Allow-Origin 'http://localhost:3000', got %q", origin)
	}

	if creds := rec.Header().Get("Access-Control-Allow-Credentials"); creds != "true" {
		t.Errorf("Expected Access-Control-Allow-Credentials 'true', got %q", creds)
	}

	// Verify next handler was called
	if !nextHandlerCalled {
		t.Error("Next handler should be called for actual requests")
	}

	// Verify response body
	if body := rec.Body.String(); body != "success" {
		t.Errorf("Expected body 'success', got %q", body)
	}
}

// TestCORS_ActualRequest_DisallowedOrigin tests that
// actual requests from disallowed origins do not get CORS headers but still call next handler
func TestCORS_ActualRequest_DisallowedOrigin(t *testing.T) {
	logger := &mockCORSLogger{}
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: false,
			origins: []string{"http://localhost:3000"},
		},
		Logger: logger,
	}

	nextHandlerCalled := false
	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://malicious.com")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify CORS headers are NOT set
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Errorf("Expected no Access-Control-Allow-Origin header, got %q", origin)
	}

	// Verify warning was logged
	if logger.warnCount != 1 {
		t.Errorf("Expected 1 warning log, got %d", logger.warnCount)
	}

	// Verify next handler was still called (browser blocks response, not server)
	if !nextHandlerCalled {
		t.Error("Next handler should still be called for disallowed actual requests")
	}
}

// TestCORS_SameOriginRequest_NoOriginHeader tests that
// same-origin requests (no Origin header) skip CORS processing
func TestCORS_SameOriginRequest_NoOriginHeader(t *testing.T) {
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: true,
			origins: []string{"http://localhost:3000"},
		},
		Logger: &NoOpLogger{},
	}

	nextHandlerCalled := false
	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextHandlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	// Request without Origin header (same-origin)
	req := httptest.NewRequest("GET", "/api/test", nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify CORS headers are NOT set
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Errorf("Expected no Access-Control-Allow-Origin header for same-origin, got %q", origin)
	}

	// Verify next handler was called
	if !nextHandlerCalled {
		t.Error("Next handler should be called for same-origin requests")
	}
}

// TestCORS_CustomValidator tests integration with custom validator
func TestCORS_CustomValidator(t *testing.T) {
	customValidator := &mockOriginValidator{
		allowed: true,
		origins: []string{"http://custom.com"},
	}

	config := CORSConfig{
		AllowedMethods:   []string{"GET"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator:        customValidator,
		Logger:           &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://custom.com")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify custom validator was used (allowed origin)
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://custom.com" {
		t.Errorf("Expected Access-Control-Allow-Origin 'http://custom.com', got %q", origin)
	}
}

// TestCORS_LoggerIntegration tests that logger is called correctly
func TestCORS_LoggerIntegration(t *testing.T) {
	logger := &mockCORSLogger{}
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: false,
			origins: []string{"http://localhost:3000"},
		},
		Logger: logger,
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://malicious.com")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify Warn was called
	if logger.warnCount != 1 {
		t.Errorf("Expected 1 warning, got %d", logger.warnCount)
	}

	// Verify log message
	if !strings.Contains(logger.lastMsg, "origin not allowed") {
		t.Errorf("Expected 'origin not allowed' in log message, got: %s", logger.lastMsg)
	}

	// Verify log fields
	if logger.lastFields["origin"] != "http://malicious.com" {
		t.Errorf("Expected origin field 'http://malicious.com', got %v", logger.lastFields["origin"])
	}

	if logger.lastFields["path"] != "/api/test" {
		t.Errorf("Expected path field '/api/test', got %v", logger.lastFields["path"])
	}

	if logger.lastFields["method"] != "GET" {
		t.Errorf("Expected method field 'GET', got %v", logger.lastFields["method"])
	}
}

// TestCORS_PreflightRequest_LoggerDebug tests that
// debug logging is called for preflight requests
func TestCORS_PreflightRequest_LoggerDebug(t *testing.T) {
	logger := &mockCORSLogger{}
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: true,
			origins: []string{"http://localhost:3000"},
		},
		Logger: logger,
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify Debug was called
	if logger.debugCount != 1 {
		t.Errorf("Expected 1 debug log, got %d", logger.debugCount)
	}

	// Verify log message
	if !strings.Contains(logger.lastMsg, "preflight request") {
		t.Errorf("Expected 'preflight request' in log message, got: %s", logger.lastMsg)
	}

	// Verify log fields
	if logger.lastFields["origin"] != "http://localhost:3000" {
		t.Errorf("Expected origin field, got %v", logger.lastFields["origin"])
	}

	if logger.lastFields["requested_method"] != "POST" {
		t.Errorf("Expected requested_method field 'POST', got %v", logger.lastFields["requested_method"])
	}
}

// TestCORS_AllowedMethodsHeader tests that
// Access-Control-Allow-Methods contains all configured methods
func TestCORS_AllowedMethodsHeader(t *testing.T) {
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: true,
			origins: []string{"http://localhost:3000"},
		},
		Logger: &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	methods := rec.Header().Get("Access-Control-Allow-Methods")

	// Verify all methods are present
	expectedMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
	for _, method := range expectedMethods {
		if !strings.Contains(methods, method) {
			t.Errorf("Expected Access-Control-Allow-Methods to contain %s, got %q", method, methods)
		}
	}
}

// TestCORS_AllowedHeadersHeader tests that
// Access-Control-Allow-Headers contains all configured headers
func TestCORS_AllowedHeadersHeader(t *testing.T) {
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type", "Authorization", "X-Request-ID", "X-Custom-Header"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: true,
			origins: []string{"http://localhost:3000"},
		},
		Logger: &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/api/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	headers := rec.Header().Get("Access-Control-Allow-Headers")

	// Verify all headers are present
	expectedHeaders := []string{"Content-Type", "Authorization", "X-Request-ID", "X-Custom-Header"}
	for _, header := range expectedHeaders {
		if !strings.Contains(headers, header) {
			t.Errorf("Expected Access-Control-Allow-Headers to contain %s, got %q", header, headers)
		}
	}
}

// TestCORS_MaxAgeHeader tests that
// Access-Control-Max-Age matches configured value
func TestCORS_MaxAgeHeader(t *testing.T) {
	testCases := []struct {
		name   string
		maxAge int
	}{
		{"1 hour", 3600},
		{"24 hours", 86400},
		{"1 week", 604800},
		{"no cache", 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := CORSConfig{
				AllowedMethods:   []string{"GET", "POST"},
				AllowedHeaders:   []string{"Content-Type"},
				AllowCredentials: true,
				MaxAge:           tc.maxAge,
				Validator: &mockOriginValidator{
					allowed: true,
					origins: []string{"http://localhost:3000"},
				},
				Logger: &NoOpLogger{},
			}

			handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("OPTIONS", "/api/test", nil)
			req.Header.Set("Origin", "http://localhost:3000")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			maxAge := rec.Header().Get("Access-Control-Max-Age")
			// Convert int to string for comparison
			if maxAge != strconv.Itoa(tc.maxAge) {
				t.Errorf("Expected Access-Control-Max-Age %d, got %q", tc.maxAge, maxAge)
			}
		})
	}
}

// TestCORS_AllowCredentialsAlwaysTrue tests that
// Access-Control-Allow-Credentials is always "true"
func TestCORS_AllowCredentialsAlwaysTrue(t *testing.T) {
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: true,
			origins: []string{"http://localhost:3000"},
		},
		Logger: &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test both preflight and actual request
	testCases := []struct {
		name   string
		method string
	}{
		{"preflight", "OPTIONS"},
		{"actual GET", "GET"},
		{"actual POST", "POST"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/test", nil)
			req.Header.Set("Origin", "http://localhost:3000")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			creds := rec.Header().Get("Access-Control-Allow-Credentials")
			if creds != "true" {
				t.Errorf("Expected Access-Control-Allow-Credentials 'true', got %q", creds)
			}
		})
	}
}

// TestCORS_NoDuplicateHeaders tests that
// CORS headers are not duplicated on subsequent calls
func TestCORS_NoDuplicateHeaders(t *testing.T) {
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: true,
			origins: []string{"http://localhost:3000"},
		},
		Logger: &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make two requests
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("Origin", "http://localhost:3000")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		// Verify headers are set exactly once
		origins := rec.Header().Values("Access-Control-Allow-Origin")
		if len(origins) != 1 {
			t.Errorf("Request %d: Expected 1 Access-Control-Allow-Origin header, got %d", i+1, len(origins))
		}
	}
}

// TestCORS_MultipleHTTPMethods tests CORS with various HTTP methods
func TestCORS_MultipleHTTPMethods(t *testing.T) {
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: true,
			origins: []string{"http://localhost:3000"},
		},
		Logger: &NoOpLogger{},
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/test", nil)
			req.Header.Set("Origin", "http://localhost:3000")

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Verify CORS headers are set
			if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3000" {
				t.Errorf("Expected Access-Control-Allow-Origin, got %q", origin)
			}
		})
	}
}

// TestCORS_NoLogger tests that middleware works without logger
func TestCORS_NoLogger(t *testing.T) {
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: false,
			origins: []string{"http://localhost:3000"},
		},
		Logger: nil, // No logger
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("Origin", "http://malicious.com")

	rec := httptest.NewRecorder()

	// Should not panic without logger
	handler.ServeHTTP(rec, req)

	// Verify request was processed
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestCORS_EmptyOriginString tests handling of empty origin
func TestCORS_EmptyOriginString(t *testing.T) {
	logger := &mockCORSLogger{}
	config := CORSConfig{
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
		Validator: &mockOriginValidator{
			allowed: true,
			origins: []string{"http://localhost:3000"},
		},
		Logger: logger,
	}

	handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/test", nil)
	// No Origin header set

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify no CORS headers
	if origin := rec.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Errorf("Expected no CORS headers for empty origin, got %q", origin)
	}

	// Verify no warnings logged (empty origin is valid for same-origin)
	if logger.warnCount != 0 {
		t.Errorf("Expected no warnings for empty origin, got %d", logger.warnCount)
	}
}

// TestCORS_OriginEchoBack tests that the exact Origin is echoed back
func TestCORS_OriginEchoBack(t *testing.T) {
	testCases := []string{
		"http://localhost:3000",
		"https://example.com",
		"http://subdomain.example.com:8080",
	}

	for _, origin := range testCases {
		t.Run(origin, func(t *testing.T) {
			config := CORSConfig{
				AllowedMethods:   []string{"GET"},
				AllowedHeaders:   []string{"Content-Type"},
				AllowCredentials: true,
				MaxAge:           3600,
				Validator: &mockOriginValidator{
					allowed: true,
					origins: []string{origin},
				},
				Logger: &NoOpLogger{},
			}

			handler := CORS(config)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/api/test", nil)
			req.Header.Set("Origin", origin)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Verify exact origin is echoed back
			allowedOrigin := rec.Header().Get("Access-Control-Allow-Origin")
			if allowedOrigin != origin {
				t.Errorf("Expected Access-Control-Allow-Origin %q, got %q", origin, allowedOrigin)
			}
		})
	}
}
