package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"catchup-feed/pkg/security/csp"
)

// TestNewCSPMiddleware verifies CSPMiddleware instance creation
func TestNewCSPMiddleware(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.StrictPolicy(),
	}

	middleware := NewCSPMiddleware(config)

	if middleware == nil {
		t.Fatal("NewCSPMiddleware returned nil")
	}

	if middleware.config.Enabled != config.Enabled {
		t.Error("Expected Enabled to match config")
	}

	if middleware.config.DefaultPolicy == nil {
		t.Error("Expected DefaultPolicy to be set")
	}
}

// TestCSPMiddleware_Disabled tests that CSP headers are not added when disabled
func TestCSPMiddleware_Disabled(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:       false,
		DefaultPolicy: csp.StrictPolicy(),
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify no CSP header was added
	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("Expected no CSP header when disabled")
	}

	if rec.Header().Get("Content-Security-Policy-Report-Only") != "" {
		t.Error("Expected no CSP-Report-Only header when disabled")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestCSPMiddleware_DefaultPolicyApplication tests default policy is applied
func TestCSPMiddleware_DefaultPolicyApplication(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.StrictPolicy(),
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify CSP header was added
	cspHeader := rec.Header().Get("Content-Security-Policy")
	if cspHeader == "" {
		t.Fatal("Expected CSP header to be set")
	}

	// Verify it's a strict policy
	expectedDirectives := []string{
		"default-src 'none'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
	}

	for _, directive := range expectedDirectives {
		if !strings.Contains(cspHeader, directive) {
			t.Errorf("Expected CSP header to contain %q, got %q", directive, cspHeader)
		}
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestCSPMiddleware_PathBasedPolicySelection tests path-based policy selection
func TestCSPMiddleware_PathBasedPolicySelection(t *testing.T) {
	tests := []struct {
		name               string
		requestPath        string
		expectedDirectives []string
		unexpectedSubstr   string
	}{
		{
			name:        "swagger path uses SwaggerUIPolicy",
			requestPath: "/swagger/index.html",
			expectedDirectives: []string{
				"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net",
				"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net",
			},
		},
		{
			name:        "api path uses StrictPolicy",
			requestPath: "/api/articles",
			expectedDirectives: []string{
				"default-src 'none'",
				"connect-src 'self'",
			},
			unexpectedSubstr: "unsafe-inline",
		},
		{
			name:        "other path uses DefaultPolicy (RelaxedPolicy)",
			requestPath: "/health",
			expectedDirectives: []string{
				"default-src 'self'",
				"script-src 'self' 'unsafe-inline' 'unsafe-eval' https:",
			},
		},
	}

	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.RelaxedPolicy(), // Used for non-matching paths
		PathPolicies: map[string]*csp.CSPBuilder{
			"/swagger/": csp.SwaggerUIPolicy(),
			"/api/":     csp.StrictPolicy(),
		},
	}

	middleware := NewCSPMiddleware(config)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", tt.requestPath, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			cspHeader := rec.Header().Get("Content-Security-Policy")
			if cspHeader == "" {
				t.Fatal("Expected CSP header to be set")
			}

			for _, directive := range tt.expectedDirectives {
				if !strings.Contains(cspHeader, directive) {
					t.Errorf("Expected CSP header to contain %q, got %q", directive, cspHeader)
				}
			}

			if tt.unexpectedSubstr != "" && strings.Contains(cspHeader, tt.unexpectedSubstr) {
				t.Errorf("Expected CSP header NOT to contain %q, got %q", tt.unexpectedSubstr, cspHeader)
			}
		})
	}
}

