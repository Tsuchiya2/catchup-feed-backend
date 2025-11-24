package scraper_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/infra/scraper"
)

// BenchmarkWebflowScraper_Fetch benchmarks Webflow scraper performance
func BenchmarkWebflowScraper_Fetch(b *testing.B) {
	// Mock server with realistic HTML size (~50KB)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := generateLargeWebflowHTML(10) // 10 articles
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector:  ".blog_cms_item",
		TitleSelector: ".card_blog_title",
		DateSelector:  ".card_blog_list_field",
		URLSelector:   "a.w-inline-block",
		DateFormat:    "Jan 2, 2006",
		URLPrefix:     server.URL,
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := fetcher.Fetch(ctx, server.URL)
		if err != nil {
			b.Fatalf("Fetch() error = %v", err)
		}
	}
}

// BenchmarkNextJSScraper_Fetch benchmarks NextJS scraper performance
func BenchmarkNextJSScraper_Fetch(b *testing.B) {
	// Mock server with realistic JSON size (~100KB)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := generateLargeNextJSHTML(20) // 20 articles
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey:   "initialSeedData",
		URLPrefix: "https://example.com/news/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := fetcher.Fetch(ctx, server.URL)
		if err != nil {
			b.Fatalf("Fetch() error = %v", err)
		}
	}
}

// BenchmarkRemixScraper_Fetch benchmarks Remix scraper performance
func BenchmarkRemixScraper_Fetch(b *testing.B) {
	// Mock server with realistic JSON size
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := generateLargeRemixHTML(15) // 15 issues
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/($lang)._layout._index",
		URLPrefix:  "https://example.com/issues/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := fetcher.Fetch(ctx, server.URL)
		if err != nil {
			b.Fatalf("Fetch() error = %v", err)
		}
	}
}

// TestWebflowScraper_LatencyP95 measures P95 latency for Webflow scraper
func TestWebflowScraper_LatencyP95(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping latency test in short mode")
	}

	// Mock server with realistic network delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate network latency (50-200ms)
		time.Sleep(100 * time.Millisecond)

		html := generateLargeWebflowHTML(10)
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector:  ".blog_cms_item",
		TitleSelector: ".card_blog_title",
		DateSelector:  ".card_blog_list_field",
		URLSelector:   "a.w-inline-block",
		DateFormat:    "Jan 2, 2006",
		URLPrefix:     server.URL,
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	// Run 100 iterations to calculate P95
	iterations := 100
	latencies := make([]time.Duration, iterations)

	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, err := fetcher.Fetch(ctx, server.URL)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Fetch() error = %v", err)
		}

		latencies[i] = elapsed
	}

	// Calculate P95 (95th percentile)
	p95Index := int(float64(iterations) * 0.95)
	// Sort latencies (simple bubble sort for test)
	for i := 0; i < len(latencies); i++ {
		for j := i + 1; j < len(latencies); j++ {
			if latencies[i] > latencies[j] {
				latencies[i], latencies[j] = latencies[j], latencies[i]
			}
		}
	}

	p95Latency := latencies[p95Index]
	t.Logf("P95 latency: %v", p95Latency)

	// P95 should be under 5 seconds (requirement)
	if p95Latency > 5*time.Second {
		t.Errorf("P95 latency = %v, want < 5s", p95Latency)
	}
}

// TestNextJSScraper_LatencyP95 measures P95 latency for NextJS scraper
func TestNextJSScraper_LatencyP95(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping latency test in short mode")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)

		html := generateLargeNextJSHTML(20)
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey:   "initialSeedData",
		URLPrefix: "https://example.com/news/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	iterations := 100
	latencies := make([]time.Duration, iterations)

	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, err := fetcher.Fetch(ctx, server.URL)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Fetch() error = %v", err)
		}

		latencies[i] = elapsed
	}

	// Calculate P95
	p95Index := int(float64(iterations) * 0.95)
	for i := 0; i < len(latencies); i++ {
		for j := i + 1; j < len(latencies); j++ {
			if latencies[i] > latencies[j] {
				latencies[i], latencies[j] = latencies[j], latencies[i]
			}
		}
	}

	p95Latency := latencies[p95Index]
	t.Logf("P95 latency: %v", p95Latency)

	if p95Latency > 5*time.Second {
		t.Errorf("P95 latency = %v, want < 5s", p95Latency)
	}
}

// TestRemixScraper_LatencyP95 measures P95 latency for Remix scraper
func TestRemixScraper_LatencyP95(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping latency test in short mode")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)

		html := generateLargeRemixHTML(15)
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/($lang)._layout._index",
		URLPrefix:  "https://example.com/issues/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	iterations := 100
	latencies := make([]time.Duration, iterations)

	for i := 0; i < iterations; i++ {
		start := time.Now()
		_, err := fetcher.Fetch(ctx, server.URL)
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("Fetch() error = %v", err)
		}

		latencies[i] = elapsed
	}

	// Calculate P95
	p95Index := int(float64(iterations) * 0.95)
	for i := 0; i < len(latencies); i++ {
		for j := i + 1; j < len(latencies); j++ {
			if latencies[i] > latencies[j] {
				latencies[i], latencies[j] = latencies[j], latencies[i]
			}
		}
	}

	p95Latency := latencies[p95Index]
	t.Logf("P95 latency: %v", p95Latency)

	if p95Latency > 5*time.Second {
		t.Errorf("P95 latency = %v, want < 5s", p95Latency)
	}
}

// Helper functions to generate realistic HTML

func generateLargeWebflowHTML(itemCount int) string {
	html := `<!DOCTYPE html><html><head><title>Blog</title></head><body><div class="blog_list">`

	for i := 0; i < itemCount; i++ {
		html += `
		<div class="blog_cms_item">
			<a href="/blog/article-` + string(rune('0'+i%10)) + `" class="w-inline-block">
				<div class="card_blog_content">
					<h3 class="card_blog_title">Test Article ` + string(rune('0'+i%10)) + `</h3>
					<p class="card_blog_excerpt">This is a test article excerpt with some content to make it realistic. Lorem ipsum dolor sit amet, consectetur adipiscing elit.</p>
					<div class="card_blog_list_field">Nov 20, 2024</div>
				</div>
			</a>
		</div>`
	}

	html += `</div></body></html>`
	return html
}

func generateLargeNextJSHTML(itemCount int) string {
	html := `<!DOCTYPE html><html><head><title>News</title><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"initialSeedData":{"items":[`

	for i := 0; i < itemCount; i++ {
		if i > 0 {
			html += `,`
		}
		html += `{"title":"Article ` + string(rune('0'+i%10)) + `","slug":"article-` + string(rune('0'+i%10)) + `","publishedOn":"2024-11-20T10:00:00Z","summary":"Test summary for article ` + string(rune('0'+i%10)) + ` with some content"}`
	}

	html += `]}}},"page":"/news","query":{},"buildId":"test"}</script></head><body><div id="__next"></div></body></html>`
	return html
}

func generateLargeRemixHTML(itemCount int) string {
	html := `<!DOCTYPE html><html><head><title>Issues</title><script>window.__remixContext={"routes":{"routes/($lang)._layout._index":{"loaderData":{"issues":[`

	for i := 0; i < itemCount; i++ {
		if i > 0 {
			html += `,`
		}
		html += `{"web_title":"Issue ` + string(rune('0'+i%10)) + `","slug":"issue-` + string(rune('0'+i%10)) + `","override_scheduled_at":"2024-11-20T10:00:00Z","description":"Test issue description"}`
	}

	html += `]}}}};</script></head><body><div id="root"></div></body></html>`
	return html
}
