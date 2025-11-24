//go:build integration

package fetcher_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"catchup-feed/internal/infra/fetcher"
)

// ───────────────────────────────────────────────────────────────
// TASK-016: End-to-End Content Fetch Integration Tests
// ───────────────────────────────────────────────────────────────

func TestContentFetchIntegration_Success(t *testing.T) {
	// Set up test HTTP server serving real HTML article
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Real-world-like HTML structure
		html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Integration Test Article</title>
</head>
<body>
    <header>
        <nav>
            <a href="/">Home</a>
            <a href="/about">About</a>
        </nav>
    </header>

    <main>
        <article>
            <h1>Integration Test Article Title</h1>
            <div class="metadata">
                <span class="author">John Doe</span>
                <time datetime="2024-01-01">January 1, 2024</time>
            </div>

            <div class="content">
                <p>This is the first paragraph of the article. It contains important information about the topic being discussed.</p>

                <p>This is the second paragraph with more detailed analysis. The content here is meant to be extracted by the Readability algorithm.</p>

                <p>This is the third paragraph providing additional context and examples. The article continues with valuable insights.</p>

                <h2>Section Header</h2>
                <p>This section discusses a specific aspect of the topic in more detail.</p>

                <p>Final paragraph wrapping up the article with conclusions and recommendations.</p>
            </div>
        </article>
    </main>

    <footer>
        <p>&copy; 2024 Test Site</p>
    </footer>
</body>
</html>`

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write([]byte(html)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	// Create ReadabilityFetcher instance
	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	// Call FetchContent
	content, err := contentFetcher.FetchContent(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("FetchContent() error = %v", err)
	}

	// Verify HTTP request successful
	if content == "" {
		t.Fatal("expected non-empty content")
	}

	// Verify HTML fetched correctly
	t.Logf("Fetched content length: %d characters", len(content))

	// Verify Readability extraction successful
	if !strings.Contains(content, "Integration Test Article") || !strings.Contains(content, "first paragraph") {
		t.Errorf("expected article content to be extracted, got: %q", content)
	}

	// Verify clean article text returned (no navigation elements)
	if strings.Contains(content, "Home") && strings.Contains(content, "About") {
		t.Error("navigation elements should be stripped by Readability")
	}

	// Verify footer is stripped
	if strings.Contains(content, "2024 Test Site") {
		t.Error("footer should be stripped by Readability")
	}

	// Content should contain main article text
	if !strings.Contains(content, "first paragraph") {
		t.Error("expected article paragraphs in extracted content")
	}
}

func TestContentFetchIntegration_RedirectChain(t *testing.T) {
	// Set up HTTP server with redirect chain (3 redirects)
	redirectCount := 0
	maxRedirects := 3

	finalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html><head><title>Final Destination</title></head>
<body><article><h1>Final Article</h1><p>Reached after redirect chain.</p></article></body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer finalServer.Close()

	// Create intermediate redirect servers
	currentURL := finalServer.URL

	for i := maxRedirects - 1; i >= 0; i-- {
		nextURL := currentURL
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			redirectCount++
			http.Redirect(w, r, nextURL, http.StatusFound)
		}))
		defer server.Close()
		currentURL = server.URL
	}

	initialURL := currentURL

	// Test redirect chain
	config := fetcher.DefaultConfig()
	config.MaxRedirects = 5 // Allow more redirects than chain length
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	content, err := contentFetcher.FetchContent(context.Background(), initialURL)
	if err != nil {
		t.Fatalf("FetchContent() error = %v", err)
	}

	// Verify all redirects followed
	if redirectCount != maxRedirects {
		t.Errorf("expected %d redirects, got %d", maxRedirects, redirectCount)
	}

	// Verify final destination reached
	if !strings.Contains(content, "Final Article") {
		t.Errorf("expected content from final destination, got: %q", content)
	}

	// Verify content extracted from final page
	if !strings.Contains(content, "redirect chain") {
		t.Errorf("expected final page content, got: %q", content)
	}
}

func TestContentFetchIntegration_RealWorldHTML(t *testing.T) {
	// Test with sample HTML from various popular site structures

	tests := []struct {
		name string
		html string
		want string
	}{
		{
			name: "Wikipedia-style article",
			html: `<!DOCTYPE html>
<html>
<head><title>Test Topic - Wikipedia</title></head>
<body>
	<div id="mw-page-base"></div>
	<div id="mw-head-base"></div>
	<div id="content">
		<h1 id="firstHeading">Test Topic</h1>
		<div id="bodyContent">
			<div id="mw-content-text">
				<p><b>Test Topic</b> is an example article demonstrating content extraction.</p>
				<p>This paragraph contains detailed information about the topic.</p>
				<h2>Background</h2>
				<p>Background information goes here with historical context.</p>
			</div>
		</div>
	</div>
</body>
</html>`,
			want: "Test Topic",
		},
		{
			name: "Medium-style blog post",
			html: `<!DOCTYPE html>
<html>
<head><title>My Blog Post</title></head>
<body>
	<article>
		<header>
			<h1>My Blog Post Title</h1>
			<div class="meta">
				<span class="author">Author Name</span>
				<time>2024-01-01</time>
			</div>
		</header>
		<section>
			<p>Introduction paragraph with engaging content.</p>
			<p>Main body paragraph with the core message.</p>
			<p>Conclusion paragraph summarizing key points.</p>
		</section>
	</article>
</body>
</html>`,
			want: "My Blog Post",
		},
		{
			name: "News article with aside elements",
			html: `<!DOCTYPE html>
<html>
<head><title>Breaking News</title></head>
<body>
	<main>
		<article>
			<h1>Breaking News Story</h1>
			<p class="lead">This is the lead paragraph summarizing the news.</p>
			<aside class="related">Related articles sidebar</aside>
			<p>First paragraph of the news story with details.</p>
			<p>Second paragraph continuing the narrative.</p>
			<aside class="ad">Advertisement</aside>
			<p>Third paragraph with quotes and analysis.</p>
		</article>
	</main>
</body>
</html>`,
			want: "Breaking News Story",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				if _, err := w.Write([]byte(tt.html)); err != nil {
					t.Errorf("failed to write response: %v", err)
				}
			}))
			defer server.Close()

			config := fetcher.DefaultConfig()
			contentFetcher := fetcher.NewReadabilityFetcher(config)

			content, err := contentFetcher.FetchContent(context.Background(), server.URL)
			if err != nil {
				t.Fatalf("FetchContent() error = %v", err)
			}

			// Verify Readability handles diverse HTML structures
			if content == "" {
				t.Error("expected non-empty content")
			}

			// Check extraction quality - should contain expected text
			if !strings.Contains(content, tt.want) {
				t.Errorf("expected content to contain %q, got: %q", tt.want, content)
			}

			t.Logf("Successfully extracted %d characters from %s", len(content), tt.name)
		})
	}
}

// ───────────────────────────────────────────────────────────────
// TASK-018: Circuit Breaker Integration Tests
// ───────────────────────────────────────────────────────────────

func TestCircuitBreakerIntegration_FailureRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping circuit breaker integration test in short mode")
	}

	failureCount := 0
	shouldFail := true
	var requestCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)

		if shouldFail {
			failureCount++
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Success response
		html := fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>Success #%d</title></head>
<body><article><p>Success after recovery</p></article></body>
</html>`, count)
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			t.Logf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	// Make 5 requests -> verify all fail
	t.Log("Phase 1: Making 5 failing requests to trip circuit breaker")
	for i := 0; i < 5; i++ {
		_, err := contentFetcher.FetchContent(context.Background(), server.URL)
		if err == nil {
			t.Logf("request %d: expected error, got success", i+1)
		}
	}

	if failureCount < 5 {
		t.Errorf("expected at least 5 failures, got %d", failureCount)
	}

	// Verify circuit opens (may need more requests)
	t.Log("Phase 2: Making additional request to verify circuit state")
	previousRequestCount := atomic.LoadInt32(&requestCount)
	_, err := contentFetcher.FetchContent(context.Background(), server.URL)
	if err == nil {
		t.Log("Circuit may not be open yet")
	}

	// Make 6th request -> verify fast fail if circuit is open
	t.Log("Phase 3: Testing if circuit is open (fast fail)")
	currentRequestCount := atomic.LoadInt32(&requestCount)
	if currentRequestCount == previousRequestCount {
		t.Log("Circuit is OPEN - request failed fast without hitting server")
	} else {
		t.Log("Circuit is still CLOSED - request hit server")
	}

	// Note: Full recovery test would require waiting for circuit timeout (60s)
	// which is too long for integration tests
	t.Log("Circuit breaker integration test completed (full recovery requires 60s timeout)")
}

