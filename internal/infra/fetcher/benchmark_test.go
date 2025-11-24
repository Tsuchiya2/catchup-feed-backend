package fetcher_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"catchup-feed/internal/infra/fetcher"
)

// ───────────────────────────────────────────────────────────────
// TASK-020: Performance Benchmarks
// ───────────────────────────────────────────────────────────────

// BenchmarkFetchContent measures single content fetch performance
// Target: <10s per fetch (p99)
func BenchmarkFetchContent(b *testing.B) {
	// Set up HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := generateArticleHTML(3000) // 3KB article
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			b.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := contentFetcher.FetchContent(ctx, server.URL)
		if err != nil {
			b.Fatalf("FetchContent() error = %v", err)
		}
	}
}

// BenchmarkFetchContent_Small benchmarks small article fetching
func BenchmarkFetchContent_Small(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := generateArticleHTML(1000) // 1KB article
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			b.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := contentFetcher.FetchContent(ctx, server.URL)
		if err != nil {
			b.Fatalf("FetchContent() error = %v", err)
		}
	}
}

// BenchmarkFetchContent_Large benchmarks large article fetching
func BenchmarkFetchContent_Large(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := generateArticleHTML(50000) // 50KB article
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			b.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := contentFetcher.FetchContent(ctx, server.URL)
		if err != nil {
			b.Fatalf("FetchContent() error = %v", err)
		}
	}
}

// BenchmarkURLValidation benchmarks URL validation performance
// This is called on every request, so it should be fast
func BenchmarkURLValidation(b *testing.B) {
	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	ctx := context.Background()

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("<html><body>test</body></html>")); err != nil {
			b.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = contentFetcher.FetchContent(ctx, server.URL)
	}
}

// BenchmarkReadabilityExtraction benchmarks just the Readability extraction
func BenchmarkReadabilityExtraction(b *testing.B) {
	// Generate HTML once
	html := generateArticleHTML(10000)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			b.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := contentFetcher.FetchContent(ctx, server.URL)
		if err != nil {
			b.Fatalf("FetchContent() error = %v", err)
		}
	}
}

// BenchmarkConcurrentFetching benchmarks concurrent content fetches
// Target: 10 concurrent operations without contention
func BenchmarkConcurrentFetching(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := generateArticleHTML(5000)
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			b.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := contentFetcher.FetchContent(ctx, server.URL)
			if err != nil {
				b.Errorf("FetchContent() error = %v", err)
			}
		}
	})
}

// BenchmarkConfigValidation benchmarks config validation
func BenchmarkConfigValidation(b *testing.B) {
	cfg := fetcher.DefaultConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cfg.Validate()
	}
}

// BenchmarkConfigLoadFromEnv benchmarks environment variable loading
func BenchmarkConfigLoadFromEnv(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = fetcher.LoadConfigFromEnv()
	}
}

// ───────────────────────────────────────────────────────────────
// Helper functions
// ───────────────────────────────────────────────────────────────

// generateArticleHTML generates HTML of specified size for benchmarking
func generateArticleHTML(contentSize int) string {
	// Generate content to reach desired size
	paragraphText := "Lorem ipsum dolor sit amet, consectetur adipiscing elit. " +
		"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. " +
		"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris. "

	numParagraphs := contentSize / len(paragraphText)
	if numParagraphs < 1 {
		numParagraphs = 1
	}

	var paragraphs strings.Builder
	for i := 0; i < numParagraphs; i++ {
		paragraphs.WriteString(fmt.Sprintf("		<p>%s</p>\n", paragraphText))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><title>Benchmark Article</title></head>
<body>
	<article>
		<h1>Benchmark Article Title</h1>
%s
	</article>
</body>
</html>`, paragraphs.String())
}
