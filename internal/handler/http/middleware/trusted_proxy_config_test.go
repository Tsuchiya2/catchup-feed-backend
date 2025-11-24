package middleware

import (
	"net/netip"
	"os"
	"testing"
)

// TestLoadTrustedProxyConfig_Disabled tests the default configuration
// when RATE_LIMIT_TRUST_PROXY is not set or set to false
func TestLoadTrustedProxyConfig_Disabled(t *testing.T) {
	// Ensure environment is clean
	t.Setenv("RATE_LIMIT_TRUST_PROXY", "false")

	config, err := LoadTrustedProxyConfig()

	if err != nil {
		t.Fatalf("LoadTrustedProxyConfig() returned unexpected error: %v", err)
	}

	if config.Enabled {
		t.Error("Expected Enabled=false when RATE_LIMIT_TRUST_PROXY is false")
	}

	if len(config.AllowedCIDRs) != 0 {
		t.Errorf("Expected empty AllowedCIDRs when disabled, got %d entries", len(config.AllowedCIDRs))
	}
}

// TestLoadTrustedProxyConfig_EnabledWithSingleIP tests configuration
// with a single IP that should be auto-converted to /32 prefix
func TestLoadTrustedProxyConfig_EnabledWithSingleIP(t *testing.T) {
	t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
	t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "192.168.1.100")

	config, err := LoadTrustedProxyConfig()

	if err != nil {
		t.Fatalf("LoadTrustedProxyConfig() returned unexpected error: %v", err)
	}

	if !config.Enabled {
		t.Error("Expected Enabled=true when RATE_LIMIT_TRUST_PROXY is true")
	}

	if len(config.AllowedCIDRs) != 1 {
		t.Fatalf("Expected 1 AllowedCIDR, got %d", len(config.AllowedCIDRs))
	}

	// Verify the IP was converted to /32 prefix
	expected := netip.MustParsePrefix("192.168.1.100/32")
	if config.AllowedCIDRs[0] != expected {
		t.Errorf("Expected CIDR %v, got %v", expected, config.AllowedCIDRs[0])
	}
}

// TestLoadTrustedProxyConfig_EnabledWithCIDR tests configuration
// with proper CIDR notation
func TestLoadTrustedProxyConfig_EnabledWithCIDR(t *testing.T) {
	t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
	t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "10.0.0.0/8")

	config, err := LoadTrustedProxyConfig()

	if err != nil {
		t.Fatalf("LoadTrustedProxyConfig() returned unexpected error: %v", err)
	}

	if !config.Enabled {
		t.Error("Expected Enabled=true")
	}

	if len(config.AllowedCIDRs) != 1 {
		t.Fatalf("Expected 1 AllowedCIDR, got %d", len(config.AllowedCIDRs))
	}

	expected := netip.MustParsePrefix("10.0.0.0/8")
	if config.AllowedCIDRs[0] != expected {
		t.Errorf("Expected CIDR %v, got %v", expected, config.AllowedCIDRs[0])
	}
}

// TestLoadTrustedProxyConfig_EnabledWithMultipleCIDRs tests configuration
// with comma-separated list of IPs and CIDRs
func TestLoadTrustedProxyConfig_EnabledWithMultipleCIDRs(t *testing.T) {
	t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
	t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "10.0.0.0/8, 172.16.0.0/12, 192.168.1.1")

	config, err := LoadTrustedProxyConfig()

	if err != nil {
		t.Fatalf("LoadTrustedProxyConfig() returned unexpected error: %v", err)
	}

	if len(config.AllowedCIDRs) != 3 {
		t.Fatalf("Expected 3 AllowedCIDRs, got %d", len(config.AllowedCIDRs))
	}

	expectedCIDRs := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.1.1/32"),
	}

	for i, expected := range expectedCIDRs {
		if config.AllowedCIDRs[i] != expected {
			t.Errorf("Expected CIDR[%d] = %v, got %v", i, expected, config.AllowedCIDRs[i])
		}
	}
}

// TestLoadTrustedProxyConfig_ErrorOnInvalidCIDR tests that invalid CIDR
// format returns an error
func TestLoadTrustedProxyConfig_ErrorOnInvalidCIDR(t *testing.T) {
	testCases := []struct {
		name         string
		proxiesValue string
	}{
		{"invalid IP", "999.999.999.999"},
		{"invalid CIDR", "192.168.1.0/99"},
		{"malformed", "not-an-ip"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
			t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", tc.proxiesValue)

			_, err := LoadTrustedProxyConfig()

			if err == nil {
				t.Error("Expected error for invalid CIDR format, got nil")
			}
		})
	}
}

