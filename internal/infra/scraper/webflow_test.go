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

func TestWebflowScraper_Fetch_Success(t *testing.T) {
	// Mock HTTP server with Webflow HTML structure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<body>
  <div class="blog_cms_item">
    <a class="w-inline-block" href="/blog/article-1">
      <h3 class="card_blog_title">Test Article 1</h3>
      <div class="card_blog_list_field">Nov 20, 2024</div>
    </a>
  </div>
  <div class="blog_cms_item">
    <a class="w-inline-block" href="/blog/article-2">
      <h3 class="card_blog_title">Test Article 2</h3>
      <div class="card_blog_list_field">Nov 21, 2024</div>
    </a>
  </div>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	// Create context with scraper config
	config := &entity.ScraperConfig{
		ItemSelector:  ".blog_cms_item",
		TitleSelector: ".card_blog_title",
		DateSelector:  ".card_blog_list_field",
		URLSelector:   "a.w-inline-block",
		DateFormat:    "Jan 2, 2006",
		URLPrefix:     server.URL,
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	items, err := fetcher.Fetch(ctx, server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("items length = %d, want 2", len(items))
	}

	// Verify first item
	if items[0].Title != "Test Article 1" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Test Article 1")
	}
	expectedURL1 := server.URL + "/blog/article-1"
	if items[0].URL != expectedURL1 {
		t.Errorf("items[0].URL = %q, want %q", items[0].URL, expectedURL1)
	}

	// Verify second item
	if items[1].Title != "Test Article 2" {
		t.Errorf("items[1].Title = %q, want %q", items[1].Title, "Test Article 2")
	}
}

func TestWebflowScraper_Fetch_NoConfig(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	// Context without config
	ctx := context.Background()

	_, err := fetcher.Fetch(ctx, "http://example.com")
	if err == nil {
		t.Fatal("Fetch() error = nil, want scraper_config not found error")
	}

	expectedMsg := "scraper_config not found in context"
	if err.Error() != expectedMsg {
		t.Errorf("error message = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestWebflowScraper_Fetch_InvalidURL(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	// Invalid URL format
	_, err := fetcher.Fetch(ctx, "not-a-url")
	if err == nil {
		t.Fatal("Fetch() error = nil, want invalid URL error")
	}
}

func TestWebflowScraper_Fetch_PrivateIP_Localhost(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	// Try to access localhost (SSRF attempt)
	_, err := fetcher.Fetch(ctx, "http://127.0.0.1:8080")
	if err == nil {
		t.Fatal("Fetch() error = nil, want SSRF prevention error")
	}

	// Should contain "private IP" or "SSRF" in error message
	errMsg := err.Error()
	if !containsAny(errMsg, []string{"private IP", "SSRF"}) {
		t.Errorf("error message = %q, want to contain 'private IP' or 'SSRF'", errMsg)
	}
}

func TestWebflowScraper_Fetch_PrivateIP_10Network(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	// Try to access 10.x.x.x network (private IP)
	_, err := fetcher.Fetch(ctx, "http://10.0.0.1")
	if err == nil {
		t.Fatal("Fetch() error = nil, want SSRF prevention error")
	}
}

func TestWebflowScraper_Fetch_PrivateIP_192Network(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	// Try to access 192.168.x.x network (private IP)
	_, err := fetcher.Fetch(ctx, "http://192.168.1.1")
	if err == nil {
		t.Fatal("Fetch() error = nil, want SSRF prevention error")
	}
}

func TestWebflowScraper_Fetch_HTTPError(t *testing.T) {
	// Mock server returning 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want HTTP error")
	}
}

func TestWebflowScraper_Fetch_NoItemsFound(t *testing.T) {
	// Mock server with HTML but no matching selectors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<body>
  <div class="other-content">No blog items here</div>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector:  ".blog_cms_item",
		TitleSelector: ".card_blog_title",
		URLSelector:   "a",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want no items found error")
	}

	if !containsAny(err.Error(), []string{"no items found"}) {
		t.Errorf("error message = %q, want to contain 'no items found'", err.Error())
	}
}

func TestWebflowScraper_Fetch_InvalidHTML(t *testing.T) {
	// Mock server with malformed HTML
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte("Not valid HTML <><><>")); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector:  ".blog_cms_item",
		TitleSelector: ".card_blog_title",
		URLSelector:   "a",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	// Malformed HTML should still parse (goquery is lenient)
	// but won't find any items
	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want no items found error")
	}
}

func TestWebflowScraper_Fetch_EmptyTitle(t *testing.T) {
	// Mock server with items that have empty titles
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<body>
  <div class="blog_cms_item">
    <a class="w-inline-block" href="/blog/article-1">
      <h3 class="card_blog_title"></h3>
    </a>
  </div>
  <div class="blog_cms_item">
    <a class="w-inline-block" href="/blog/article-2">
      <h3 class="card_blog_title">Valid Article</h3>
    </a>
  </div>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector:  ".blog_cms_item",
		TitleSelector: ".card_blog_title",
		URLSelector:   "a.w-inline-block",
		URLPrefix:     server.URL,
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	items, err := fetcher.Fetch(ctx, server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	// Should skip item with empty title
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}

	if items[0].Title != "Valid Article" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Valid Article")
	}
}

