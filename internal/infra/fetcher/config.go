package fetcher

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// ContentFetchConfig holds the configuration for content fetching operations.
// This configuration controls security, performance, and behavior of the
// content enhancement feature.
//
// Security settings:
//   - DenyPrivateIPs: Prevents SSRF attacks by blocking private IP addresses
//   - MaxBodySize: Prevents memory exhaustion from oversized responses
//   - MaxRedirects: Prevents infinite redirect loops
//   - Timeout: Prevents resource starvation from slow servers
//
// Performance settings:
//   - Parallelism: Controls concurrent fetch operations
//   - Threshold: Avoids unnecessary fetching for sufficient RSS content
//
// Feature toggle:
//   - Enabled: Allows the feature to be disabled without code changes
type ContentFetchConfig struct {
	// Enabled controls whether content fetching is enabled.
	// When false, all content fetching is skipped and RSS content is used directly.
	// This allows for instant feature toggle without redeployment.
	// Default: true
	Enabled bool

	// Threshold is the minimum RSS content length (in characters) before fetching.
	// If RSS content length >= Threshold, fetching is skipped (content is sufficient).
	// If RSS content length < Threshold, full content is fetched from source URL.
	// Default: 1500
	Threshold int

	// Timeout is the maximum duration for a single HTTP request.
	// This prevents resource starvation from slow or unresponsive servers.
	// Should be less than the overall crawl timeout.
	// Default: 10s
	Timeout time.Duration

	// Parallelism is the maximum number of concurrent content fetching operations.
	// This controls how many articles can be fetched simultaneously.
	// Higher values increase throughput but consume more resources.
	// Should be higher than AI summarization parallelism (which is 5).
	// Default: 10
	Parallelism int

	// MaxBodySize is the maximum HTTP response body size in bytes.
	// Responses exceeding this limit are rejected to prevent memory exhaustion.
	// This is enforced during response reading, not based on Content-Length header.
	// Default: 10485760 (10MB)
	MaxBodySize int64

	// MaxRedirects is the maximum number of HTTP redirects to follow.
	// This prevents infinite redirect loops and redirect-based attacks.
	// Each redirect target is validated for security (SSRF check).
	// Default: 5
	MaxRedirects int

	// DenyPrivateIPs controls whether to block access to private IP addresses.
	// When true, URLs resolving to private/loopback/link-local IPs are rejected.
	// This prevents Server-Side Request Forgery (SSRF) attacks.
	// Should always be true in production.
	// Default: true
	DenyPrivateIPs bool
}

// DefaultConfig returns the default configuration for content fetching.
// These defaults are optimized for:
//   - Security: SSRF prevention enabled, size/redirect limits enforced
//   - Performance: 10 concurrent fetches, 10s timeout per request
//   - Quality: 1500 character threshold balances fetch rate vs quality
//
// Returns:
//   - ContentFetchConfig with production-ready default values
//
// Example:
//
//	config := DefaultConfig()
//	config.Parallelism = 20  // Customize as needed
//	fetcher := NewReadabilityFetcher(config)
func DefaultConfig() ContentFetchConfig {
	return ContentFetchConfig{
		Enabled:        true,
		Threshold:      1500,
		Timeout:        10 * time.Second,
		Parallelism:    10,
		MaxBodySize:    10 * 1024 * 1024, // 10MB
		MaxRedirects:   5,
		DenyPrivateIPs: true,
	}
}

// Validate checks if the configuration values are valid and safe.
// This prevents misconfigurations that could lead to security issues
// or performance problems.
//
// Validation rules:
//   - Threshold: >= 0 (can be 0 to always fetch)
//   - Timeout: > 0 (must have timeout)
//   - Parallelism: 1-50 (prevent resource exhaustion)
//   - MaxBodySize: 1KB-100MB (prevent memory issues)
//   - MaxRedirects: 0-10 (reasonable redirect limit)
//
// Returns:
//   - error: nil if configuration is valid, descriptive error otherwise
//
// Example:
//
//	config := LoadConfigFromEnv()
//	if err := config.Validate(); err != nil {
//	    log.Fatal("Invalid configuration: %v", err)
//	}
func (c *ContentFetchConfig) Validate() error {
	if c.Threshold < 0 {
		return fmt.Errorf("threshold must be non-negative, got %d", c.Threshold)
	}

	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got %v", c.Timeout)
	}

	if c.Parallelism < 1 || c.Parallelism > 50 {
		return fmt.Errorf("parallelism must be between 1 and 50, got %d", c.Parallelism)
	}

	minBodySize := int64(1024)              // 1KB
	maxBodySize := int64(100 * 1024 * 1024) // 100MB
	if c.MaxBodySize < minBodySize || c.MaxBodySize > maxBodySize {
		return fmt.Errorf("max body size must be between %d and %d bytes, got %d", minBodySize, maxBodySize, c.MaxBodySize)
	}

	if c.MaxRedirects < 0 || c.MaxRedirects > 10 {
		return fmt.Errorf("max redirects must be between 0 and 10, got %d", c.MaxRedirects)
	}

	return nil
}