// TestLoadTrustedProxyConfig_SkipsEmptyElements tests that empty elements
// in the comma-separated list are skipped
func TestLoadTrustedProxyConfig_SkipsEmptyElements(t *testing.T) {
	t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
	t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "10.0.0.0/8,  , 192.168.1.1")

	config, err := LoadTrustedProxyConfig()

	if err != nil {
		t.Fatalf("LoadTrustedProxyConfig() returned unexpected error: %v", err)
	}

	// Should have 2 CIDRs (empty element is skipped)
	if len(config.AllowedCIDRs) != 2 {
		t.Errorf("Expected 2 AllowedCIDRs (empty element skipped), got %d", len(config.AllowedCIDRs))
	}
}

// TestLoadTrustedProxyConfig_ErrorWhenEnabledButEmpty tests that
// enabling proxy trust without providing proxy list returns an error
func TestLoadTrustedProxyConfig_ErrorWhenEnabledButEmpty(t *testing.T) {
	t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
	t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "")

	_, err := LoadTrustedProxyConfig()

	if err == nil {
		t.Fatal("Expected error when RATE_LIMIT_TRUST_PROXY=true but RATE_LIMIT_TRUSTED_PROXIES is empty")
	}

	expectedErrMsg := "RATE_LIMIT_TRUST_PROXY is enabled but RATE_LIMIT_TRUSTED_PROXIES is empty"
	if err.Error() != expectedErrMsg {
		t.Errorf("Expected error message %q, got %q", expectedErrMsg, err.Error())
	}
}

// TestLoadTrustedProxyConfig_ErrorWhenEnabledWithWhitespace tests that
// whitespace-only proxy list returns an error
func TestLoadTrustedProxyConfig_ErrorWhenEnabledWithWhitespace(t *testing.T) {
	t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
	t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "   ")

	_, err := LoadTrustedProxyConfig()

	if err == nil {
		t.Fatal("Expected error when RATE_LIMIT_TRUSTED_PROXIES contains only whitespace")
	}
}

// TestLoadTrustedProxyConfig_IPv6Support tests IPv6 CIDR parsing
func TestLoadTrustedProxyConfig_IPv6Support(t *testing.T) {
	testCases := []struct {
		name     string
		proxies  string
		expected netip.Prefix
	}{
		{"IPv6 CIDR", "2001:db8::/32", netip.MustParsePrefix("2001:db8::/32")},
		{"IPv6 single IP", "2001:db8::1", netip.MustParsePrefix("2001:db8::1/128")},
		{"IPv6 loopback", "::1", netip.MustParsePrefix("::1/128")},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
			t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", tc.proxies)

			config, err := LoadTrustedProxyConfig()

			if err != nil {
				t.Fatalf("LoadTrustedProxyConfig() returned unexpected error: %v", err)
			}

			if len(config.AllowedCIDRs) != 1 {
				t.Fatalf("Expected 1 AllowedCIDR, got %d", len(config.AllowedCIDRs))
			}

			if config.AllowedCIDRs[0] != tc.expected {
				t.Errorf("Expected CIDR %v, got %v", tc.expected, config.AllowedCIDRs[0])
			}
		})
	}
}

// TestTrustedProxyConfig_IsTrusted_MatchesCIDR tests that IPs within
// the configured CIDR ranges are correctly identified as trusted
func TestTrustedProxyConfig_IsTrusted_MatchesCIDR(t *testing.T) {
	config := &TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
			netip.MustParsePrefix("192.168.1.0/24"),
		},
	}

	testCases := []struct {
		name       string
		remoteAddr string
		expected   bool
	}{
		{"IP in first CIDR", "10.0.0.1:54321", true},
		{"IP in first CIDR (high range)", "10.255.255.255:8080", true},
		{"IP in second CIDR", "192.168.1.100:12345", true},
		{"IP not in any CIDR", "172.16.0.1:9000", false},
		{"Public IP", "8.8.8.8:443", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := config.IsTrusted(tc.remoteAddr)

			if result != tc.expected {
				t.Errorf("IsTrusted(%q) = %v, expected %v", tc.remoteAddr, result, tc.expected)
			}
		})
	}
}

