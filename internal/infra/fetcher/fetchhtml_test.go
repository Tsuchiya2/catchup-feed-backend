package fetcher_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"catchup-feed/internal/infra/fetcher"
	"catchup-feed/internal/usecase/fetch"
)

// ───────────────────────────────────────────────────────────
// FetchHTML (newsletter link-expansion fallback) — the raw-HTML
// path must share the exact SSRF posture of FetchContent. The
// entry validateURL now lives inside fetchRawHTML (structural
// sharing), and these tests pin the behavior from the outside so
// a regression on either path is caught.
// ───────────────────────────────────────────────────────────

// TestFetchHTML_ReturnsRawHTML: the raw page bytes come back verbatim —
// no readability extraction (the newsletter link extractor needs the
// <a href> structure readability would destroy).
func TestFetchHTML_ReturnsRawHTML(t *testing.T) {
	const page = `<!DOCTYPE html>
<html><head><title>Issue 42</title></head>
<body>
	<nav>This navigation would be stripped by readability.</nav>
	<a href="https://alpha.dev/post1">Alpha Post</a>
	<a href="https://beta.dev/post2">Beta Post</a>
</body></html>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != fetcher.UserAgent {
			t.Errorf("expected User-Agent=%q, got %q", fetcher.UserAgent, r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write([]byte(page)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	config := fetcher.DefaultConfig()
	config.DenyPrivateIPs = false // local test server is loopback
	f := fetcher.NewReadabilityFetcher(config)

	got, err := f.FetchHTML(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("FetchHTML() error = %v", err)
	}
	if got != page {
		t.Errorf("FetchHTML must return the raw HTML unmodified.\ngot:  %q\nwant: %q", got, page)
	}
}

// TestFetchHTML_PrivateIPRejected: entry-point SSRF validation fires on the
// FetchHTML path exactly as on FetchContent (shared fetchRawHTML).
func TestFetchHTML_PrivateIPRejected(t *testing.T) {
	config := fetcher.DefaultConfig()
	config.DenyPrivateIPs = true
	f := fetcher.NewReadabilityFetcher(config)

	tests := []struct {
		name string
		url  string
	}{
		{"localhost", "http://localhost/issue"},
		{"loopback IPv4", "http://127.0.0.1:8080/issue"},
		{"private 10.x", "http://10.0.0.1/issue"},
		{"private 192.168.x", "http://192.168.1.1/issue"},
		{"cloud metadata (link-local)", "http://169.254.169.254/latest/meta-data/"},
		{"loopback IPv6", "http://[::1]/issue"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.FetchHTML(context.Background(), tt.url)
			if err == nil {
				t.Fatal("expected SSRF rejection, got nil")
			}
			if !errors.Is(err, fetch.ErrPrivateIP) {
				t.Errorf("expected ErrPrivateIP, got: %v", err)
			}
		})
	}
}

// TestFetchHTML_InvalidSchemeRejected: only http/https reach the network.
func TestFetchHTML_InvalidSchemeRejected(t *testing.T) {
	config := fetcher.DefaultConfig()
	f := fetcher.NewReadabilityFetcher(config)

	for _, url := range []string{"file:///etc/passwd", "ftp://example.com/x", "javascript:alert(1)"} {
		_, err := f.FetchHTML(context.Background(), url)
		if err == nil {
			t.Fatalf("expected scheme rejection for %q, got nil", url)
		}
		if !errors.Is(err, fetch.ErrInvalidURL) {
			t.Errorf("expected ErrInvalidURL for %q, got: %v", url, err)
		}
	}
}

// TestFetchHTML_RedirectGuardEngaged proves the shared SSRFCheckRedirect
// hook is installed and firing on the FetchHTML path: the redirect-count
// cap rejects the chain mid-flight. The hook's per-hop private-IP rejection
// itself is covered exhaustively by TestSSRFCheckRedirect — FetchHTML uses
// the very same http.Client instance as FetchContent, and the
// "public entry redirecting to a private IP" scenario cannot be built
// hermetically here (httptest binds loopback, which the entry validation
// rejects first — same constraint documented in
// TestFetchContent_RedirectToPrivateIP).
func TestFetchHTML_RedirectGuardEngaged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String(), http.StatusFound) // endless self-redirect
	}))
	defer server.Close()

	config := fetcher.DefaultConfig()
	config.DenyPrivateIPs = false // local test server is loopback
	config.MaxRedirects = 2
	f := fetcher.NewReadabilityFetcher(config)

	_, err := f.FetchHTML(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected redirect-cap rejection, got nil")
	}
	if !strings.Contains(err.Error(), "redirect") {
		t.Errorf("expected redirect error from the shared SSRF hook, got: %v", err)
	}
}

// TestFetchHTML_BodyTooLarge: the MaxBodySize guard applies to the raw-HTML
// path (memory exhaustion protection shared with FetchContent).
func TestFetchHTML_BodyTooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(strings.Repeat("x", 4096))); err != nil {
			t.Logf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	config := fetcher.DefaultConfig()
	config.DenyPrivateIPs = false // local test server is loopback
	config.MaxBodySize = 2048
	f := fetcher.NewReadabilityFetcher(config)

	_, err := f.FetchHTML(context.Background(), server.URL)
	if err == nil {
		t.Fatal("expected body-too-large rejection, got nil")
	}
	if !errors.Is(err, fetch.ErrBodyTooLarge) {
		t.Errorf("expected ErrBodyTooLarge, got: %v", err)
	}
}
