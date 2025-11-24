// Package fetcher provides content fetching implementations for RSS content enhancement.
package fetcher

import (
	"fmt"
	"net"
	"net/url"

	"catchup-feed/internal/usecase/fetch"
)

// validateURL validates a URL for security before making an HTTP request.
// This function prevents Server-Side Request Forgery (SSRF) attacks by:
//   - Checking URL scheme (only http/https allowed)
//   - Resolving DNS to check for private IP addresses
//   - Blocking access to loopback, private, and link-local addresses
//
// Parameters:
//   - urlStr: The URL string to validate
//   - denyPrivateIPs: If true, block access to private IP addresses (SSRF prevention)
//
// Returns:
//   - error: nil if URL is valid and safe, error otherwise
//
// Blocked IP ranges (when denyPrivateIPs is true):
//   - 127.0.0.0/8 (loopback)
//   - 10.0.0.0/8 (private)
//   - 172.16.0.0/12 (private)
//   - 192.168.0.0/16 (private)
//   - 169.254.0.0/16 (link-local)
//   - ::1 (IPv6 loopback)
//   - fc00::/7 (IPv6 private)
//   - fe80::/10 (IPv6 link-local)
//
// Example:
//
//	err := validateURL("https://example.com/article", true)
//	if err != nil {
//	    // URL is invalid or points to private IP
//	}
func validateURL(urlStr string, denyPrivateIPs bool) error {
	// Parse URL
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("%w: parse error: %v", fetch.ErrInvalidURL, err)
	}

	// Validate scheme (only http and https allowed)
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("%w: scheme '%s' not allowed (only http/https)", fetch.ErrInvalidURL, u.Scheme)
	}

	// If hostname is empty, URL is invalid
	hostname := u.Hostname()
	if hostname == "" {
		return fmt.Errorf("%w: empty hostname", fetch.ErrInvalidURL)
	}

	// Skip private IP check if disabled
	if !denyPrivateIPs {
		return nil
	}

	// DNS resolution to check for private IPs
	// This prevents SSRF attacks where attacker provides URLs pointing to internal network
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return fmt.Errorf("%w: DNS lookup failed for %s: %v", fetch.ErrInvalidURL, hostname, err)
	}

	// Check each resolved IP address
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("%w: hostname '%s' resolves to private IP %s", fetch.ErrPrivateIP, hostname, ip.String())
		}
	}

	return nil
}

// isPrivateIP checks if an IP address is in a private or loopback range.
// This function supports both IPv4 and IPv6 addresses.
//
// Blocked IP ranges:
//   - Loopback: 127.0.0.0/8 (IPv4), ::1 (IPv6)
//   - Private: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16 (IPv4), fc00::/7 (IPv6)
//   - Link-local: 169.254.0.0/16 (IPv4), fe80::/10 (IPv6)
//
// Parameters:
//   - ip: The IP address to check
//
// Returns:
//   - bool: true if IP is private/loopback/link-local, false otherwise
//
// Example:
//
//	if isPrivateIP(net.ParseIP("192.168.1.1")) {
//	    // This is a private IP
//	}
//
// Reference:
//   - https://tools.ietf.org/html/rfc1918 (Private IPv4)
//   - https://tools.ietf.org/html/rfc4193 (Private IPv6)
//   - https://tools.ietf.org/html/rfc3927 (Link-local IPv4)
//   - https://tools.ietf.org/html/rfc4291 (Link-local IPv6)
func isPrivateIP(ip net.IP) bool {
	// Check loopback addresses
	// IPv4: 127.0.0.0/8 (127.0.0.1, 127.0.0.2, etc.)
	// IPv6: ::1
	if ip.IsLoopback() {
		return true
	}

	// Check private addresses
	// IPv4: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
	// IPv6: fc00::/7
	if ip.IsPrivate() {
		return true
	}

	// Check link-local addresses
	// IPv4: 169.254.0.0/16
	// IPv6: fe80::/10
	if ip.IsLinkLocalUnicast() {
		return true
	}

	return false
}
