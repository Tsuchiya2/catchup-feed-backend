package auth

import "testing"

// TestIsPublicEndpoint_ExhaustiveCoverage provides comprehensive test coverage
// for the IsPublicEndpoint function.
func TestIsPublicEndpoint_ExhaustiveCoverage(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
		reason   string
	}{
		// Health check endpoints (Kubernetes/Docker health probes)
		{
			name:     "health check exact",
			path:     "/health",
			expected: true,
			reason:   "Required for orchestration health checks",
		},
		{
			name:     "readiness probe exact",
			path:     "/ready",
			expected: true,
			reason:   "Required for Kubernetes readiness probes",
		},
		{
			name:     "liveness probe exact",
			path:     "/live",
			expected: true,
			reason:   "Required for Kubernetes liveness probes",
		},

		// Metrics endpoint (Prometheus)
		{
			name:     "metrics exact",
			path:     "/metrics",
			expected: true,
			reason:   "Required for Prometheus scraping",
		},

		// Swagger documentation
		{
			name:     "swagger root",
			path:     "/swagger/",
			expected: true,
			reason:   "API documentation",
		},
		{
			name:     "swagger index",
			path:     "/swagger/index.html",
			expected: true,
			reason:   "API documentation index",
		},
		{
			name:     "swagger css",
			path:     "/swagger/swagger-ui.css",
			expected: true,
			reason:   "API documentation resources",
		},
		{
			name:     "swagger js",
			path:     "/swagger/swagger-ui-bundle.js",
			expected: true,
			reason:   "API documentation resources",
		},
		{
			name:     "swagger json",
			path:     "/swagger/doc.json",
			expected: true,
			reason:   "API documentation spec",
		},

		// Authentication endpoint
		{
			name:     "auth token exact",
			path:     "/auth/token",
			expected: true,
			reason:   "Token generation endpoint (can't require token to get token)",
		},

		// Protected endpoints - Articles
		{
			name:     "articles list",
			path:     "/articles",
			expected: false,
			reason:   "Protected resource - requires authentication",
		},
		{
			name:     "articles search",
			path:     "/articles/search",
			expected: false,
			reason:   "Protected resource - requires authentication",
		},
		{
			name:     "article detail",
			path:     "/articles/123",
			expected: false,
			reason:   "Protected resource - requires authentication",
		},
		{
			name:     "article with uuid",
			path:     "/articles/550e8400-e29b-41d4-a716-446655440000",
			expected: false,
			reason:   "Protected resource - requires authentication",
		},

		// Protected endpoints - Sources
		{
			name:     "sources list",
			path:     "/sources",
			expected: false,
			reason:   "Protected resource - requires authentication",
		},
		{
			name:     "sources search",
			path:     "/sources/search",
			expected: false,
			reason:   "Protected resource - requires authentication",
		},
		{
			name:     "source detail",
			path:     "/sources/456",
			expected: false,
			reason:   "Protected resource - requires authentication",
		},

		// Edge cases
		{
			name:     "root path",
			path:     "/",
			expected: false,
			reason:   "Root is not explicitly public",
		},
		{
			name:     "unknown path",
			path:     "/unknown",
			expected: false,
			reason:   "Unknown paths should be protected by default",
		},
		{
			name:     "admin path",
			path:     "/admin",
			expected: false,
			reason:   "Admin paths should require authentication",
		},
		{
			name:     "api path",
			path:     "/api",
			expected: false,
			reason:   "Generic API path should require authentication",
		},

		// Prefix matching edge cases
		{
			name:     "health with query params",
			path:     "/health?detailed=true",
			expected: true,
			reason:   "Query params don't affect prefix match",
		},
		{
			name:     "metrics with query params",
			path:     "/metrics?format=prometheus",
			expected: true,
			reason:   "Query params don't affect prefix match",
		},

		// Negative cases - paths that look similar but aren't public
		{
			name:     "healthcheck (no slash)",
			path:     "/healthcheck",
			expected: false,
			reason:   "Different from /health endpoint",
		},
		{
			name:     "metric (singular)",
			path:     "/metric",
			expected: false,
			reason:   "Different from /metrics endpoint",
		},
		{
			name:     "authenticate",
			path:     "/authenticate",
			expected: false,
			reason:   "Different from /auth/token endpoint",
		},
		{
			name:     "auth without token",
			path:     "/auth",
			expected: false,
			reason:   "Only /auth/token is public, not /auth",
		},
		{
			name:     "auth/login",
			path:     "/auth/login",
			expected: false,
			reason:   "Only /auth/token is public",
		},

		// Empty and special paths
		{
			name:     "empty path",
			path:     "",
			expected: false,
			reason:   "Empty path should not be public",
		},
		{
			name:     "path without leading slash",
			path:     "health",
			expected: false,
			reason:   "Path must start with /",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPublicEndpoint(tt.path)
			if result != tt.expected {
				t.Errorf("IsPublicEndpoint(%q) = %v, want %v\nReason: %s",
					tt.path, result, tt.expected, tt.reason)
			}
		})
	}
}

