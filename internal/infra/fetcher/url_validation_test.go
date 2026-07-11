package fetcher_test

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"catchup-feed/internal/infra/fetcher"
	"catchup-feed/internal/usecase/fetch"
)

// TestSSRFCheckRedirect covers the shared per-hop redirect guard used by both
// the RSS feed-fetch client and the article-body client (H-1). The public and
// article paths must stay symmetric, so this exercises the single source of
// truth directly: redirect-count cap + private-IP rejection on each hop.
func TestSSRFCheckRedirect(t *testing.T) {
	mustReq := func(rawURL string) *http.Request {
		u, err := url.Parse(rawURL)
		if err != nil {
			t.Fatalf("failed to parse %q: %v", rawURL, err)
		}
		return &http.Request{URL: u}
	}
	// via builds a redirect chain of length n (targets are irrelevant, only len).
	via := func(n int) []*http.Request {
		chain := make([]*http.Request, n)
		for i := range chain {
			chain[i] = mustReq("https://example.com")
		}
		return chain
	}

	tests := []struct {
		name           string
		maxRedirects   int
		denyPrivateIPs bool
		target         string
		via            []*http.Request
		wantErr        error  // errors.Is target, nil = expect success
		wantMsgContain string // substring check when wantErr is not a sentinel
	}{
		{
			name:           "redirect to cloud metadata is blocked",
			maxRedirects:   5,
			denyPrivateIPs: true,
			target:         "http://169.254.169.254/latest/meta-data/",
			via:            via(1),
			wantErr:        fetch.ErrPrivateIP,
		},
		{
			name:           "redirect to loopback is blocked",
			maxRedirects:   5,
			denyPrivateIPs: true,
			target:         "http://127.0.0.1:6379/",
			via:            via(1),
			wantErr:        fetch.ErrPrivateIP,
		},
		{
			name:           "redirect to private 192.168 is blocked",
			maxRedirects:   5,
			denyPrivateIPs: true,
			target:         "http://192.168.1.10/admin",
			via:            via(1),
			wantErr:        fetch.ErrPrivateIP,
		},
		{
			name:           "redirect to IPv6 loopback is blocked",
			maxRedirects:   5,
			denyPrivateIPs: true,
			target:         "http://[::1]/",
			via:            via(1),
			wantErr:        fetch.ErrPrivateIP,
		},
		{
			name:           "non-http scheme redirect is blocked",
			maxRedirects:   5,
			denyPrivateIPs: true,
			target:         "file:///etc/passwd",
			via:            via(1),
			wantErr:        fetch.ErrInvalidURL,
		},
		{
			name:           "too many redirects is blocked",
			maxRedirects:   3,
			denyPrivateIPs: true,
			target:         "https://example.com/next",
			via:            via(3),
			wantErr:        fetch.ErrTooManyRedirects,
		},
		{
			name:           "public target within limit is allowed",
			maxRedirects:   5,
			denyPrivateIPs: true,
			target:         "https://example.com/article",
			via:            via(1),
			wantErr:        nil,
		},
		{
			name:           "private target allowed when denyPrivateIPs is false",
			maxRedirects:   5,
			denyPrivateIPs: false,
			target:         "http://127.0.0.1:6379/",
			via:            via(1),
			wantErr:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check := fetcher.SSRFCheckRedirect(tt.maxRedirects, tt.denyPrivateIPs)
			err := check(mustReq(tt.target), tt.via)

			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %v, got nil", tt.wantErr)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected error to wrap %v, got %v", tt.wantErr, err)
			}
			if tt.wantMsgContain != "" && !strings.Contains(err.Error(), tt.wantMsgContain) {
				t.Errorf("expected error message to contain %q, got %q", tt.wantMsgContain, err.Error())
			}
		})
	}
}
