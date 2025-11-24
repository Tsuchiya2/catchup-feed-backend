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

func TestNextJSScraper_Fetch_Success(t *testing.T) {
	// Mock HTTP server with Next.js __NEXT_DATA__ structure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script id="__NEXT_DATA__" type="application/json">
  {
    "props": {
      "pageProps": {
        "initialSeedData": {
          "items": [
            {
              "title": "Next.js Article 1",
              "slug": "article-1",
              "publishedOn": "2024-11-20T10:00:00Z",
              "summary": "Summary of article 1"
            },
            {
              "title": "Next.js Article 2",
              "slug": "article-2",
              "publishedOn": "2024-11-21T10:00:00Z",
              "summary": "Summary of article 2"
            }
          ]
        }
      }
    }
  }
  </script>
</head>
<body></body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey:   "initialSeedData",
		URLPrefix: "https://example.com/news/",
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
	if items[0].Title != "Next.js Article 1" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Next.js Article 1")
	}
	expectedURL1 := "https://example.com/news/article-1"
	if items[0].URL != expectedURL1 {
		t.Errorf("items[0].URL = %q, want %q", items[0].URL, expectedURL1)
	}
	if items[0].Content != "Summary of article 1" {
		t.Errorf("items[0].Content = %q, want %q", items[0].Content, "Summary of article 1")
	}

	// Verify second item
	if items[1].Title != "Next.js Article 2" {
		t.Errorf("items[1].Title = %q, want %q", items[1].Title, "Next.js Article 2")
	}
}

func TestNextJSScraper_Fetch_NoConfig(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

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

func TestNextJSScraper_Fetch_MissingScript(t *testing.T) {
	// Mock server without __NEXT_DATA__ script tag
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head></head>
<body>No Next.js data here</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey: "initialSeedData",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want __NEXT_DATA__ not found error")
	}

	if !containsAny(err.Error(), []string{"__NEXT_DATA__", "not found"}) {
		t.Errorf("error message = %q, want to contain '__NEXT_DATA__' or 'not found'", err.Error())
	}
}

func TestNextJSScraper_Fetch_InvalidJSON(t *testing.T) {
	// Mock server with malformed JSON in __NEXT_DATA__
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script id="__NEXT_DATA__" type="application/json">
  {invalid json here}
  </script>
</head>
<body></body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey: "initialSeedData",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want JSON parse error")
	}
}

func TestNextJSScraper_Fetch_MissingFields(t *testing.T) {
	// Mock server with items missing title or slug
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script id="__NEXT_DATA__" type="application/json">
  {
    "props": {
      "pageProps": {
        "initialSeedData": {
          "items": [
            {
              "slug": "article-1",
              "publishedOn": "2024-11-20T10:00:00Z"
            },
            {
              "title": "Valid Article",
              "slug": "article-2",
              "publishedOn": "2024-11-21T10:00:00Z"
            }
          ]
        }
      }
    }
  }
  </script>
</head>
<body></body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey:   "initialSeedData",
		URLPrefix: "https://example.com/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	items, err := fetcher.Fetch(ctx, server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	// Should skip item without title
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}

	if items[0].Title != "Valid Article" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Valid Article")
	}
}

func TestNextJSScraper_Fetch_MissingSlug(t *testing.T) {
	// Mock server with items missing slug
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script id="__NEXT_DATA__" type="application/json">
  {
    "props": {
      "pageProps": {
        "initialSeedData": {
          "items": [
            {
              "title": "Article without slug",
              "publishedOn": "2024-11-20T10:00:00Z"
            }
          ]
        }
      }
    }
  }
  </script>
</head>
<body></body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey:   "initialSeedData",
		URLPrefix: "https://example.com/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want no items error")
	}

	// Should error because all items are skipped
	if !containsAny(err.Error(), []string{"no items"}) {
		t.Errorf("error message = %q, want to contain 'no items'", err.Error())
	}
}