// TestCSPMiddleware_ReportOnlyMode tests report-only mode
func TestCSPMiddleware_ReportOnlyMode(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.StrictPolicy(),
		ReportOnly:    true,
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify report-only header is used
	reportOnlyHeader := rec.Header().Get("Content-Security-Policy-Report-Only")
	if reportOnlyHeader == "" {
		t.Fatal("Expected Content-Security-Policy-Report-Only header to be set")
	}

	// Verify enforcement header is NOT set
	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("Expected Content-Security-Policy header NOT to be set in report-only mode")
	}

	// Verify policy content
	if !strings.Contains(reportOnlyHeader, "default-src 'none'") {
		t.Errorf("Expected policy content, got %q", reportOnlyHeader)
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestCSPMiddleware_PolicyBuildingAndHeaderContent tests policy building
func TestCSPMiddleware_PolicyBuildingAndHeaderContent(t *testing.T) {
	policy := csp.NewCSPBuilder().
		DefaultSrc("'self'").
		ScriptSrc("'self'", "https://cdn.example.com").
		StyleSrc("'self'", "'unsafe-inline'").
		ImgSrc("'self'", "data:").
		FrameAncestors("'none'")

	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: policy,
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	cspHeader := rec.Header().Get("Content-Security-Policy")
	if cspHeader == "" {
		t.Fatal("Expected CSP header to be set")
	}

	// Verify all directives are present
	expectedDirectives := []string{
		"default-src 'self'",
		"script-src 'self' https://cdn.example.com",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"frame-ancestors 'none'",
	}

	for _, directive := range expectedDirectives {
		if !strings.Contains(cspHeader, directive) {
			t.Errorf("Expected CSP header to contain %q, got %q", directive, cspHeader)
		}
	}

	// Verify directives are separated by semicolons
	if !strings.Contains(cspHeader, ";") {
		t.Error("Expected CSP directives to be separated by semicolons")
	}
}

// TestCSPMiddleware_EmptyConfigWithDefaults tests empty config behavior
func TestCSPMiddleware_EmptyConfigWithDefaults(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: nil, // No default policy
		PathPolicies:  nil,
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify no CSP header when no policy is configured
	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("Expected no CSP header when no policy is configured")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestCSPMiddleware_MultiplePathPoliciesLongestMatch tests longest match wins
func TestCSPMiddleware_MultiplePathPoliciesLongestMatch(t *testing.T) {
	tests := []struct {
		name             string
		requestPath      string
		expectedInHeader string
	}{
		{
			name:             "/api/v1/users matches /api/v1/ (longest)",
			requestPath:      "/api/v1/users",
			expectedInHeader: "connect-src 'self'",
		},
		{
			name:             "/api/health matches /api/ (shorter)",
			requestPath:      "/api/health",
			expectedInHeader: "default-src 'none'",
		},
		{
			name:             "/other matches default policy",
			requestPath:      "/other",
			expectedInHeader: "default-src 'self'",
		},
	}

	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.RelaxedPolicy(),
		PathPolicies: map[string]*csp.CSPBuilder{
			"/api/":    csp.StrictPolicy(),
			"/api/v1/": csp.NewCSPBuilder().DefaultSrc("'self'").ConnectSrc("'self'"),
		},
	}

	middleware := NewCSPMiddleware(config)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", tt.requestPath, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			cspHeader := rec.Header().Get("Content-Security-Policy")
			if cspHeader == "" {
				t.Fatal("Expected CSP header to be set")
			}

			if !strings.Contains(cspHeader, tt.expectedInHeader) {
				t.Errorf("Expected CSP header to contain %q, got %q", tt.expectedInHeader, cspHeader)
			}
		})
	}
}

// TestCSPMiddleware_ConcurrentRequests tests thread-safety with concurrent requests
func TestCSPMiddleware_ConcurrentRequests(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.StrictPolicy(),
		PathPolicies: map[string]*csp.CSPBuilder{
			"/swagger/": csp.SwaggerUIPolicy(),
			"/api/":     csp.StrictPolicy(),
		},
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	paths := []string{"/test", "/swagger/index.html", "/api/articles"}

	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()

			path := paths[index%len(paths)]
			req := httptest.NewRequest("GET", path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			// Verify CSP header was set
			cspHeader := rec.Header().Get("Content-Security-Policy")
			if cspHeader == "" {
				t.Errorf("Expected CSP header to be set for path %s", path)
			}

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
			}
		}(i)
	}

	wg.Wait()
}

// TestCSPMiddleware_EdgeCasesUnknownPath tests edge case with unknown path
func TestCSPMiddleware_EdgeCasesUnknownPath(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.StrictPolicy(),
		PathPolicies: map[string]*csp.CSPBuilder{
			"/api/": csp.RelaxedPolicy(),
		},
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/unknown", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should use default policy for unknown path
	cspHeader := rec.Header().Get("Content-Security-Policy")
	if cspHeader == "" {
		t.Fatal("Expected CSP header to be set")
	}

	if !strings.Contains(cspHeader, "default-src 'none'") {
		t.Errorf("Expected default policy for unknown path, got %q", cspHeader)
	}
}

// TestCSPMiddleware_EdgeCasesRootPath tests edge case with root path
func TestCSPMiddleware_EdgeCasesRootPath(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.StrictPolicy(),
		PathPolicies: map[string]*csp.CSPBuilder{
			"/": csp.RelaxedPolicy(),
		},
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	cspHeader := rec.Header().Get("Content-Security-Policy")
	if cspHeader == "" {
		t.Fatal("Expected CSP header to be set")
	}

	// Should use the "/" path policy (RelaxedPolicy)
	if !strings.Contains(cspHeader, "unsafe-inline") {
		t.Errorf("Expected relaxed policy for root path, got %q", cspHeader)
	}
}

// TestCSPMiddleware_HeaderValueFormatCorrectness tests header format
func TestCSPMiddleware_HeaderValueFormatCorrectness(t *testing.T) {
	policy := csp.NewCSPBuilder().
		DefaultSrc("'self'").
		ScriptSrc("'self'", "https://cdn.example.com").
		StyleSrc("'self'", "'unsafe-inline'")

	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: policy,
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	cspHeader := rec.Header().Get("Content-Security-Policy")
	if cspHeader == "" {
		t.Fatal("Expected CSP header to be set")
	}

	// Verify format: directives separated by "; "
	directives := strings.Split(cspHeader, "; ")
	if len(directives) < 3 {
		t.Errorf("Expected at least 3 directives, got %d: %q", len(directives), cspHeader)
	}

	// Verify each directive has correct format: "directive-name source1 source2"
	for _, directive := range directives {
		parts := strings.SplitN(directive, " ", 2)
		if len(parts) < 2 {
			t.Errorf("Invalid directive format: %q", directive)
		}

		// Verify directive name format (lowercase with hyphens)
		directiveName := parts[0]
		if !strings.Contains(directiveName, "-src") && directiveName != "frame-ancestors" {
			t.Errorf("Unexpected directive name: %q", directiveName)
		}
	}
}