// TestPublicEndpointsList verifies the PublicEndpoints list contains
// the expected endpoints and no duplicates.
func TestPublicEndpointsList(t *testing.T) {
	expectedEndpoints := []string{
		"/health",
		"/ready",
		"/live",
		"/metrics",
		"/swagger/",
		"/auth/token",
	}

	if len(PublicEndpoints) != len(expectedEndpoints) {
		t.Errorf("Expected %d public endpoints, got %d",
			len(expectedEndpoints), len(PublicEndpoints))
	}

	// Check all expected endpoints are present
	endpointMap := make(map[string]bool)
	for _, endpoint := range PublicEndpoints {
		if endpointMap[endpoint] {
			t.Errorf("Duplicate endpoint found: %s", endpoint)
		}
		endpointMap[endpoint] = true
	}

	for _, expected := range expectedEndpoints {
		if !endpointMap[expected] {
			t.Errorf("Expected endpoint %s not found in PublicEndpoints", expected)
		}
	}
}

// TestIsPublicEndpoint_PrefixMatching verifies that prefix matching works correctly
// for endpoints with subpaths and query parameters.
func TestIsPublicEndpoint_PrefixMatching(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		// Swagger prefix matching
		{"swagger with nested path", "/swagger/ui/index.html", true},
		{"swagger with deep nesting", "/swagger/assets/css/style.css", true},

		// Should NOT match partial prefixes
		{"swagge (missing r)", "/swagge/index.html", false},
		{"swagg (partial)", "/swagg/index.html", false},

		// Auth token prefix matching
		{"auth token exact", "/auth/token", true},
		{"auth token with slash", "/auth/token/", true},

		// Should NOT match other auth paths
		{"auth only", "/auth", false},
		{"auth with different suffix", "/auth/refresh", false},
		{"auth with subpath", "/auth/users", false},

		// Health check - exact match only
		{"health exact", "/health", true},
		{"health with query", "/health?format=json", true},

		// Should NOT match extensions
		{"health check (no space)", "/healthcheck", false},
		{"health status", "/health-status", false},
		{"health/detail", "/health/detail", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPublicEndpoint(tt.path)
			if result != tt.expected {
				t.Errorf("IsPublicEndpoint(%q) = %v, want %v",
					tt.path, result, tt.expected)
			}
		})
	}
}

// BenchmarkIsPublicEndpoint measures the performance of the IsPublicEndpoint function.
func BenchmarkIsPublicEndpoint(b *testing.B) {
	paths := []string{
		"/health",
		"/articles",
		"/swagger/index.html",
		"/sources/search",
		"/unknown/path",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, path := range paths {
			IsPublicEndpoint(path)
		}
	}
}

// BenchmarkIsPublicEndpoint_PublicPath benchmarks public endpoint lookup.
func BenchmarkIsPublicEndpoint_PublicPath(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsPublicEndpoint("/health")
	}
}

// BenchmarkIsPublicEndpoint_ProtectedPath benchmarks protected endpoint lookup.
func BenchmarkIsPublicEndpoint_ProtectedPath(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IsPublicEndpoint("/articles")
	}
}