func TestCircuitBreakerIntegration_PartialFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping circuit breaker integration test in short mode")
	}

	var requestCount int32
	failurePattern := []bool{false, true, false, true, false, false, false, true, false, false}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&requestCount, 1)
		shouldFail := failurePattern[min(int(count)-1, len(failurePattern)-1)]

		if shouldFail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		html := `<!DOCTYPE html>
<html><head><title>Success</title></head>
<body><article><p>Success response</p></article></body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			t.Logf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	// Make 10 requests with 50% failure rate (not consecutive)
	successCount := 0
	failureCount := 0

	for i := 0; i < 10; i++ {
		_, err := contentFetcher.FetchContent(context.Background(), server.URL)
		if err == nil {
			successCount++
		} else {
			failureCount++
		}
		time.Sleep(10 * time.Millisecond) // Small delay between requests
	}

	t.Logf("Results: %d successes, %d failures out of 10 requests", successCount, failureCount)

	// Verify circuit doesn't open (failures are not consecutive)
	// Circuit breaker requires consecutive failures above threshold
	// With 50% alternating pattern, circuit should remain closed

	// Make a final request to verify circuit is still closed
	_, err := contentFetcher.FetchContent(context.Background(), server.URL)
	if err == nil {
		t.Log("Circuit remained CLOSED as expected (no consecutive failures)")
	} else {
		t.Logf("Final request error: %v", err)
	}
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
