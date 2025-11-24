package pathutil

import (
	"fmt"
	"testing"
)

// BenchmarkNormalizePath benchmarks the path normalization function.
// Target: <1μs per operation
func BenchmarkNormalizePath(b *testing.B) {
	paths := []string{
		"/articles/123",
		"/articles/456/comments",
		"/sources/789",
		"/sources/1/articles",
		"/articles/search",
		"/health",
		"/metrics",
		"/auth/token",
		"/unknown/path/123",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		_ = NormalizePath(path)
	}
}

// BenchmarkNormalizePath_Match benchmarks paths that match patterns (common case).
func BenchmarkNormalizePath_Match(b *testing.B) {
	path := "/articles/123"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizePath(path)
	}
}

// BenchmarkNormalizePath_NoMatch benchmarks paths that don't match (static endpoints).
func BenchmarkNormalizePath_NoMatch(b *testing.B) {
	path := "/health"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizePath(path)
	}
}

// BenchmarkNormalizePath_WithQueryParams benchmarks paths with query parameters.
func BenchmarkNormalizePath_WithQueryParams(b *testing.B) {
	path := "/articles/123?page=1&limit=10"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizePath(path)
	}
}

// BenchmarkNormalizePath_WithTrailingSlash benchmarks paths with trailing slashes.
func BenchmarkNormalizePath_WithTrailingSlash(b *testing.B) {
	path := "/articles/123/"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizePath(path)
	}
}

// BenchmarkNormalizePath_LongPath benchmarks very long paths.
func BenchmarkNormalizePath_LongPath(b *testing.B) {
	path := "/articles/123456789012345678901234567890/comments"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizePath(path)
	}
}

// BenchmarkNormalizePath_Parallel benchmarks concurrent normalization (simulates real load).
func BenchmarkNormalizePath_Parallel(b *testing.B) {
	paths := []string{
		"/articles/123",
		"/sources/456",
		"/health",
		"/articles/search",
	}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			path := paths[i%len(paths)]
			_ = NormalizePath(path)
			i++
		}
	})
}

// BenchmarkNormalizePath_AllPatterns benchmarks each pattern individually.
func BenchmarkNormalizePath_AllPatterns(b *testing.B) {
	testCases := []struct {
		name string
		path string
	}{
		{"article_id", "/articles/123"},
		{"article_comments", "/articles/123/comments"},
		{"article_related", "/articles/123/related"},
		{"source_id", "/sources/456"},
		{"source_articles", "/sources/456/articles"},
		{"source_stats", "/sources/456/stats"},
		{"user_id", "/users/789"},
		{"user_profile", "/users/789/profile"},
		{"static_health", "/health"},
		{"static_metrics", "/metrics"},
		{"search", "/articles/search"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = NormalizePath(tc.path)
			}
		})
	}
}

// BenchmarkNormalizePath_WorstCase benchmarks the worst-case scenario (no match, all patterns checked).
func BenchmarkNormalizePath_WorstCase(b *testing.B) {
	path := "/unknown/very/long/path/that/does/not/match/any/pattern/123"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NormalizePath(path)
	}
}

// BenchmarkNormalizePath_VsRawPath compares normalized vs raw path performance.
// This demonstrates the overhead of normalization.
func BenchmarkNormalizePath_VsRawPath(b *testing.B) {
	path := "/articles/123"

	b.Run("with_normalization", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = NormalizePath(path)
		}
	})

	b.Run("without_normalization", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = path // Just use the path directly
		}
	})
}

// BenchmarkNormalizePath_CardinalityReduction demonstrates the memory savings.
// This shows why normalization is important for Prometheus metrics.
func BenchmarkNormalizePath_CardinalityReduction(b *testing.B) {
	// Simulate 10,000 unique article IDs
	paths := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		paths[i] = fmt.Sprintf("/articles/%d", i+1)
	}

	b.Run("raw_paths", func(b *testing.B) {
		uniquePaths := make(map[string]bool)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			path := paths[i%len(paths)]
			uniquePaths[path] = true
		}
		b.StopTimer()
		b.Logf("Raw paths: %d unique paths", len(uniquePaths))
	})

	b.Run("normalized_paths", func(b *testing.B) {
		uniquePaths := make(map[string]bool)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			path := paths[i%len(paths)]
			normalized := NormalizePath(path)
			uniquePaths[normalized] = true
		}
		b.StopTimer()
		b.Logf("Normalized paths: %d unique paths", len(uniquePaths))
	})
}
