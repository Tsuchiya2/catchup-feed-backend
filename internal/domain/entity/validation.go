package entity

import (
	"fmt"
	"net"
	"net/url"
)

// maxURLLength defines the maximum allowed length for URLs to prevent DoS attacks.
const maxURLLength = 2048

// ValidateURL validates the format and safety of a URL.
// It checks that the URL is well-formed, uses HTTP/HTTPS scheme, and has a valid host.
// It also blocks private IP addresses to prevent SSRF attacks.
// Returns a ValidationError if the URL is invalid or empty.
func ValidateURL(rawURL string) error {
	if rawURL == "" {
		return &ValidationError{Field: "url", Message: "URL is required"}
	}

	// DoS protection: enforce maximum URL length
	if len(rawURL) > maxURLLength {
		return &ValidationError{
			Field:   "url",
			Message: fmt.Sprintf("url must not exceed %d characters", maxURLLength),
		}
	}

	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}

	// HTTPまたはHTTPSスキームのみ許可
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return &ValidationError{Field: "url", Message: "URL must use http or https scheme"}
	}

	// ホスト名の検証
	if parsedURL.Host == "" {
		return &ValidationError{Field: "url", Message: "URL must have a valid host"}
	}

	// SSRF対策: プライベートIPアドレスをブロック
	host := parsedURL.Hostname()
	ips, err := net.LookupIP(host)
	if err == nil && len(ips) > 0 {
		for _, ip := range ips {
			if isPrivateIP(ip) {
				return &ValidationError{
					Field:   "url",
					Message: "url cannot point to private network",
				}
			}
		}
	}

	return nil
}

// isPrivateIP checks if an IP address is in a private or restricted range.
// This prevents SSRF attacks by blocking access to:
// - localhost (127.0.0.0/8, ::1)
// - link-local addresses (169.254.0.0/16, fe80::/10)
// - private networks (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
// - cloud metadata endpoints (169.254.169.254)
func isPrivateIP(ip net.IP) bool {
	// localhost
	if ip.IsLoopback() {
		return true
	}

	// link-local
	if ip.IsLinkLocalUnicast() {
		return true
	}

	// Private IPv4 ranges
	privateIPv4Ranges := []string{
		"10.0.0.0/8",     // Private network
		"172.16.0.0/12",  // Private network
		"192.168.0.0/16", // Private network
		"169.254.0.0/16", // Link-local (includes cloud metadata)
	}

	for _, cidr := range privateIPv4Ranges {
		_, subnet, _ := net.ParseCIDR(cidr)
		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}
