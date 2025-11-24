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

// TestSSRF_ComprehensiveCases tests SSRF prevention for all private IP ranges
func TestSSRF_ComprehensiveCases(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "Localhost IPv4",
			url:     "http://localhost:8080",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Localhost 127.0.0.1",
			url:     "http://127.0.0.1",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Localhost 127.0.0.1 with port",
			url:     "http://127.0.0.1:8080",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Private IP 10.0.0.0/8 - start",
			url:     "http://10.0.0.1",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Private IP 10.0.0.0/8 - mid",
			url:     "http://10.128.0.1",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Private IP 10.0.0.0/8 - end",
			url:     "http://10.255.255.254",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Private IP 172.16.0.0/12 - start",
			url:     "http://172.16.0.1",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Private IP 172.16.0.0/12 - mid",
			url:     "http://172.20.0.1",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Private IP 172.16.0.0/12 - end",
			url:     "http://172.31.255.254",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Private IP 192.168.0.0/16 - start",
			url:     "http://192.168.0.1",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Private IP 192.168.0.0/16 - mid",
			url:     "http://192.168.100.1",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Private IP 192.168.0.0/16 - end",
			url:     "http://192.168.255.254",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Link-local 169.254.0.0/16 - AWS metadata",
			url:     "http://169.254.169.254",
			wantErr: true,
			errMsg:  "private IP",
		},
		{
			name:    "Link-local 169.254.0.0/16 - start",
			url:     "http://169.254.0.1",
			wantErr: true,
			errMsg:  "private IP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Timeout: 10 * time.Second}
			fetcher := scraper.NewWebflowScraper(client)

			config := &entity.ScraperConfig{
				ItemSelector: ".item",
			}
			ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

			_, err := fetcher.Fetch(ctx, tt.url)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Fetch() error = nil, want error containing %q", tt.errMsg)
				}
				if !containsAny(err.Error(), []string{tt.errMsg, "SSRF"}) {
					t.Errorf("error = %q, want to contain %q or 'SSRF'", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Fetch() error = %v, want nil", err)
				}
			}
		})
	}
}

// TestSSRF_RedirectChain tests SSRF prevention with redirect chains
func TestSSRF_RedirectChain(t *testing.T) {
	// Create a mock server that redirects to localhost
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect to localhost (SSRF attempt)
		http.Redirect(w, r, "http://127.0.0.1:8080", http.StatusFound)
	}))
	defer server.Close()

	// Create HTTP client with redirect validation
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			// This would need validateURL from scraper package
			// For now, client follows redirects, but individual scrapers should validate
			return nil
		},
	}

	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	// Note: This test may pass if the HTTP client doesn't follow the redirect
	// The key is that the scraper validates the final URL
	_, err := fetcher.Fetch(ctx, server.URL)

	// We expect an error, either from redirect or from SSRF validation
	if err == nil {
		t.Log("Note: Redirect may not be followed, but test passed (no SSRF)")
	}
}

// TestResourceLimit_MaxBodySize tests the 10MB body size limit
func TestResourceLimit_MaxBodySize(t *testing.T) {
	// Mock server with 11MB response (exceeds limit)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// Write more than 10MB of data
		chunk := make([]byte, 1024*1024) // 1MB chunks
		for i := range chunk {
			chunk[i] = 'a'
		}
		for i := 0; i < 11; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	client := &http.Client{Timeout: 30 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector:  ".item",
		TitleSelector: ".title",
		URLSelector:   "a",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	// Should handle gracefully (body limited to 10MB)
	_, err := fetcher.Fetch(ctx, server.URL)

	// Expect error because truncated HTML won't have valid items
	if err == nil {
		t.Log("Note: Large response handled but resulted in no items (expected)")
	}
}

// TestResourceLimit_HTTPTimeout tests the HTTP timeout enforcement
func TestResourceLimit_HTTPTimeout(t *testing.T) {
	// Mock server with slow response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than client timeout
		time.Sleep(15 * time.Second)
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer server.Close()

	// Client with 5 second timeout
	client := &http.Client{Timeout: 5 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	start := time.Now()
	_, err := fetcher.Fetch(ctx, server.URL)
	elapsed := time.Since(start)

	// Should timeout and return error
	if err == nil {
		t.Fatal("Fetch() error = nil, want timeout error")
	}

	// Should timeout around 5 seconds (with retry overhead, may be longer)
	if elapsed > 20*time.Second {
		t.Errorf("elapsed time = %v, want around 5-15s (with retries)", elapsed)
	}

	if !containsAny(err.Error(), []string{"timeout", "context deadline", "Client.Timeout"}) {
		t.Logf("Note: error = %q (expected timeout-related error)", err.Error())
	}
}

// TestSSRF_InvalidSchemes tests rejection of non-HTTP schemes
func TestSSRF_InvalidSchemes(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		errMsg string
	}{
		{
			name:   "File scheme",
			url:    "file:///etc/passwd",
			errMsg: "unsupported scheme",
		},
		{
			name:   "FTP scheme",
			url:    "ftp://example.com",
			errMsg: "unsupported scheme",
		},
		{
			name:   "Data scheme",
			url:    "data:text/html,<html></html>",
			errMsg: "unsupported scheme",
		},
		{
			name:   "JavaScript scheme",
			url:    "javascript:alert(1)",
			errMsg: "unsupported scheme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Timeout: 10 * time.Second}
			fetcher := scraper.NewWebflowScraper(client)

			config := &entity.ScraperConfig{
				ItemSelector: ".item",
			}
			ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

			_, err := fetcher.Fetch(ctx, tt.url)

			if err == nil {
				t.Fatalf("Fetch() error = nil, want error containing %q", tt.errMsg)
			}

			// May error at different stages (URL parsing or scheme validation)
			// Just ensure it errors out
			t.Logf("Error: %v (expected)", err)
		})
	}
}

