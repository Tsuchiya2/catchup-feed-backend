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

// TestTrustedProxyExtractor_UsesXRealIP_AsFallback tests that
// TrustedProxyExtractor uses X-Real-IP when X-Forwarded-For is not present
func TestTrustedProxyExtractor_UsesXRealIP_AsFallback(t *testing.T) {
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

	if ip != "203.0.113.2" {
		t.Errorf("ExtractIP() = %q, expected %q (from X-Real-IP)", ip, "203.0.113.2")
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
// TrustedProxyExtractor extracts the first IP from comma-separated X-Forwarded-For
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
		{"two IPs", "203.0.113.1, 10.0.0.5", "203.0.113.1"},
		{"three IPs", "203.0.113.1, 192.168.1.1, 10.0.0.5", "203.0.113.1"},
		{"single IP", "203.0.113.1", "203.0.113.1"},
		{"with spaces", "  203.0.113.1  , 10.0.0.5", ""},
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

			// If expected is empty, should fallback to RemoteAddr
			if tc.expected == "" {
				if ip != "10.0.0.5" {
					t.Errorf("ExtractIP() = %q, expected fallback to RemoteAddr", ip)
				}
			} else if ip != tc.expected {
				t.Errorf("ExtractIP() = %q, expected %q (first IP from XFF)", ip, tc.expected)
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

// TestTrustedProxyExtractor_XFFPriority tests X-Forwarded-For
// takes priority over X-Real-IP
func TestTrustedProxyExtractor_XFFPriority(t *testing.T) {
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
	req.Header.Set("X-Real-IP", "203.0.113.2")

	ip, err := extractor.ExtractIP(req)

	if err != nil {
		t.Fatalf("ExtractIP() returned unexpected error: %v", err)
	}

	if ip != "203.0.113.1" {
		t.Errorf("ExtractIP() = %q, expected %q (XFF has priority over X-Real-IP)", ip, "203.0.113.1")
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

// TestParseFirstIP_EdgeCases tests edge cases for parseFirstIP helper
func TestParseFirstIP_EdgeCases(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{"single IP", "192.168.1.1", "192.168.1.1"},
		{"multiple IPs", "192.168.1.1, 10.0.0.1", "192.168.1.1"},
		{"invalid first IP", "invalid, 10.0.0.1", ""},
		{"empty string", "", ""},
		{"IPv6", "2001:db8::1", "2001:db8::1"},
		{"IPv6 multiple", "2001:db8::1, 10.0.0.1", "2001:db8::1"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseFirstIP(tc.input)

			if result != tc.expected {
				t.Errorf("parseFirstIP(%q) = %q, expected %q", tc.input, result, tc.expected)
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

// TestTrustedProxyExtractor_InvalidXRealIP tests fallback when X-Real-IP is invalid
func TestTrustedProxyExtractor_InvalidXRealIP(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	}
	extractor := NewTrustedProxyExtractor(config)

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.5:54321" // Trusted proxy
	// No X-Forwarded-For
	req.Header.Set("X-Real-IP", "invalid-ip")

	ip, err := extractor.ExtractIP(req)

	if err != nil {
		t.Fatalf("ExtractIP() returned unexpected error: %v", err)
	}

	// Should fallback to RemoteAddr
	if ip != "10.0.0.5" {
		t.Errorf("ExtractIP() = %q, expected fallback to RemoteAddr", ip)
	}
}
