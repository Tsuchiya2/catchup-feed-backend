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

// TestWebflowScraper_Integration_ClaudeBlog tests end-to-end Webflow scraping
// with realistic HTML structure from Claude Blog
func TestWebflowScraper_Integration_ClaudeBlog(t *testing.T) {
	// Mock server with realistic Claude Blog HTML
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Claude Blog</title>
</head>
<body>
    <div class="blog_list">
        <div class="blog_cms_item">
            <a href="/blog/introducing-claude-3" class="w-inline-block">
                <div class="card_blog_content">
                    <h3 class="card_blog_title">Introducing Claude 3</h3>
                    <p class="card_blog_excerpt">Our most capable AI model yet</p>
                    <div class="card_blog_list_field">Mar 4, 2024</div>
                </div>
            </a>
        </div>
        <div class="blog_cms_item">
            <a href="/blog/prompt-engineering-guide" class="w-inline-block">
                <div class="card_blog_content">
                    <h3 class="card_blog_title">Prompt Engineering Guide</h3>
                    <p class="card_blog_excerpt">Best practices for working with Claude</p>
                    <div class="card_blog_list_field">Feb 15, 2024</div>
                </div>
            </a>
        </div>
        <div class="blog_cms_item">
            <a href="/blog/safety-and-alignment" class="w-inline-block">
                <div class="card_blog_content">
                    <h3 class="card_blog_title">Safety and Alignment</h3>
                    <p class="card_blog_excerpt">How we build safe AI systems</p>
                    <div class="card_blog_list_field">Jan 20, 2024</div>
                </div>
            </a>
        </div>
    </div>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
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
		URLPrefix:     "https://www.claude.com",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	items, err := fetcher.Fetch(ctx, server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("items length = %d, want 3", len(items))
	}

	// Verify first article
	if items[0].Title != "Introducing Claude 3" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Introducing Claude 3")
	}
	if items[0].URL != "https://www.claude.com/blog/introducing-claude-3" {
		t.Errorf("items[0].URL = %q", items[0].URL)
	}
	if items[0].PublishedAt.Month() != time.March {
		t.Errorf("items[0].PublishedAt.Month() = %v, want March", items[0].PublishedAt.Month())
	}

	// Verify all items have valid data
	for i, item := range items {
		if item.Title == "" {
			t.Errorf("items[%d].Title is empty", i)
		}
		if item.URL == "" {
			t.Errorf("items[%d].URL is empty", i)
		}
		if item.PublishedAt.IsZero() {
			t.Errorf("items[%d].PublishedAt is zero", i)
		}
	}
}

// TestNextJSScraper_Integration_AnthropicNews tests end-to-end NextJS scraping
// with realistic __NEXT_DATA__ structure from Anthropic News
func TestNextJSScraper_Integration_AnthropicNews(t *testing.T) {
	// Mock server with realistic Anthropic News structure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
    <title>Anthropic News</title>
    <script id="__NEXT_DATA__" type="application/json">
    {
        "props": {
            "pageProps": {
                "initialSeedData": {
                    "items": [
                        {
                            "id": "news-1",
                            "title": "Anthropic Raises $500M Series C",
                            "slug": "series-c-announcement",
                            "publishedOn": "2024-03-15T09:00:00Z",
                            "summary": "We're excited to announce our Series C funding round",
                            "category": "Company News"
                        },
                        {
                            "id": "news-2",
                            "title": "Constitutional AI Paper Published",
                            "slug": "constitutional-ai-paper",
                            "publishedOn": "2024-02-28T14:30:00Z",
                            "summary": "Our research on training helpful, harmless, and honest AI",
                            "category": "Research"
                        },
                        {
                            "id": "news-3",
                            "title": "Partnership with Enterprise Customers",
                            "slug": "enterprise-partnerships",
                            "publishedOn": "2024-01-10T11:00:00Z",
                            "summary": "Expanding Claude's reach in the enterprise",
                            "category": "Partnerships"
                        }
                    ]
                }
            }
        },
        "page": "/news",
        "query": {},
        "buildId": "abc123"
    }
    </script>
</head>
<body>
    <div id="__next"></div>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey:   "initialSeedData",
		URLPrefix: "https://www.anthropic.com/news/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	items, err := fetcher.Fetch(ctx, server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("items length = %d, want 3", len(items))
	}

	// Verify first article
	if items[0].Title != "Anthropic Raises $500M Series C" {
		t.Errorf("items[0].Title = %q", items[0].Title)
	}
	expectedURL := "https://www.anthropic.com/news/series-c-announcement"
	if items[0].URL != expectedURL {
		t.Errorf("items[0].URL = %q, want %q", items[0].URL, expectedURL)
	}
	if items[0].Content != "We're excited to announce our Series C funding round" {
		t.Errorf("items[0].Content = %q", items[0].Content)
	}

	// Verify date parsing
	if items[0].PublishedAt.Year() != 2024 {
		t.Errorf("items[0].PublishedAt.Year() = %d, want 2024", items[0].PublishedAt.Year())
	}
	if items[0].PublishedAt.Month() != time.March {
		t.Errorf("items[0].PublishedAt.Month() = %v, want March", items[0].PublishedAt.Month())
	}

	// Verify all items have valid data
	for i, item := range items {
		if item.Title == "" {
			t.Errorf("items[%d].Title is empty", i)
		}
		if item.URL == "" {
			t.Errorf("items[%d].URL is empty", i)
		}
	}
}

