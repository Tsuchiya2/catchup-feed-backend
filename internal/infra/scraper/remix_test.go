package scraper_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/infra/scraper"
)

func TestRemixScraper_Fetch_Success(t *testing.T) {
	// Mock HTTP server with Remix window.__remixContext structure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script>
  window.__remixContext = {
    "routes": {
      "routes/($lang)._layout._index": {
        "loaderData": {
          "issues": [
            {
              "web_title": "Python Weekly Issue #1",
              "slug": "issue-1",
              "override_scheduled_at": "2024-11-20T10:00:00Z"
            },
            {
              "web_title": "Python Weekly Issue #2",
              "slug": "issue-2",
              "override_scheduled_at": "2024-11-21T10:00:00Z"
            }
          ]
        }
      }
    }
  };
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
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/($lang)._layout._index",
		URLPrefix:  "https://pythonweekly.com/issues/",
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
	if items[0].Title != "Python Weekly Issue #1" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Python Weekly Issue #1")
	}
	expectedURL1 := "https://pythonweekly.com/issues/issue-1"
	if items[0].URL != expectedURL1 {
		t.Errorf("items[0].URL = %q, want %q", items[0].URL, expectedURL1)
	}

	// Verify second item
	if items[1].Title != "Python Weekly Issue #2" {
		t.Errorf("items[1].Title = %q, want %q", items[1].Title, "Python Weekly Issue #2")
	}
}

func TestRemixScraper_Fetch_NoConfig(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRemixScraper(client)

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

func TestRemixScraper_Fetch_MissingContext(t *testing.T) {
	// Mock server without window.__remixContext
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head></head>
<body>No Remix context here</body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/test",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want __remixContext not found error")
	}

	if !containsAny(err.Error(), []string{"__remixContext", "not found"}) {
		t.Errorf("error message = %q, want to contain '__remixContext' or 'not found'", err.Error())
	}
}

func TestRemixScraper_Fetch_InvalidJSON(t *testing.T) {
	// Mock server with malformed JSON in __remixContext
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script>
  window.__remixContext = {invalid json};
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
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/test",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want JSON parse error")
	}
}

func TestRemixScraper_ExtractRemixContext_WithWhitespace(t *testing.T) {
	// Test regex with various whitespace patterns
	tests := []struct {
		name    string
		html    string
		wantErr bool
	}{
		{
			name: "Normal spacing",
			html: `<script>window.__remixContext = {"test": "value"};</script>`,
			wantErr: false,
		},
		{
			name: "Extra whitespace",
			html: `<script>window.__remixContext   =   {"test": "value"};</script>`,
			wantErr: false,
		},
		{
			name: "No whitespace",
			html: `<script>window.__remixContext={"test":"value"};</script>`,
			wantErr: false,
		},
		{
			name: "Newlines",
			html: `<script>
window.__remixContext = {
  "test": "value"
};
</script>`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				if _, err := w.Write([]byte(tt.html)); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
			}))
			defer server.Close()

			client := &http.Client{Timeout: 10 * time.Second}
			fetcher := scraper.NewRemixScraper(client)

			// Create minimal valid structure
			config := &entity.ScraperConfig{
				ContextKey: "",
			}
			ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

			// This should fail at a later stage (no routes found),
			// but regex extraction should succeed
			_, err := fetcher.Fetch(ctx, server.URL)

			// We expect an error, but not from regex extraction
			if tt.wantErr {
				if err == nil {
					t.Fatal("Fetch() error = nil, want error")
				}
			} else {
				// Should fail at routes parsing (expected), not regex extraction
				if err != nil && strings.Contains(err.Error(), "__remixContext not found") {
					t.Errorf("Failed at regex extraction: %v", err)
				}
				// It's OK if it fails at "routes not found" - that's after regex extraction
				if err != nil && !strings.Contains(err.Error(), "routes not found") &&
					!strings.Contains(err.Error(), "no route with loaderData found") {
					t.Logf("Passed regex extraction (got expected error: %v)", err)
				}
			}
		})
	}
}

