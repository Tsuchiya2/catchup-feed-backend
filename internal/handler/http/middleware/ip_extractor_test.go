package middleware

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

// TestRemoteAddrExtractor_ExtractsIP tests RemoteAddrExtractor
// correctly extracts IP from "IP:port" format
func TestRemoteAddrExtractor_ExtractsIP(t *testing.T) {
	extractor := &RemoteAddrExtractor{}

	testCases := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{"IPv4 with port", "192.168.1.1:54321", "192.168.1.1"},
		{"IPv4 localhost", "127.0.0.1:8080", "127.0.0.1"},
		{"IPv4 public IP", "8.8.8.8:443", "8.8.8.8"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remoteAddr

			ip, err := extractor.ExtractIP(req)

			if err != nil {
				t.Fatalf("ExtractIP() returned unexpected error: %v", err)
			}

			if ip != tc.expected {
				t.Errorf("ExtractIP() = %q, expected %q", ip, tc.expected)
			}
		})
	}
}

// TestRemoteAddrExtractor_HandlesIPv6 tests RemoteAddrExtractor
// correctly handles IPv6 addresses
func TestRemoteAddrExtractor_HandlesIPv6(t *testing.T) {
	extractor := &RemoteAddrExtractor{}

	testCases := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{"IPv6 with port", "[::1]:8080", "::1"},
		{"IPv6 full address", "[2001:db8::1]:443", "2001:db8::1"},
		// Note: net.ParseIP returns the canonical form, which may not be compressed
		{"IPv6 expanded", "[2001:db8:0:0:0:0:0:1]:9000", "2001:db8:0:0:0:0:0:1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remoteAddr

			ip, err := extractor.ExtractIP(req)

			if err != nil {
				t.Fatalf("ExtractIP() returned unexpected error: %v", err)
			}

			if ip != tc.expected {
				t.Errorf("ExtractIP() = %q, expected %q", ip, tc.expected)
			}
		})
	}
}

// TestRemoteAddrExtractor_HandlesNoPort tests RemoteAddrExtractor
// handles IP addresses without port numbers
func TestRemoteAddrExtractor_HandlesNoPort(t *testing.T) {
	extractor := &RemoteAddrExtractor{}

	testCases := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{"IPv4 no port", "192.168.1.1", "192.168.1.1"},
		{"IPv4 localhost", "127.0.0.1", "127.0.0.1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tc.remoteAddr

			ip, err := extractor.ExtractIP(req)

			if err != nil {
				t.Fatalf("ExtractIP() returned unexpected error: %v", err)
			}

			if ip != tc.expected {
				t.Errorf("ExtractIP() = %q, expected %q", ip, tc.expected)
			}
		})
	}
}

// TestTrustedProxyExtractor_UsesXFF_WhenTrusted tests that
// TrustedProxyExtractor uses X-Forwarded-For when request comes from trusted proxy
func TestTrustedProxyExtractor_UsesXFF_WhenTrusted(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	}
	extractor := NewTrustedProxyExtractor(config)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:54321" // Trusted proxy
	req.Header.Set("X-Forwarded-For", "203.0.113.1")

	ip, err := extractor.ExtractIP(req)

	if err != nil {
		t.Fatalf("ExtractIP() returned unexpected error: %v", err)
	}

	if ip != "203.0.113.1" {
		t.Errorf("ExtractIP() = %q, expected %q (from X-Forwarded-For)", ip, "203.0.113.1")
	}
}

// TestTrustedProxyExtractor_IgnoresXFF_WhenUntrusted tests that
// TrustedProxyExtractor ignores X-Forwarded-For when request comes from untrusted source
func TestTrustedProxyExtractor_IgnoresXFF_WhenUntrusted(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	}
	extractor := NewTrustedProxyExtractor(config)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.50:12345" // Untrusted source (not in 10.0.0.0/8)
	req.Header.Set("X-Forwarded-For", "192.168.1.100")

	ip, err := extractor.ExtractIP(req)

	if err != nil {
		t.Fatalf("ExtractIP() returned unexpected error: %v", err)
	}

	if ip != "203.0.113.50" {
		t.Errorf("ExtractIP() = %q, expected %q (from RemoteAddr, not XFF)", ip, "203.0.113.50")
	}
}

// TestTrustedProxyExtractor_IgnoresXRealIP pins the removal of the
// X-Real-IP fallback: nothing in this stack sets that header, so even from
// a trusted proxy it must be ignored — otherwise trusting a proxy that does
// not append X-Forwarded-For would hand clients a spoofable IP header.
func TestTrustedProxyExtractor_IgnoresXRealIP(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	}
	extractor := NewTrustedProxyExtractor(config)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:54321" // Trusted proxy
	// No X-Forwarded-For header
	req.Header.Set("X-Real-IP", "203.0.113.2")

	ip, err := extractor.ExtractIP(req)

	if err != nil {
		t.Fatalf("ExtractIP() returned unexpected error: %v", err)
	}

	if ip != "10.0.0.5" {
		t.Errorf("ExtractIP() = %q, expected RemoteAddr %q (X-Real-IP must be ignored)", ip, "10.0.0.5")
	}
}