func TestNextJSScraper_Fetch_BuildURL(t *testing.T) {
	tests := []struct {
		name        string
		slug        string
		urlPrefix   string
		expectedURL string
	}{
		{
			name:        "With trailing slash prefix",
			slug:        "article-1",
			urlPrefix:   "https://example.com/news/",
			expectedURL: "https://example.com/news/article-1",
		},
		{
			name:        "Without trailing slash prefix",
			slug:        "article-1",
			urlPrefix:   "https://example.com/news",
			expectedURL: "https://example.com/news/article-1",
		},
		{
			name:        "With leading slash slug",
			slug:        "/article-1",
			urlPrefix:   "https://example.com/news",
			expectedURL: "https://example.com/news/article-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				html := `<!DOCTYPE html>
<html>
<head>
  <script id="__NEXT_DATA__" type="application/json">
  {
    "props": {
      "pageProps": {
        "initialSeedData": {
          "items": [
            {
              "title": "Test Article",
              "slug": "` + tt.slug + `",
              "publishedOn": "2024-11-20T10:00:00Z"
            }
          ]
        }
      }
    }
  }
  </script>
</head>
<body></body>
</html>`
				w.Header().Set("Content-Type", "text/html")
				if _, err := w.Write([]byte(html)); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			}))
			defer server.Close()

			client := &http.Client{Timeout: 10 * time.Second}
			fetcher := scraper.NewNextJSScraper(client)

			config := &entity.ScraperConfig{
				DataKey:   "initialSeedData",
				URLPrefix: tt.urlPrefix,
			}
			ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

			items, err := fetcher.Fetch(ctx, server.URL)
			if err != nil {
				t.Fatalf("Fetch() error = %v", err)
			}

			if len(items) != 1 {
				t.Fatalf("items length = %d, want 1", len(items))
			}

			if items[0].URL != tt.expectedURL {
				t.Errorf("URL = %q, want %q", items[0].URL, tt.expectedURL)
			}
		})
	}
}

func TestNextJSScraper_Fetch_DateParsing(t *testing.T) {
	tests := []struct {
		name        string
		publishedOn string
		wantYear    int
		wantMonth   time.Month
		wantDay     int
	}{
		{
			name:        "RFC3339 format",
			publishedOn: "2024-11-20T10:30:00Z",
			wantYear:    2024,
			wantMonth:   time.November,
			wantDay:     20,
		},
		{
			name:        "ISO 8601 date only",
			publishedOn: "2024-11-20",
			wantYear:    2024,
			wantMonth:   time.November,
			wantDay:     20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				html := `<!DOCTYPE html>
<html>
<head>
  <script id="__NEXT_DATA__" type="application/json">
  {
    "props": {
      "pageProps": {
        "initialSeedData": {
          "items": [
            {
              "title": "Test Article",
              "slug": "article-1",
              "publishedOn": "` + tt.publishedOn + `"
            }
          ]
        }
      }
    }
  }
  </script>
</head>
<body></body>
</html>`
				w.Header().Set("Content-Type", "text/html")
				if _, err := w.Write([]byte(html)); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			}))
			defer server.Close()

			client := &http.Client{Timeout: 10 * time.Second}
			fetcher := scraper.NewNextJSScraper(client)

			config := &entity.ScraperConfig{
				DataKey:   "initialSeedData",
				URLPrefix: "https://example.com/",
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

func TestNextJSScraper_Fetch_MissingProps(t *testing.T) {
	// Mock server with JSON missing props
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script id="__NEXT_DATA__" type="application/json">
  {
    "notProps": {}
  }
  </script>
</head>
<body></body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey: "initialSeedData",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want props not found error")
	}

	if !containsAny(err.Error(), []string{"props", "not found"}) {
		t.Errorf("error message = %q, want to contain 'props' and 'not found'", err.Error())
	}
}

func TestNextJSScraper_Fetch_PrivateIP(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey: "initialSeedData",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	// Try to access localhost (SSRF attempt)
	_, err := fetcher.Fetch(ctx, "http://127.0.0.1:8080")
	if err == nil {
		t.Fatal("Fetch() error = nil, want SSRF prevention error")
	}

	if !containsAny(err.Error(), []string{"private IP", "SSRF"}) {
		t.Errorf("error message = %q, want to contain 'private IP' or 'SSRF'", err.Error())
	}
}

func TestNextJSScraper_Fetch_HTTPError(t *testing.T) {
	// Mock server returning 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey: "initialSeedData",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want HTTP error")
	}
}

func TestNextJSScraper_Fetch_AbsoluteURLSlug(t *testing.T) {
	// Test that absolute URL in slug is used as-is
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script id="__NEXT_DATA__" type="application/json">
  {
    "props": {
      "pageProps": {
        "initialSeedData": {
          "items": [
            {
              "title": "External Article",
              "slug": "https://external.com/article",
              "publishedOn": "2024-11-20T10:00:00Z"
            }
          ]
        }
      }
    }
  }
  </script>
</head>
<body></body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey:   "initialSeedData",
		URLPrefix: "https://example.com/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	items, err := fetcher.Fetch(ctx, server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}

	// Absolute URL should be used as-is
	if items[0].URL != "https://external.com/article" {
		t.Errorf("URL = %q, want %q", items[0].URL, "https://external.com/article")
	}
}