// LoadConfigFromEnv loads configuration from environment variables.
// If a variable is not set or invalid, the default value is used.
// After loading, the configuration is validated.
//
// Environment variables:
//   - CONTENT_FETCH_ENABLED: "true" or "false" (default: true)
//   - CONTENT_FETCH_THRESHOLD: integer (default: 1500)
//   - CONTENT_FETCH_TIMEOUT: duration string, e.g., "10s" (default: 10s)
//   - CONTENT_FETCH_PARALLELISM: integer (default: 10)
//   - CONTENT_FETCH_MAX_BODY_SIZE: integer in bytes (default: 10485760)
//   - CONTENT_FETCH_MAX_REDIRECTS: integer (default: 5)
//   - CONTENT_FETCH_DENY_PRIVATE_IPS: "true" or "false" (default: true)
//
// Returns:
//   - ContentFetchConfig: Loaded configuration
//   - error: Validation error if configuration is invalid
//
// Example:
//
//	// Set environment: CONTENT_FETCH_THRESHOLD=2000
//	config, err := LoadConfigFromEnv()
//	if err != nil {
//	    log.Fatal("Invalid configuration: %v", err)
//	}
//	// config.Threshold == 2000
func LoadConfigFromEnv() (ContentFetchConfig, error) {
	// Start with defaults
	cfg := DefaultConfig()

	// Load CONTENT_FETCH_ENABLED
	if val := os.Getenv("CONTENT_FETCH_ENABLED"); val != "" {
		cfg.Enabled = val == "true"
	}

	// Load CONTENT_FETCH_THRESHOLD
	if val := os.Getenv("CONTENT_FETCH_THRESHOLD"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			cfg.Threshold = parsed
		} else {
			return cfg, fmt.Errorf("invalid CONTENT_FETCH_THRESHOLD: %v", err)
		}
	}

	// Load CONTENT_FETCH_TIMEOUT
	if val := os.Getenv("CONTENT_FETCH_TIMEOUT"); val != "" {
		if parsed, err := time.ParseDuration(val); err == nil {
			cfg.Timeout = parsed
		} else {
			return cfg, fmt.Errorf("invalid CONTENT_FETCH_TIMEOUT: %v (expected format: '10s', '1m')", err)
		}
	}

	// Load CONTENT_FETCH_PARALLELISM
	if val := os.Getenv("CONTENT_FETCH_PARALLELISM"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			cfg.Parallelism = parsed
		} else {
			return cfg, fmt.Errorf("invalid CONTENT_FETCH_PARALLELISM: %v", err)
		}
	}

	// Load CONTENT_FETCH_MAX_BODY_SIZE
	if val := os.Getenv("CONTENT_FETCH_MAX_BODY_SIZE"); val != "" {
		if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
			cfg.MaxBodySize = parsed
		} else {
			return cfg, fmt.Errorf("invalid CONTENT_FETCH_MAX_BODY_SIZE: %v", err)
		}
	}

	// Load CONTENT_FETCH_MAX_REDIRECTS
	if val := os.Getenv("CONTENT_FETCH_MAX_REDIRECTS"); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			cfg.MaxRedirects = parsed
		} else {
			return cfg, fmt.Errorf("invalid CONTENT_FETCH_MAX_REDIRECTS: %v", err)
		}
	}

	// Load CONTENT_FETCH_DENY_PRIVATE_IPS
	if val := os.Getenv("CONTENT_FETCH_DENY_PRIVATE_IPS"); val != "" {
		cfg.DenyPrivateIPs = val == "true"
	}

	// Validate the loaded configuration
	if err := cfg.Validate(); err != nil {
		return cfg, fmt.Errorf("configuration validation failed: %w", err)
	}

	return cfg, nil
}
