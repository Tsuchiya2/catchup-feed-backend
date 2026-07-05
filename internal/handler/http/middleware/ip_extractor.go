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

	return c.containsAddr(addr)
}

// containsAddr checks if the given address is in any of the trusted CIDR ranges.
// IPv4-mapped IPv6 addresses (::ffff:a.b.c.d) are unmapped so they match IPv4 prefixes.
func (c *TrustedProxyConfig) containsAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
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
//  1. X-Forwarded-For (rightmost-untrusted: scan right-to-left, strip trusted proxies,
//     take the first non-trusted IP — see ExtractIP)
//  2. X-Real-IP (fallback)
//  3. RemoteAddr (if proxy is not trusted or headers are missing)
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
//   - Scan X-Forwarded-For right-to-left, strip trusted proxy entries, and
//     use the first (rightmost) non-trusted IP as the client IP
//     (rightmost-untrusted). NEVER use the leftmost entry: proxies such as
//     cloudflared APPEND the real peer IP to whatever X-Forwarded-For the
//     client sent, so the leftmost entry is attacker-controlled.
//
//   - If X-Forwarded-For yields nothing, check X-Real-IP header
//
//   - If both yield nothing, fallback to RemoteAddr
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

	// Trusted proxy: Try X-Forwarded-For first (rightmost-untrusted)
	if xffValues := r.Header.Values("X-Forwarded-For"); len(xffValues) > 0 {
		if ip, ok := e.clientIPFromXFF(splitXFFEntries(xffValues)); ok {
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

// splitXFFEntries flattens one or more X-Forwarded-For header values into a
// single ordered list of entries. Multiple header lines are equivalent to a
// single comma-joined header (RFC 7230 §3.2.2), so values are concatenated
// in order before splitting on commas.
func splitXFFEntries(values []string) []string {
	var entries []string
	for _, v := range values {
		entries = append(entries, strings.Split(v, ",")...)
	}
	return entries
}

// clientIPFromXFF determines the client IP from X-Forwarded-For entries using
// the rightmost-untrusted algorithm: scan the entries from right to left,
// strip every entry that belongs to a trusted proxy CIDR, and return the
// first non-trusted IP encountered.
//
// Why rightmost, not leftmost: a reverse proxy such as cloudflared APPENDS
// the real peer IP to whatever X-Forwarded-For the client already sent.
// With "X-Forwarded-For: <attacker-chosen>, <real-ip>", the leftmost entry
// is fully attacker-controlled; taking it would let clients rotate their
// apparent IP and bypass per-IP rate limits. Only the entries appended by
// trusted proxies (the rightmost suffix) can be believed, and the first IP
// to their left is the closest peer a trusted proxy actually observed.
//
// Returns:
//   - (clientIP, true) for the rightmost non-trusted entry
//   - (lastStripped, true) if ALL entries are trusted (the leftmost stripped
//     entry is the best available answer)
//   - ("", false) if no usable entry was found before the scan stopped; the
//     caller falls back to X-Real-IP / RemoteAddr. The scan stops at a
//     malformed entry: anything further left cannot be attributed safely, so
//     garbage-sending clients all collapse into the proxy's bucket
//     (fail-safe for rate limiting, never a bypass).
func (e *TrustedProxyExtractor) clientIPFromXFF(entries []string) (string, bool) {
	var lastTrusted netip.Addr

	for i := len(entries) - 1; i >= 0; i-- {
		s := strings.TrimSpace(entries[i])
		if s == "" {
			continue
		}

		addr, err := netip.ParseAddr(s)
		if err != nil {
			// Malformed entry inside the trusted suffix: stop scanning.
			break
		}

		if !e.config.containsAddr(addr) {
			// Rightmost non-trusted IP = client IP.
			return addr.String(), true
		}

		// Trusted proxy entry: strip it and keep scanning left.
		lastTrusted = addr
	}

	// All scanned entries were trusted proxies (or the scan stopped after
	// stripping at least one): fall back to the last stripped entry.
	if lastTrusted.IsValid() {
		return lastTrusted.String(), true
	}

	return "", false
}
