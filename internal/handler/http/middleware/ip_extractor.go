package middleware

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
)

// IPExtractor is an interface for extracting client IP addresses from HTTP requests.
// It provides an abstraction layer for different IP extraction strategies,
// allowing the application to choose between secure RemoteAddr extraction
// (default) or header-based extraction with proxy trust validation (opt-in).
type IPExtractor interface {
	// ExtractIP extracts the client IP address from an HTTP request.
	// Returns the IP address as a string and an error if extraction fails.
	ExtractIP(r *http.Request) (string, error)
}

// RemoteAddrExtractor extracts the client IP from the RemoteAddr field of the HTTP request.
// This is the default and most secure approach as it uses the actual TCP connection IP,
// which cannot be spoofed by the client. It should be used when the application is
// directly accessible (no reverse proxy) or when header trust is explicitly disabled.
type RemoteAddrExtractor struct{}

// ExtractIP extracts the IP address from r.RemoteAddr.
// The RemoteAddr format is "IP:port", so this method strips the port number
// to return only the IP address. Handles both IPv4 and IPv6 addresses correctly.
//
// Examples:
//   - "192.168.1.1:54321" → "192.168.1.1"
//   - "[2001:db8::1]:8080" → "2001:db8::1"
//   - "127.0.0.1" → "127.0.0.1" (no port)
func (e *RemoteAddrExtractor) ExtractIP(r *http.Request) (string, error) {
	return extractIPFromAddr(r.RemoteAddr)
}

// TrustedProxyConfig holds configuration for validating trusted reverse proxies.
// When enabled, the extractor will check if the request comes from a trusted proxy
// before extracting the client IP from X-Forwarded-For or X-Real-IP headers.
type TrustedProxyConfig struct {
	// Enabled indicates whether proxy trust is enabled.
	// When false, all header-based extraction is disabled.
	Enabled bool

	// AllowedCIDRs is a list of trusted proxy IP ranges in CIDR notation.
	// Both single IPs (converted to /32 or /128) and CIDR ranges are supported.
	// Examples: ["10.0.0.1/32", "172.16.0.0/12", "2001:db8::/32"]
	AllowedCIDRs []netip.Prefix
}

// IsTrusted checks if the given RemoteAddr belongs to a trusted proxy.
// It strips the port number from RemoteAddr and compares the IP address
// against the list of trusted proxy CIDR ranges.
//
// Returns:
//   - true if the IP is in the trusted proxy list
//   - false if the IP is not trusted or if an error occurs during parsing
func (c *TrustedProxyConfig) IsTrusted(remoteAddr string) bool {
	// Extract IP from "IP:port" format
	ip, err := extractIPFromAddr(remoteAddr)
	if err != nil {
		return false
	}

	// Parse the IP address
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}

	// Check if IP is in any of the trusted CIDR ranges
	for _, prefix := range c.AllowedCIDRs {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}

// LoadTrustedProxyConfig loads trusted proxy configuration from environment variables.
//
// Environment Variables:
//   - RATE_LIMIT_TRUST_PROXY: Set to "true" to enable proxy trust checking (default: false)
//   - RATE_LIMIT_TRUSTED_PROXIES: Comma-separated list of trusted proxy IPs or CIDR ranges
//
// Examples:
//   - Single IP: "192.168.1.1" (auto-converted to /32 prefix)
//   - CIDR range: "10.0.0.0/8,172.16.0.0/12"
//   - IPv6: "2001:db8::/32"
//
// Error Handling:
//   - Returns error if RATE_LIMIT_TRUST_PROXY=true but RATE_LIMIT_TRUSTED_PROXIES is empty
//   - Returns error if any IP/CIDR is invalid
//   - Fail-closed: invalid configuration prevents application startup
//
// Returns:
//   - *TrustedProxyConfig: Loaded configuration
//   - error: Non-nil if configuration is invalid
func LoadTrustedProxyConfig() (*TrustedProxyConfig, error) {
	// Check if proxy trust is enabled
	enabled := os.Getenv("RATE_LIMIT_TRUST_PROXY") == "true"

	config := &TrustedProxyConfig{
		Enabled:      enabled,
		AllowedCIDRs: []netip.Prefix{},
	}

	// If not enabled, return empty config
	if !enabled {
		return config, nil
	}

	// If enabled, RATE_LIMIT_TRUSTED_PROXIES must be provided
	proxiesStr := strings.TrimSpace(os.Getenv("RATE_LIMIT_TRUSTED_PROXIES"))
	if proxiesStr == "" {
		return nil, fmt.Errorf("RATE_LIMIT_TRUST_PROXY is enabled but RATE_LIMIT_TRUSTED_PROXIES is empty")
	}

	// Parse comma-separated list of IPs/CIDRs
	proxyList := strings.Split(proxiesStr, ",")
	for _, proxyStr := range proxyList {
		proxyStr = strings.TrimSpace(proxyStr)
		if proxyStr == "" {
			continue
		}

		// Try to parse as CIDR first
		prefix, err := netip.ParsePrefix(proxyStr)
		if err != nil {
			// If CIDR parsing fails, try parsing as a single IP
			ip, ipErr := netip.ParseAddr(proxyStr)
			if ipErr != nil {
				return nil, fmt.Errorf("invalid IP or CIDR format '%s': must be valid IP address or CIDR notation (e.g., '192.168.1.1' or '10.0.0.0/8')", proxyStr)
			}

			// Convert single IP to /32 (IPv4) or /128 (IPv6) prefix
			if ip.Is4() {
				prefix = netip.PrefixFrom(ip, 32)
			} else {
				prefix = netip.PrefixFrom(ip, 128)
			}
		}

		config.AllowedCIDRs = append(config.AllowedCIDRs, prefix)
	}

	// Ensure at least one proxy is configured when enabled
	if len(config.AllowedCIDRs) == 0 {
		return nil, fmt.Errorf("RATE_LIMIT_TRUST_PROXY is enabled but no valid proxies found in RATE_LIMIT_TRUSTED_PROXIES")
	}

	return config, nil
}