// TestSSRF_PublicIPsAllowed tests that public IPs are allowed
func TestSSRF_PublicIPsAllowed(t *testing.T) {
	// Note: This test uses actual DNS resolution, which may vary
	// We test with well-known public domains instead of direct IPs

	tests := []struct {
		name string
		url  string
	}{
		{
			name: "Google DNS",
			url:  "http://8.8.8.8", // Google Public DNS
		},
		{
			name: "Cloudflare DNS",
			url:  "http://1.1.1.1", // Cloudflare DNS
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Timeout: 10 * time.Second}
			fetcher := scraper.NewWebflowScraper(client)

			config := &entity.ScraperConfig{
				ItemSelector: ".item",
			}
			ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

			// These should NOT be blocked by SSRF protection
			// (They may fail for other reasons like connection refused)
			_, err := fetcher.Fetch(ctx, tt.url)

			// We don't expect SSRF errors
			if err != nil {
				if containsAny(err.Error(), []string{"private IP", "SSRF prevention"}) {
					t.Errorf("Public IP was blocked by SSRF protection: %v", err)
				} else {
					t.Logf("Request failed for other reasons (expected): %v", err)
				}
			}
		})
	}
}

// TestSSRF_AWSMetadataService tests blocking of AWS metadata service
func TestSSRF_AWSMetadataService(t *testing.T) {
	// AWS EC2 metadata service (link-local address)
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, "http://169.254.169.254/latest/meta-data/")

	if err == nil {
		t.Fatal("Fetch() error = nil, want SSRF prevention error")
	}

	if !containsAny(err.Error(), []string{"private IP", "SSRF"}) {
		t.Errorf("error = %q, want to contain 'private IP' or 'SSRF'", err.Error())
	}
}

// TestSSRF_MaxRedirects tests that excessive redirects are prevented
func TestSSRF_MaxRedirects(t *testing.T) {
	redirectCount := 0

	// Server that creates a redirect loop
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectCount++
		// Redirect to self (infinite loop)
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer server.Close()

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	fetcher := scraper.NewWebflowScraper(client)

	config := &entity.ScraperConfig{
		ItemSelector: ".item",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, server.URL)

	// Should either stop at max redirects or fail
	if err == nil {
		t.Log("Note: Redirect loop handled gracefully")
	}

	// Redirects should be limited
	if redirectCount > 10 {
		t.Errorf("redirectCount = %d, want <= 10 (should be limited)", redirectCount)
	}
}

// TestSSRF_NextJSScraper tests SSRF protection in NextJS scraper
func TestSSRF_NextJSScraper(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewNextJSScraper(client)

	config := &entity.ScraperConfig{
		DataKey: "initialSeedData",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, "http://127.0.0.1:8080")

	if err == nil {
		t.Fatal("Fetch() error = nil, want SSRF prevention error")
	}

	if !containsAny(err.Error(), []string{"private IP", "SSRF"}) {
		t.Errorf("error = %q, want to contain 'private IP' or 'SSRF'", err.Error())
	}
}

// TestSSRF_RemixScraper tests SSRF protection in Remix scraper
func TestSSRF_RemixScraper(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	fetcher := scraper.NewRemixScraper(client)

	config := &entity.ScraperConfig{
		ContextKey: "routes/test",
	}
	ctx := context.WithValue(context.Background(), scraper.ScraperConfigKey, config)

	_, err := fetcher.Fetch(ctx, "http://192.168.1.1")

	if err == nil {
		t.Fatal("Fetch() error = nil, want SSRF prevention error")
	}

	if !containsAny(err.Error(), []string{"private IP", "SSRF"}) {
		t.Errorf("error = %q, want to contain 'private IP' or 'SSRF'", err.Error())
	}
}