func TestRemixScraper_Fetch_MissingTitle(t *testing.T) {
	// Mock server with issue missing web_title
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script>
  window.__remixContext = {
    "routes": {
      "routes/test": {
        "loaderData": {
          "issues": [
            {
              "slug": "issue-1",
              "override_scheduled_at": "2024-11-20T10:00:00Z"
            },
            {
              "web_title": "Valid Issue",
              "slug": "issue-2",
              "override_scheduled_at": "2024-11-21T10:00:00Z"
            }
          ]
        }
      }
    }
  };
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
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/test",
		URLPrefix:  "https://example.com/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	items, err := fetcher.Fetch(ctx, server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	// Should skip issue without title
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}

	if items[0].Title != "Valid Issue" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Valid Issue")
	}
}

func TestRemixScraper_Fetch_MissingSlug(t *testing.T) {
	// Mock server with issue missing slug
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script>
  window.__remixContext = {
    "routes": {
      "routes/test": {
        "loaderData": {
          "issues": [
            {
              "web_title": "Issue without slug",
              "override_scheduled_at": "2024-11-20T10:00:00Z"
            }
          ]
        }
      }
    }
  };
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
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/test",
		URLPrefix:  "https://example.com/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want no issues error")
	}

	// Should error because all issues are skipped
	if !containsAny(err.Error(), []string{"no issues"}) {
		t.Errorf("error message = %q, want to contain 'no issues'", err.Error())
	}
}

func TestRemixScraper_Fetch_AutoDetectRoute(t *testing.T) {
	// Test auto-detection when ContextKey is empty
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script>
  window.__remixContext = {
    "routes": {
      "routes/auto-detected": {
        "loaderData": {
          "issues": [
            {
              "web_title": "Auto-detected Issue",
              "slug": "issue-1",
              "override_scheduled_at": "2024-11-20T10:00:00Z"
            }
          ]
        }
      }
    }
  };
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
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "", // Empty - should auto-detect
		URLPrefix:  "https://example.com/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	items, err := fetcher.Fetch(ctx, server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}

	if items[0].Title != "Auto-detected Issue" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Auto-detected Issue")
	}
}

func TestRemixScraper_Fetch_DateParsing(t *testing.T) {
	tests := []struct {
		name                 string
		overrideScheduledAt  string
		wantYear             int
		wantMonth            time.Month
		wantDay              int
	}{
		{
			name:                "RFC3339 format",
			overrideScheduledAt: "2024-11-20T10:30:00Z",
			wantYear:            2024,
			wantMonth:           time.November,
			wantDay:             20,
		},
		{
			name:                "ISO 8601 date only",
			overrideScheduledAt: "2024-11-20",
			wantYear:            2024,
			wantMonth:           time.November,
			wantDay:             20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				html := `<!DOCTYPE html>
<html>
<head>
  <script>
  window.__remixContext = {
    "routes": {
      "routes/test": {
        "loaderData": {
          "issues": [
            {
              "web_title": "Test Issue",
              "slug": "issue-1",
              "override_scheduled_at": "` + tt.overrideScheduledAt + `"
            }
          ]
        }
      }
    }
  };
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
			fetcher := scraper.NewRemixScraper(client)

			config := &entity.ScraperConfig{
				ContextKey: "routes/test",
				URLPrefix:  "https://example.com/",
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

func TestRemixScraper_Fetch_MissingRoutes(t *testing.T) {
	// Mock server with JSON missing routes
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script>
  window.__remixContext = {
    "notRoutes": {}
  };
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
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/test",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want routes not found error")
	}

	if !containsAny(err.Error(), []string{"routes", "not found"}) {
		t.Errorf("error message = %q, want to contain 'routes' and 'not found'", err.Error())
	}
}

func TestRemixScraper_Fetch_PrivateIP(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/test",
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

func TestRemixScraper_Fetch_HTTPError(t *testing.T) {
	// Mock server returning 500 error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/test",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want HTTP error")
	}
}

func TestRemixScraper_Fetch_InvalidContextKey(t *testing.T) {
	// Mock server with valid JSON but specified route doesn't exist
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html>
<head>
  <script>
  window.__remixContext = {
    "routes": {
      "routes/existing": {
        "loaderData": {
          "issues": [
            {
              "web_title": "Test Issue",
              "slug": "issue-1"
            }
          ]
        }
      }
    }
  };
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
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/nonexistent",
		URLPrefix:  "https://example.com/",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want route not found error")
	}

	if !containsAny(err.Error(), []string{"route", "not found"}) {
		t.Errorf("error message = %q, want to contain 'route' and 'not found'", err.Error())
	}
}