// TrustedProxyExtractor extracts the client IP from X-Forwarded-For or X-Real-IP headers
// when the request comes from a trusted proxy. If the proxy is not trusted, it falls back
// to RemoteAddr extraction to prevent IP spoofing attacks.
//
// Header extraction priority:
// 1. X-Forwarded-For (first IP in comma-separated list)
// 2. X-Real-IP (fallback)
// 3. RemoteAddr (if proxy is not trusted or headers are missing)
type TrustedProxyExtractor struct {
	config TrustedProxyConfig
}

// NewTrustedProxyExtractor creates a new TrustedProxyExtractor with the given configuration.
func NewTrustedProxyExtractor(config TrustedProxyConfig) *TrustedProxyExtractor {
	return &TrustedProxyExtractor{
		config: config,
	}
}

// ExtractIP extracts the client IP address using the following logic:
//
// 1. If proxy trust is disabled (config.Enabled = false):
//
//   - Always use RemoteAddr (most secure)
//
//     2. If proxy trust is enabled:
//     a. Check if RemoteAddr is a trusted proxy
//     b. If trusted:
//
//   - Check X-Forwarded-For header (use first IP)
//
//   - If empty, check X-Real-IP header
//
//   - If both empty, fallback to RemoteAddr
//     c. If NOT trusted:
//
//   - Log warning about potential spoofing attempt
//
//   - Use RemoteAddr (ignore headers)
//
// This approach prevents rate-limit bypass attacks where malicious clients
// send spoofed X-Forwarded-For headers to rotate their apparent IP address.
func (e *TrustedProxyExtractor) ExtractIP(r *http.Request) (string, error) {
	// If proxy trust is disabled, always use RemoteAddr
	if !e.config.Enabled {
		return extractIPFromAddr(r.RemoteAddr)
	}

	// Check if the request comes from a trusted proxy
	if !e.config.IsTrusted(r.RemoteAddr) {
		// Log warning if headers are present but proxy is not trusted
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			slog.Warn("untrusted proxy attempting to set X-Forwarded-For",
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("x_forwarded_for", xff),
			)
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			slog.Warn("untrusted proxy attempting to set X-Real-IP",
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("x_real_ip", xri),
			)
		}

		// Use RemoteAddr for untrusted sources
		return extractIPFromAddr(r.RemoteAddr)
	}

	// Trusted proxy: Try X-Forwarded-For first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := parseFirstIP(xff); ip != "" {
			return ip, nil
		}
	}

	// Fallback to X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String(), nil
		}
	}

	// Final fallback to RemoteAddr
	return extractIPFromAddr(r.RemoteAddr)
}

// extractIPFromAddr extracts the IP address from a "host:port" or "IP" string.
// Handles both IPv4 and IPv6 addresses correctly.
//
// Examples:
//   - "192.168.1.1:8080" → "192.168.1.1", nil
//   - "[2001:db8::1]:8080" → "2001:db8::1", nil
//   - "127.0.0.1" → "127.0.0.1", nil (no port)
//   - "[::1]" → "::1", nil (IPv6 without port)
func extractIPFromAddr(addr string) (string, error) {
	// Try to split host:port
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// If SplitHostPort fails, the address might not have a port
		// Try to parse it directly as an IP
		if ip := net.ParseIP(addr); ip != nil {
			return ip.String(), nil
		}
		return "", fmt.Errorf("invalid address format: %s", addr)
	}
	return host, nil
}

// parseFirstIP parses the first IP address from a comma-separated list.
// This is used for X-Forwarded-For headers, which may contain multiple IPs
// in the format: "client, proxy1, proxy2"
//
// Returns:
//   - The first valid IP address in the list
//   - Empty string if no valid IP is found
//
// Examples:
//   - "192.168.1.1, 10.0.0.1" → "192.168.1.1"
//   - "2001:db8::1, 10.0.0.1" → "2001:db8::1"
//   - "invalid, 10.0.0.1" → ""
//   - "192.168.1.1" → "192.168.1.1" (no comma)
func parseFirstIP(s string) string {
	// Find the first comma
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			// Parse the IP before the comma
			ip := net.ParseIP(s[:i])
			if ip != nil {
				return ip.String()
			}
			return ""
		}
	}

	// No comma found, parse the entire string
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return ""
}
