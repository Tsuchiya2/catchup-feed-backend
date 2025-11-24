package pathutil

import (
	"testing"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		// Article routes with IDs (should be normalized)
		{
			name:     "article with ID 123",
			path:     "/articles/123",
			expected: "/articles/:id",
		},
		{
			name:     "article with ID 456",
			path:     "/articles/456",
			expected: "/articles/:id",
		},
		{
			name:     "article with ID 999999",
			path:     "/articles/999999",
			expected: "/articles/:id",
		},
		{
			name:     "article with ID and trailing slash",
			path:     "/articles/123/",
			expected: "/articles/:id",
		},
		{
			name:     "article with ID and query params",
			path:     "/articles/123?page=1",
			expected: "/articles/:id",
		},
		{
			name:     "article comments",
			path:     "/articles/123/comments",
			expected: "/articles/:id/comments",
		},
		{
			name:     "article related",
			path:     "/articles/456/related",
			expected: "/articles/:id/related",
		},

		// Source routes with IDs (should be normalized)
		{
			name:     "source with ID 789",
			path:     "/sources/789",
			expected: "/sources/:id",
		},
		{
			name:     "source with ID 1",
			path:     "/sources/1",
			expected: "/sources/:id",
		},
		{
			name:     "source with ID and trailing slash",
			path:     "/sources/123/",
			expected: "/sources/:id",
		},
		{
			name:     "source articles",
			path:     "/sources/123/articles",
			expected: "/sources/:id/articles",
		},
		{
			name:     "source stats",
			path:     "/sources/456/stats",
			expected: "/sources/:id/stats",
		},

		// User routes with IDs (should be normalized)
		{
			name:     "user with ID",
			path:     "/users/123",
			expected: "/users/:id",
		},
		{
			name:     "user profile",
			path:     "/users/456/profile",
			expected: "/users/:id/profile",
		},

		// Search endpoints (should remain unchanged)
		{
			name:     "article search",
			path:     "/articles/search",
			expected: "/articles/search",
		},
		{
			name:     "article search with query params",
			path:     "/articles/search?q=golang",
			expected: "/articles/search",
		},
		{
			name:     "source search",
			path:     "/sources/search",
			expected: "/sources/search",
		},

		// Static endpoints (should remain unchanged)
		{
			name:     "health endpoint",
			path:     "/health",
			expected: "/health",
		},
		{
			name:     "health with query params",
			path:     "/health?format=json",
			expected: "/health",
		},
		{
			name:     "metrics endpoint",
			path:     "/metrics",
			expected: "/metrics",
		},
		{
			name:     "auth token endpoint",
			path:     "/auth/token",
			expected: "/auth/token",
		},
		{
			name:     "ready endpoint",
			path:     "/ready",
			expected: "/ready",
		},
		{
			name:     "live endpoint",
			path:     "/live",
			expected: "/live",
		},
		{
			name:     "swagger docs",
			path:     "/swagger/index.html",
			expected: "/swagger/index.html",
		},

		// List endpoints (should remain unchanged)
		{
			name:     "articles list",
			path:     "/articles",
			expected: "/articles",
		},
		{
			name:     "articles list with query params",
			path:     "/articles?page=1&limit=10",
			expected: "/articles",
		},
		{
			name:     "sources list",
			path:     "/sources",
			expected: "/sources",
		},

		// Unknown/unmatched paths (should remain unchanged)
		{
			name:     "unknown path with ID",
			path:     "/unknown/path/123",
			expected: "/unknown/path/123",
		},
		{
			name:     "unknown nested path",
			path:     "/api/v2/items/456",
			expected: "/api/v2/items/456",
		},

		// Edge cases
		{
			name:     "root path",
			path:     "/",
			expected: "/",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "",
		},
		{
			name:     "path with only query params",
			path:     "/?page=1",
			expected: "/",
		},
		{
			name:     "article with non-numeric ID (should not normalize)",
			path:     "/articles/abc",
			expected: "/articles/abc",
		},
		{
			name:     "article with UUID-like string (should not normalize)",
			path:     "/articles/550e8400-e29b-41d4-a716-446655440000",
			expected: "/articles/550e8400-e29b-41d4-a716-446655440000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePath(tt.path)
			if result != tt.expected {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestNormalizePath_Cardinality(t *testing.T) {
	// Test that different IDs produce the same normalized path
	paths := []string{
		"/articles/1",
		"/articles/2",
		"/articles/123",
		"/articles/456",
		"/articles/789",
		"/articles/999999",
	}

	expected := "/articles/:id"
	for _, path := range paths {
		result := NormalizePath(path)
		if result != expected {
			t.Errorf("NormalizePath(%q) = %q, want %q (cardinality check failed)", path, result, expected)
		}
	}

	// Verify that this reduces cardinality from 6 to 1
	uniqueResults := make(map[string]bool)
	for _, path := range paths {
		uniqueResults[NormalizePath(path)] = true
	}

	if len(uniqueResults) != 1 {
		t.Errorf("Expected cardinality of 1, got %d unique paths: %v", len(uniqueResults), uniqueResults)
	}
}

func TestNormalizePath_TrailingSlash(t *testing.T) {
	// Test that trailing slashes are handled consistently
	tests := []struct {
		path1    string
		path2    string
		expected string
	}{
		{"/articles/123", "/articles/123/", "/articles/:id"},
		{"/sources/456", "/sources/456/", "/sources/:id"},
		{"/health", "/health/", "/health"},
		{"/articles", "/articles/", "/articles"},
	}

	for _, tt := range tests {
		result1 := NormalizePath(tt.path1)
		result2 := NormalizePath(tt.path2)

		if result1 != tt.expected {
			t.Errorf("NormalizePath(%q) = %q, want %q", tt.path1, result1, tt.expected)
		}
		if result2 != tt.expected {
			t.Errorf("NormalizePath(%q) = %q, want %q", tt.path2, result2, tt.expected)
		}
		if result1 != result2 {
			t.Errorf("Trailing slash inconsistency: %q vs %q", result1, result2)
		}
	}
}

func TestNormalizePath_QueryParameters(t *testing.T) {
	// Test that query parameters are stripped before normalization
	tests := []struct {
		path     string
		expected string
	}{
		{"/articles/123?page=1", "/articles/:id"},
		{"/articles/123?page=1&limit=10", "/articles/:id"},
		{"/articles/search?q=golang", "/articles/search"},
		{"/health?format=json", "/health"},
		{"/sources/456?include=stats", "/sources/:id"},
	}

	for _, tt := range tests {
		result := NormalizePath(tt.path)
		if result != tt.expected {
			t.Errorf("NormalizePath(%q) = %q, want %q", tt.path, result, tt.expected)
		}
	}
}

func TestGetExpectedCardinality(t *testing.T) {
	cardinality := GetExpectedCardinality()

	// Expected cardinality should be between 15 and 35
	// (8 template patterns + ~10 static endpoints)
	if cardinality < 15 || cardinality > 35 {
		t.Errorf("GetExpectedCardinality() = %d, want between 15 and 35", cardinality)
	}

	t.Logf("Expected cardinality: %d unique path labels", cardinality)
}

func TestNormalizePath_RealWorldScenario(t *testing.T) {
	// Simulate a real-world scenario with many requests
	// This demonstrates the cardinality reduction
	requests := []string{
		// 100 different article IDs
		"/articles/1", "/articles/2", "/articles/3", "/articles/4", "/articles/5",
		"/articles/10", "/articles/20", "/articles/30", "/articles/40", "/articles/50",
		"/articles/100", "/articles/200", "/articles/300", "/articles/400", "/articles/500",
		// ... many more ...
		"/articles/999", "/articles/1000",

		// 50 different source IDs
		"/sources/1", "/sources/2", "/sources/3",
		"/sources/10", "/sources/20", "/sources/30",
		// ... many more ...

		// Static endpoints
		"/health", "/metrics", "/auth/token",
		"/articles", "/sources",
		"/articles/search", "/sources/search",
	}

	// Collect unique normalized paths
	uniquePaths := make(map[string]int)
	for _, path := range requests {
		normalized := NormalizePath(path)
		uniquePaths[normalized]++
	}

	// Verify that cardinality is low
	if len(uniquePaths) > 30 {
		t.Errorf("Expected cardinality ≤30, got %d unique paths", len(uniquePaths))
	}

	t.Logf("Real-world scenario: %d requests reduced to %d unique paths", len(requests), len(uniquePaths))
	for path, count := range uniquePaths {
		t.Logf("  %s: %d requests", path, count)
	}
}