// TestTrustedProxyConfig_IsTrusted_NotInList tests that IPs outside
// the configured CIDR ranges are correctly identified as untrusted
func TestTrustedProxyConfig_IsTrusted_NotInList(t *testing.T) {
	config := &TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("192.168.1.0/24"),
		},
	}

	untrustedIPs := []string{
		"192.168.2.1:8080",   // Different subnet
		"192.168.0.255:9000", // Just outside range
		"10.0.0.1:443",       // Different private range
		"1.2.3.4:12345",      // Public IP
	}

	for _, ip := range untrustedIPs {
		t.Run(ip, func(t *testing.T) {
			if config.IsTrusted(ip) {
				t.Errorf("IsTrusted(%q) should return false for IP outside CIDR range", ip)
			}
		})
	}
}

// TestTrustedProxyConfig_IsTrusted_HandlesPortFormat tests that
// IsTrusted correctly strips port numbers from IP:port format
func TestTrustedProxyConfig_IsTrusted_HandlesPortFormat(t *testing.T) {
	config := &TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("192.168.1.100/32"),
		},
	}

	testCases := []struct {
		name       string
		remoteAddr string
		expected   bool
	}{
		{"with port", "192.168.1.100:8080", true},
		{"with different port", "192.168.1.100:443", true},
		{"with high port", "192.168.1.100:54321", true},
		{"different IP with port", "192.168.1.101:8080", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := config.IsTrusted(tc.remoteAddr)

			if result != tc.expected {
				t.Errorf("IsTrusted(%q) = %v, expected %v", tc.remoteAddr, result, tc.expected)
			}
		})
	}
}

// TestTrustedProxyConfig_IsTrusted_InvalidIP tests that invalid IPs
// return false instead of panicking
func TestTrustedProxyConfig_IsTrusted_InvalidIP(t *testing.T) {
	config := &TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("192.168.1.0/24"),
		},
	}

	invalidIPs := []string{
		"not-an-ip",
		"999.999.999.999:8080",
		"",
		"invalid:invalid",
	}

	for _, ip := range invalidIPs {
		t.Run(ip, func(t *testing.T) {
			// Should not panic, should return false
			result := config.IsTrusted(ip)

			if result {
				t.Errorf("IsTrusted(%q) should return false for invalid IP", ip)
			}
		})
	}
}

// TestLoadTrustedProxyConfig_NoEnvVars tests default behavior when
// no environment variables are set
func TestLoadTrustedProxyConfig_NoEnvVars(t *testing.T) {
	// Unset all relevant env vars
	_ = os.Unsetenv("RATE_LIMIT_TRUST_PROXY")
	_ = os.Unsetenv("RATE_LIMIT_TRUSTED_PROXIES")

	config, err := LoadTrustedProxyConfig()

	if err != nil {
		t.Fatalf("LoadTrustedProxyConfig() returned unexpected error: %v", err)
	}

	if config.Enabled {
		t.Error("Expected Enabled=false when no env vars are set")
	}

	if len(config.AllowedCIDRs) != 0 {
		t.Errorf("Expected empty AllowedCIDRs, got %d entries", len(config.AllowedCIDRs))
	}
}

// TestTrustedProxyConfig_IsTrusted_IPv6 tests IPv6 address matching
func TestTrustedProxyConfig_IsTrusted_IPv6(t *testing.T) {
	config := &TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("2001:db8::/32"),
			netip.MustParsePrefix("::1/128"),
		},
	}

	testCases := []struct {
		name       string
		remoteAddr string
		expected   bool
	}{
		{"IPv6 in range", "[2001:db8::1]:8080", true},
		{"IPv6 in range (high)", "[2001:db8:ffff:ffff::1]:9000", true},
		{"IPv6 loopback", "[::1]:54321", true},
		{"IPv6 not in range", "[2001:db9::1]:8080", false},
		{"IPv6 public", "[2606:4700:4700::1111]:443", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := config.IsTrusted(tc.remoteAddr)

			if result != tc.expected {
				t.Errorf("IsTrusted(%q) = %v, expected %v", tc.remoteAddr, result, tc.expected)
			}
		})
	}
}
