package scraper_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"catchup-feed/internal/infra/fetcher"
	"catchup-feed/internal/infra/scraper"
)

// ssrfGuardedClient builds an HTTP client wired exactly like the worker's
// feed-fetch client (cmd/worker createHTTPClient): per-hop SSRF validation via
// the shared fetcher.SSRFCheckRedirect hook. This is the client under test for
// H-1: the RSS feed path must reject redirects to private IPs the same way the
// article-body path does.
func ssrfGuardedClient(maxRedirects int, denyPrivateIPs bool) *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: fetcher.SSRFCheckRedirect(maxRedirects, denyPrivateIPs),
	}
}

// TestRSSFetcher_RedirectToPrivateIP_Blocked verifies that a public feed URL
// that 30x-redirects to a private/link-local address is rejected before the
// redirect is followed. The initial request lands on a loopback httptest
// server (which CheckRedirect never inspects — only redirect *targets* are
// validated), so the redirect hop to a private IP literal is what must be
// blocked. This is the exact SSRF / DNS-rebind pivot from the H-1 audit.
func TestRSSFetcher_RedirectToPrivateIP_Blocked(t *testing.T) {
	tests := []struct {
		name     string
		location string
	}{
		{name: "cloud metadata", location: "http://169.254.169.254/latest/meta-data/"},
		{name: "loopback redis", location: "http://127.0.0.1:6379/"},
		{name: "private 10.x", location: "http://10.0.0.1/internal-feed"},
		{name: "private 192.168.x", location: "http://192.168.1.1/feed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, tt.location, http.StatusFound)
			}))
			defer server.Close()

			client := ssrfGuardedClient(5, true)
			f := scraper.NewRSSFetcher(client)

			_, err := f.Fetch(context.Background(), server.URL)
			if err == nil {
				t.Fatalf("expected fetch to be blocked for redirect to %s, got nil", tt.location)
			}
			// gofeed wraps the transport error; the redirect-validation message
			// must surface (private IP or SSRF-related).
			if !strings.Contains(err.Error(), "private IP") &&
				!strings.Contains(err.Error(), "SSRF") &&
				!strings.Contains(err.Error(), "redirect target validation failed") {
				t.Errorf("expected SSRF/private-IP redirect error, got: %v", err)
			}
		})
	}
}

// TestRSSFetcher_RedirectToPublic_Allowed is the regression guard: a normal
// public-to-public redirect chain (both loopback httptest servers here, since
// DenyPrivateIPs is disabled for the local test) still fetches and parses the
// feed. This proves the SSRF hook does not break legitimate redirects.
func TestRSSFetcher_RedirectToPublic_Allowed(t *testing.T) {
	finalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Redirected Feed</title>
    <link>https://example.com</link>
    <description>After redirect</description>
    <item>
      <title>Redirected Article</title>
      <link>https://example.com/redirected</link>
      <description>Body</description>
      <pubDate>Mon, 01 Jan 2024 00:00:00 +0000</pubDate>
    </item>
  </channel>
</rss>`
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, err := w.Write([]byte(rss)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer finalServer.Close()

	initialServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, finalServer.URL, http.StatusFound)
	}))
	defer initialServer.Close()

	// DenyPrivateIPs=false so the loopback redirect target is permitted, the
	// same way DenyPrivateIPs=false is used across the existing local-server
	// tests. This isolates the "redirect still works" behavior.
	client := ssrfGuardedClient(5, false)
	f := scraper.NewRSSFetcher(client)

	items, err := f.Fetch(context.Background(), initialServer.URL)
	if err != nil {
		t.Fatalf("Fetch() through public redirect error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}
	if items[0].Title != "Redirected Article" {
		t.Errorf("items[0].Title = %q, want %q", items[0].Title, "Redirected Article")
	}
}

// TestRSSFetcher_NoRedirect_StillWorks confirms the plain (no-redirect) fetch
// path is unchanged once the CheckRedirect hook is installed.
func TestRSSFetcher_NoRedirect_StillWorks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Direct Feed</title>
    <link>https://example.com</link>
    <description>No redirect</description>
    <item>
      <title>Direct Article</title>
      <link>https://example.com/direct</link>
      <description>Body</description>
    </item>
  </channel>
</rss>`
		w.Header().Set("Content-Type", "application/rss+xml")
		if _, err := w.Write([]byte(rss)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := ssrfGuardedClient(5, true)
	f := scraper.NewRSSFetcher(client)

	items, err := f.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items length = %d, want 1", len(items))
	}
}