// TestTrustedProxyExtractor_FallsBackToRemoteAddr tests that
// TrustedProxyExtractor uses RemoteAddr when no headers are present
func TestTrustedProxyExtractor_FallsBackToRemoteAddr(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	}
	extractor := NewTrustedProxyExtractor(config)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:54321" // Trusted proxy
	// No X-Forwarded-For or X-Real-IP headers

	ip, err := extractor.ExtractIP(req)

	if err != nil {
		t.Fatalf("ExtractIP() returned unexpected error: %v", err)
	}

	if ip != "10.0.0.5" {
		t.Errorf("ExtractIP() = %q, expected %q (from RemoteAddr)", ip, "10.0.0.5")
	}
}

// TestTrustedProxyExtractor_ParsesMultipleXFFIPs tests that
// TrustedProxyExtractor picks the rightmost non-trusted IP from
// comma-separated X-Forwarded-For (rightmost-untrusted), never the leftmost
// (which is client-controlled when proxies append).
func TestTrustedProxyExtractor_ParsesMultipleXFFIPs(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	}
	extractor := NewTrustedProxyExtractor(config)

	testCases := []struct {
		name     string
		xffValue string
		expected string
	}{
		// Rightmost entry is a trusted proxy → stripped; next is the client.
		{"two IPs", "203.0.113.1, 10.0.0.5", "203.0.113.1"},
		// 192.168.1.1 is NOT in the trusted list → it is the rightmost
		// untrusted IP and wins over the (spoofable) leftmost 203.0.113.1.
		{"three IPs", "203.0.113.1, 192.168.1.1, 10.0.0.5", "192.168.1.1"},
		{"single IP", "203.0.113.1", "203.0.113.1"},
		// Entries are trimmed before parsing.
		{"with spaces", "  203.0.113.1  , 10.0.0.5", "203.0.113.1"},
		// All entries trusted → fall back to the last stripped entry.
		{"all trusted", "10.0.0.7, 10.0.0.5", "10.0.0.7"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = "10.0.0.5:54321" // Trusted proxy
			req.Header.Set("X-Forwarded-For", tc.xffValue)

			ip, err := extractor.ExtractIP(req)

			if err != nil {
				t.Fatalf("ExtractIP() returned unexpected error: %v", err)
			}

			if ip != tc.expected {
				t.Errorf("ExtractIP() = %q, expected %q (rightmost-untrusted from XFF)", ip, tc.expected)
			}
		})
	}
}

// TestTrustedProxyExtractor_SpoofedXFF_NotBypassable is the regression test
// for the rate-limit bypass: an attacker sends a forged X-Forwarded-For and
// the trusted proxy (cloudflared on 127.0.0.1) APPENDS the real client IP.
// The extractor must adopt the appended real IP, never the forged leftmost
// entry — otherwise per-IP limits (e.g. /auth/token 5 req/min) can be
// rotated away for free.
func TestTrustedProxyExtractor_SpoofedXFF_NotBypassable(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("127.0.0.1/32"), // cloudflared on localhost
		},
	}
	extractor := NewTrustedProxyExtractor(config)

	const (
		spoofedIP = "6.6.6.6"      // attacker-chosen, sent in the forged XFF
		realIP    = "203.0.113.77" // appended by cloudflared (actual peer)
	)

	testCases := []struct {
		name string
		xff  []string // one or more X-Forwarded-For header values
	}{
		{
			name: "attacker prepends fake IP",
			xff:  []string{spoofedIP + ", " + realIP},
		},
		{
			name: "attacker prepends multiple fake IPs",
			xff:  []string{spoofedIP + ", 7.7.7.7, " + realIP},
		},
		{
			name: "attacker prepends trusted proxy IP",
			xff:  []string{spoofedIP + ", 127.0.0.1, " + realIP},
		},
		{
			name: "attacker sends separate XFF header line",
			xff:  []string{spoofedIP, realIP},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/auth/token", nil)
			req.RemoteAddr = "127.0.0.1:51234" // connection comes from cloudflared
			for _, v := range tc.xff {
				req.Header.Add("X-Forwarded-For", v)
			}

			ip, err := extractor.ExtractIP(req)

			if err != nil {
				t.Fatalf("ExtractIP() returned unexpected error: %v", err)
			}

			if ip == spoofedIP {
				t.Fatalf("ExtractIP() adopted the attacker-controlled XFF entry %q — rate-limit bypass regression", spoofedIP)
			}
			if ip != realIP {
				t.Errorf("ExtractIP() = %q, expected the proxy-appended real IP %q", ip, realIP)
			}
		})
	}
}