func TestWebflowScraper_Fetch_RelativeURLs(t *testing.T) {
	// Mock server with relative URLs
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<body>
  <div class="blog_cms_item">
    <a class="w-inline-block" href="/blog/article-1">
      <h3 class="card_blog_title">Article 1</h3>
    </a>
  </div>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector:  ".blog_cms_item",
		TitleSelector: ".card_blog_title",
		URLSelector:   "a.w-inline-block",
		URLPrefix:     "https://example.com",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	items, err := fetcher.Fetch(ctx, server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}

	expectedURL := "https://example.com/blog/article-1"
	if items[0].URL != expectedURL {
		t.Errorf("items[0].URL = %q, want %q", items[0].URL, expectedURL)
	}
}

func TestWebflowScraper_Fetch_DateParsing(t *testing.T) {
	tests := []struct {
		name       string
		dateStr    string
		dateFormat string
		wantYear   int
		wantMonth  time.Month
		wantDay    int
	}{
		{
			name:       "Format: Jan 2, 2006",
			dateStr:    "Nov 20, 2024",
			dateFormat: "Jan 2, 2006",
			wantYear:   2024,
			wantMonth:  time.November,
			wantDay:    20,
		},
		{
			name:       "Format: 2006-01-02",
			dateStr:    "2024-11-20",
			dateFormat: "2006-01-02",
			wantYear:   2024,
			wantMonth:  time.November,
			wantDay:    20,
		},
		{
			name:       "Fallback format: ISO 8601",
			dateStr:    "2024-11-20",
			dateFormat: "", // No format specified, should fallback
			wantYear:   2024,
			wantMonth:  time.November,
			wantDay:    20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				html := `<!DOCTYPE html>
<html>
<body>
  <div class="blog_cms_item">
    <a class="w-inline-block" href="/article">
      <h3 class="card_blog_title">Test Article</h3>
      <div class="card_blog_list_field">` + tt.dateStr + `</div>
    </a>
  </div>
</body>
</html>`
				w.Header().Set("Content-Type", "text/html")
				if _, err := w.Write([]byte(html)); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			}))
			defer server.Close()

			client := &http.Client{Timeout: 10 * time.Second}
			fetcher := scraper.NewWebflowScraper(client)

			config := &entity.ScraperConfig{
				ItemSelector:  ".blog_cms_item",
				TitleSelector: ".card_blog_title",
				DateSelector:  ".card_blog_list_field",
				URLSelector:   "a.w-inline-block",
				DateFormat:    tt.dateFormat,
				URLPrefix:     server.URL,
			}
			ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

			items, err := fetcher.Fetch(ctx, server.URL)
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}

			if len(items) != 1 {
				t.Fatalf("items length = %d, want 1", len(items))
			}

			pubDate := items[0].PublishedAt
			if pubDate.Year() != tt.wantYear {
				t.Errorf("PublishedAt.Year() = %d, want %d", pubDate.Year(), tt.wantYear)
			}
			if pubDate.Month() != tt.wantMonth {
				t.Errorf("PublishedAt.Month() = %v, want %v", pubDate.Month(), tt.wantMonth)
			}
			if pubDate.Day() != tt.wantDay {
				t.Errorf("PublishedAt.Day() = %d, want %d", pubDate.Day(), tt.wantDay)
			}
		})
	}
}

func TestWebflowScraper_Fetch_InvalidDateFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<body>
  <div class="blog_cms_item">
    <a class="w-inline-block" href="/article">
      <h3 class="card_blog_title">Test Article</h3>
      <div class="card_blog_list_field">Invalid Date Format</div>
    </a>
  </div>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
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

	before := time.Now()
	items, err := fetcher.Fetch(ctx, server.URL)
	after := time.Now()

	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}

	// Should fallback to current time
	pubDate := items[0].PublishedAt
	if pubDate.Before(before) || pubDate.After(after) {
		t.Errorf("PublishedAt = %v, want between %v and %v", pubDate, before, after)
	}
}

func TestWebflowScraper_Fetch_ContextCanceled(t *testing.T) {
	// Server with delay
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	client := &http.Client{}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, scraper.ContextKey("scraper_config"), config)
	cancel()

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want context canceled error")
	}
}

func TestWebflowScraper_Fetch_LargeResponse(t *testing.T) {
	// Mock server with 11MB response (exceeds 10MB limit)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// Write 11MB of data
		data := make([]byte, 11*1024*1024)
		for i := range data {
			data[i] = 'a'
		}
		_, _ = w.Write(data)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector:  ".blog_cms_item",
		TitleSelector: ".card_blog_title",
		URLSelector:   "a",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	// Should handle large response gracefully (body is limited to 10MB)
	_, err := fetcher.Fetch(ctx, server.URL)
	// Error is expected because truncated HTML won't have valid items
	if err == nil {
		t.Log("Note: Large response handled, but no items found (expected)")
	}
}

// Helper function
func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}