// TestCSPMiddleware_WithMetrics tests WithMetrics method
func TestCSPMiddleware_WithMetrics(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.StrictPolicy(),
	}

	middleware := NewCSPMiddleware(config)

	// Verify initial metrics is nil
	if middleware.metrics != nil {
		t.Error("Expected initial metrics to be nil")
	}

	// Mock metrics (we can't test the actual metrics recording here)
	// but we can verify the method chain works
	result := middleware.WithMetrics(nil)

	if result != middleware {
		t.Error("WithMetrics should return the middleware instance for method chaining")
	}
}

// TestCSPMiddleware_EmptyPolicySkipped tests that empty policies are skipped
func TestCSPMiddleware_EmptyPolicySkipped(t *testing.T) {
	emptyPolicy := csp.NewCSPBuilder() // No directives added

	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: emptyPolicy,
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify no CSP header when policy builds to empty string
	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("Expected no CSP header when policy is empty")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestShouldApplyCSP tests the utility function for backward compatibility
func TestShouldApplyCSP(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		applyToPaths []string
		expected     bool
	}{
		{
			name:         "exact match",
			path:         "/swagger/",
			applyToPaths: []string{"/swagger/"},
			expected:     true,
		},
		{
			name:         "wildcard match",
			path:         "/swagger/index.html",
			applyToPaths: []string{"/swagger/*"},
			expected:     true,
		},
		{
			name:         "prefix match with trailing slash",
			path:         "/api/v1/users",
			applyToPaths: []string{"/api/"},
			expected:     true,
		},
		{
			name:         "no match",
			path:         "/health",
			applyToPaths: []string{"/api/", "/swagger/"},
			expected:     false,
		},
		{
			name:         "empty path list",
			path:         "/test",
			applyToPaths: []string{},
			expected:     false,
		},
		{
			name:         "wildcard deep path",
			path:         "/docs/api/v1/reference",
			applyToPaths: []string{"/docs/*"},
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldApplyCSP(tt.path, tt.applyToPaths)
			if result != tt.expected {
				t.Errorf("ShouldApplyCSP(%q, %v) = %v, expected %v",
					tt.path, tt.applyToPaths, result, tt.expected)
			}
		})
	}
}

// TestCSPMiddleware_ReportOnlyWithPathPolicies tests report-only with path policies
func TestCSPMiddleware_ReportOnlyWithPathPolicies(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:    true,
		ReportOnly: true,
		PathPolicies: map[string]*csp.CSPBuilder{
			"/api/": csp.StrictPolicy(),
		},
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/users", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify report-only header is used even with path policies
	reportOnlyHeader := rec.Header().Get("Content-Security-Policy-Report-Only")
	if reportOnlyHeader == "" {
		t.Fatal("Expected Content-Security-Policy-Report-Only header to be set")
	}

	if rec.Header().Get("Content-Security-Policy") != "" {
		t.Error("Expected Content-Security-Policy header NOT to be set in report-only mode")
	}

	if !strings.Contains(reportOnlyHeader, "default-src 'none'") {
		t.Errorf("Expected strict policy content, got %q", reportOnlyHeader)
	}
}

// TestCSPMiddleware_HandlerChain tests CSP middleware in handler chain
func TestCSPMiddleware_HandlerChain(t *testing.T) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.StrictPolicy(),
	}

	middleware := NewCSPMiddleware(config)

	// Create a handler chain with CSP middleware
	finalHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	handler := middleware.Middleware()(finalHandler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Verify CSP header was added
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("Expected CSP header to be set")
	}

	// Verify handler executed
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	if rec.Body.String() != "OK" {
		t.Errorf("Expected body %q, got %q", "OK", rec.Body.String())
	}
}

// Benchmark tests
func BenchmarkCSPMiddleware_DefaultPolicy(b *testing.B) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.StrictPolicy(),
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkCSPMiddleware_PathSelection(b *testing.B) {
	config := CSPMiddlewareConfig{
		Enabled:       true,
		DefaultPolicy: csp.StrictPolicy(),
		PathPolicies: map[string]*csp.CSPBuilder{
			"/swagger/": csp.SwaggerUIPolicy(),
			"/api/":     csp.StrictPolicy(),
			"/docs/":    csp.RelaxedPolicy(),
		},
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/swagger/index.html", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkCSPMiddleware_Disabled(b *testing.B) {
	config := CSPMiddlewareConfig{
		Enabled:       false,
		DefaultPolicy: csp.StrictPolicy(),
	}

	middleware := NewCSPMiddleware(config)
	handler := middleware.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}