// TestTrustedProxyExtractor_HandlesInvalidXFF tests that
// malformed X-Forwarded-For falls back to RemoteAddr
func TestTrustedProxyExtractor_HandlesInvalidXFF(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	}
	extractor := NewTrustedProxyExtractor(config)

	testCases := []struct {
		name     string
		xffValue string
	}{
		{"invalid IP", "not-an-ip"},
		{"invalid format", "999.999.999.999"},
		{"empty", ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = "10.0.0.5:54321" // Trusted proxy
			req.Header.Set("X-Forwarded-For", tc.xffValue)

			ip, err := extractor.ExtractIP(req)

			if err != nil {
				t.Fatalf("ExtractIP() returned unexpected error: %v", err)
			}

			// Should fallback to RemoteAddr
			if ip != "10.0.0.5" {
				t.Errorf("ExtractIP() = %q, expected fallback to RemoteAddr %q", ip, "10.0.0.5")
			}
		})
	}
}

// TestTrustedProxyExtractor_DisabledConfig tests that when
// Enabled=false, always uses RemoteAddr
func TestTrustedProxyExtractor_DisabledConfig(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled:      false, // Disabled
		AllowedCIDRs: []netip.Prefix{},
	}
	extractor := NewTrustedProxyExtractor(config)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.50:12345"
	req.Header.Set("X-Forwarded-For", "192.168.1.100")
	req.Header.Set("X-Real-IP", "192.168.1.101")

	ip, err := extractor.ExtractIP(req)

	if err != nil {
		t.Fatalf("ExtractIP() returned unexpected error: %v", err)
	}

	if ip != "203.0.113.50" {
		t.Errorf("ExtractIP() = %q, expected %q (RemoteAddr, headers ignored)", ip, "203.0.113.50")
	}
}

// TestExtractIPFromAddr_EdgeCases tests edge cases for extractIPFromAddr helper
func TestExtractIPFromAddr_EdgeCases(t *testing.T) {
	testCases := []struct {
		name      string
		addr      string
		expected  string
		expectErr bool
	}{
		{"IPv4:port", "192.168.1.1:8080", "192.168.1.1", false},
		{"IPv6:port", "[::1]:8080", "::1", false},
		{"IPv4 no port", "192.168.1.1", "192.168.1.1", false},
		{"invalid format", "not-an-address", "", true},
		{"empty string", "", "", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ip, err := extractIPFromAddr(tc.addr)

			if tc.expectErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if ip != tc.expected {
					t.Errorf("extractIPFromAddr(%q) = %q, expected %q", tc.addr, ip, tc.expected)
				}
			}
		})
	}
}

// TestClientIPFromXFF_EdgeCases tests edge cases for the rightmost-untrusted
// clientIPFromXFF helper (trusted CIDR: 10.0.0.0/8).
func TestClientIPFromXFF_EdgeCases(t *testing.T) {
	extractor := NewTrustedProxyExtractor(TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	})

	testCases := []struct {
		name     string
		entries  []string
		expected string
		ok       bool
	}{
		{"single untrusted IP", []string{"192.168.1.1"}, "192.168.1.1", true},
		{"untrusted then trusted", []string{"192.168.1.1", " 10.0.0.1"}, "192.168.1.1", true},
		{"rightmost untrusted wins over leftmost", []string{"192.168.1.1", "203.0.113.9", "10.0.0.1"}, "203.0.113.9", true},
		{"all trusted returns last stripped", []string{"10.0.0.9", "10.0.0.1"}, "10.0.0.9", true},
		// Malformed entry stops the scan; entries to its left are ignored.
		{"malformed left of trusted", []string{"invalid", "10.0.0.1"}, "10.0.0.1", true},
		{"malformed rightmost", []string{"192.168.1.1", "invalid"}, "", false},
		{"empty list", nil, "", false},
		{"empty entries only", []string{"", "  "}, "", false},
		{"IPv6 untrusted", []string{"2001:db8::1", "10.0.0.1"}, "2001:db8::1", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, ok := extractor.clientIPFromXFF(tc.entries)

			if ok != tc.ok {
				t.Fatalf("clientIPFromXFF(%v) ok = %v, expected %v", tc.entries, ok, tc.ok)
			}
			if result != tc.expected {
				t.Errorf("clientIPFromXFF(%v) = %q, expected %q", tc.entries, result, tc.expected)
			}
		})
	}
}

// TestTrustedProxyExtractor_IPv6Headers tests IPv6 addresses in headers
func TestTrustedProxyExtractor_IPv6Headers(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("2001:db8::/32"),
		},
	}
	extractor := NewTrustedProxyExtractor(config)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "[2001:db8::1]:54321" // Trusted IPv6 proxy
	req.Header.Set("X-Forwarded-For", "2606:4700:4700::1111")

	ip, err := extractor.ExtractIP(req)

	if err != nil {
		t.Fatalf("ExtractIP() returned unexpected error: %v", err)
	}

	if ip != "2606:4700:4700::1111" {
		t.Errorf("ExtractIP() = %q, expected %q (from XFF)", ip, "2606:4700:4700::1111")
	}
}