// TestRemixScraper_Integration_PythonWeekly tests end-to-end Remix scraping
// with realistic window.__remixContext structure from Python Weekly
func TestRemixScraper_Integration_PythonWeekly(t *testing.T) {
	// Mock server with realistic Python Weekly structure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html lang="en">
<head>
    <title>Python Weekly</title>
    <script>
    window.__remixContext = {
        "url": "/",
        "state": {
            "loaderData": {}
        },
        "routes": {
            "routes/($lang)._layout._index": {
                "id": "routes/($lang)._layout._index",
                "module": "/build/routes/index.js",
                "loaderData": {
                    "issues": [
                        {
                            "id": "issue-680",
                            "web_title": "Python Weekly Issue #680",
                            "slug": "680",
                            "override_scheduled_at": "2024-11-21T12:00:00Z",
                            "description": "Latest Python news, articles, and projects"
                        },
                        {
                            "id": "issue-679",
                            "web_title": "Python Weekly Issue #679",
                            "slug": "679",
                            "override_scheduled_at": "2024-11-14T12:00:00Z",
                            "description": "Weekly Python roundup"
                        },
                        {
                            "id": "issue-678",
                            "web_title": "Python Weekly Issue #678",
                            "slug": "678",
                            "override_scheduled_at": "2024-11-07T12:00:00Z",
                            "description": "Python community highlights"
                        }
                    ]
                }
            }
        }
    };
    </script>
</head>
<body>
    <div id="root"></div>
</body>
</html>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/($lang)._layout._index",
		URLPrefix:  "https://www.pythonweekly.com/issues/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	items, err := fetcher.Fetch(ctx, server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 3 {
		t.Fatalf("items length = %d, want 3", len(items))
	}

	// Verify first issue
	if items[0].Title != "Python Weekly Issue #680" {
		t.Errorf("items[0].Title = %q", items[0].Title)
	}
	expectedURL := "https://www.pythonweekly.com/issues/680"
	if items[0].URL != expectedURL {
		t.Errorf("items[0].URL = %q, want %q", items[0].URL, expectedURL)
	}

	// Verify date parsing
	if items[0].PublishedAt.Year() != 2024 {
		t.Errorf("items[0].PublishedAt.Year() = %d, want 2024", items[0].PublishedAt.Year())
	}

	// Verify all issues have valid data
	for i, item := range items {
		if item.Title == "" {
			t.Errorf("items[%d].Title is empty", i)
		}
		if item.URL == "" {
			t.Errorf("items[%d].URL is empty", i)
		}
		if !item.PublishedAt.After(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("items[%d].PublishedAt = %v, want after 2024-01-01", i, item.PublishedAt)
		}
	}
}

// TestScraperFactory_Integration tests the scraper factory creation
func TestScraperFactory_Integration(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	factory := scraper.NewScraperFactory(client)

	scrapers := factory.CreateScrapers()

	// Verify all scraper types are created
	expectedTypes := []string{"Webflow", "NextJS", "Remix"}
	for _, scraperType := range expectedTypes {
		if _, exists := scrapers[scraperType]; !exists {
			t.Errorf("scraper type %q not found in factory", scraperType)
		}
	}

	// Verify each scraper implements FeedFetcher interface
	for scraperType, s := range scrapers {
		if s == nil {
			t.Errorf("scraper %q is nil", scraperType)
		}
	}
}

// TestContextPropagation tests that context is properly propagated
func TestContextPropagation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay to test context cancellation
		time.Sleep(1 * time.Second)
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ctx = context.WithValue(ctx, scraper.ContextKey("scraper_config"), config)

	_, err := fetcher.Fetch(ctx, server.URL)

	// Should timeout
	if err == nil {
		t.Fatal("Fetch() error = nil, want context deadline exceeded")
	}

	if !containsAny(err.Error(), []string{"context", "deadline", "canceled"}) {
		t.Logf("Note: error = %q (expected context-related error)", err.Error())
	}
}
