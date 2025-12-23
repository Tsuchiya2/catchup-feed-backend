package ratelimit

import (
	"fmt"
	"time"
)

// RateLimitConfig contains the configuration for rate limiting.
//
// This struct holds all settings needed to configure rate limiters,
// including global defaults, per-endpoint overrides, and tier-based limits.
type RateLimitConfig struct {
	// Global default rate limit for IP-based limiting
	DefaultIPLimit int
	// Time window for IP-based rate limiting
	DefaultIPWindow time.Duration

	// Global default rate limit for user-based limiting
	DefaultUserLimit int
	// Time window for user-based rate limiting
	DefaultUserWindow time.Duration

	// Per-endpoint rate limit overrides
	EndpointOverrides []EndpointRateLimitConfig

	// User tier-based rate limits
	TierLimits []TierRateLimitConfig

	// Maximum number of active keys to keep in memory
	MaxActiveKeys int

	// How often to run cleanup of expired entries
	CleanupInterval time.Duration

	// Remove entries older than this duration
	CleanupMaxAge time.Duration

	// Circuit breaker settings
	CircuitBreakerFailureThreshold int           // Open circuit after N consecutive failures
	CircuitBreakerResetTimeout     time.Duration // Try half-open state after this timeout

	// Feature flag to enable/disable rate limiting
	Enabled bool
}

// EndpointRateLimitConfig defines rate limit overrides for specific endpoints.
//
// This allows different endpoints to have different rate limits.
// For example, authentication endpoints might have stricter limits.
type EndpointRateLimitConfig struct {
	// PathPattern is the endpoint path pattern (supports wildcards like "/articles/*")
	PathPattern string

	// IP-based rate limit for this endpoint
	IPLimit int
	// Time window for IP-based rate limiting
	IPWindow time.Duration

	// User-based rate limit for this endpoint
	UserLimit int
	// Time window for user-based rate limiting
	UserWindow time.Duration
}

// TierRateLimitConfig defines rate limits for user tiers.
//
// Different user tiers (admin, premium, basic, viewer) can have
// different rate limits to provide tiered service levels.
type TierRateLimitConfig struct {
	// Tier identifies the user tier (admin, premium, basic, viewer)
	Tier UserTier

	// Limit is the maximum requests allowed for this tier
	Limit int

	// Window is the time window for this tier's rate limit
	Window time.Duration
}

// UserTier represents a user's service tier.
type UserTier string

const (
	// TierAdmin has the highest rate limits (typically for administrators)
	TierAdmin UserTier = "admin"

	// TierPremium has elevated rate limits (for paying customers)
	TierPremium UserTier = "premium"

	// TierBasic has standard rate limits (for regular users)
	TierBasic UserTier = "basic"

	// TierViewer has the lowest rate limits (for read-only access)
	TierViewer UserTier = "viewer"
)

// String returns the string representation of the user tier.
func (t UserTier) String() string {
	return string(t)
}

// IsValid checks if the user tier is a recognized value.
func (t UserTier) IsValid() bool {
	switch t {
	case TierAdmin, TierPremium, TierBasic, TierViewer:
		return true
	default:
		return false
	}
}

// Validate checks if the RateLimitConfig is valid.
//
// Returns an error if any configuration values are invalid.
func (c *RateLimitConfig) Validate() error {
	// Validate IP rate limit
	if c.DefaultIPLimit < 0 {
		return fmt.Errorf("DefaultIPLimit must be non-negative, got %d", c.DefaultIPLimit)
	}
	if c.DefaultIPWindow < 0 {
		return fmt.Errorf("DefaultIPWindow must be non-negative, got %s", c.DefaultIPWindow)
	}

	// Validate user rate limit
	if c.DefaultUserLimit < 0 {
		return fmt.Errorf("DefaultUserLimit must be non-negative, got %d", c.DefaultUserLimit)
	}
	if c.DefaultUserWindow < 0 {
		return fmt.Errorf("DefaultUserWindow must be non-negative, got %s", c.DefaultUserWindow)
	}

	// Validate memory management settings
	if c.MaxActiveKeys < 0 {
		return fmt.Errorf("MaxActiveKeys must be non-negative, got %d", c.MaxActiveKeys)
	}
	if c.CleanupInterval < 0 {
		return fmt.Errorf("CleanupInterval must be non-negative, got %s", c.CleanupInterval)
	}
	if c.CleanupMaxAge < 0 {
		return fmt.Errorf("CleanupMaxAge must be non-negative, got %s", c.CleanupMaxAge)
	}

	// Validate circuit breaker settings
	if c.CircuitBreakerFailureThreshold < 0 {
		return fmt.Errorf("CircuitBreakerFailureThreshold must be non-negative, got %d", c.CircuitBreakerFailureThreshold)
	}
	if c.CircuitBreakerResetTimeout < 0 {
		return fmt.Errorf("CircuitBreakerResetTimeout must be non-negative, got %s", c.CircuitBreakerResetTimeout)
	}

	// Validate endpoint overrides
	for i, override := range c.EndpointOverrides {
		if override.PathPattern == "" {
			return fmt.Errorf("EndpointOverrides[%d].PathPattern cannot be empty", i)
		}
		if override.IPLimit < 0 {
			return fmt.Errorf("EndpointOverrides[%d].IPLimit must be non-negative, got %d", i, override.IPLimit)
		}
		if override.IPWindow < 0 {
			return fmt.Errorf("EndpointOverrides[%d].IPWindow must be non-negative, got %s", i, override.IPWindow)
		}
		if override.UserLimit < 0 {
			return fmt.Errorf("EndpointOverrides[%d].UserLimit must be non-negative, got %d", i, override.UserLimit)
		}
		if override.UserWindow < 0 {
			return fmt.Errorf("EndpointOverrides[%d].UserWindow must be non-negative, got %s", i, override.UserWindow)
		}
	}

	// Validate tier limits
	for i, tierLimit := range c.TierLimits {
		if !tierLimit.Tier.IsValid() {
			return fmt.Errorf("TierLimits[%d].Tier has invalid value %q", i, tierLimit.Tier)
		}
		if tierLimit.Limit < 0 {
			return fmt.Errorf("TierLimits[%d].Limit must be non-negative, got %d", i, tierLimit.Limit)
		}
		if tierLimit.Window < 0 {
			return fmt.Errorf("TierLimits[%d].Window must be non-negative, got %s", i, tierLimit.Window)
		}
	}

	return nil
}

// ApplyDefaults sets safe default values for any missing or zero configuration values.
//
// This ensures the rate limiter can function even if the configuration is incomplete.
func (c *RateLimitConfig) ApplyDefaults() {
	// IP rate limiting defaults
	if c.DefaultIPLimit == 0 {
		c.DefaultIPLimit = 100 // 100 requests per minute
	}
	if c.DefaultIPWindow == 0 {
		c.DefaultIPWindow = 1 * time.Minute
	}

	// User rate limiting defaults
	if c.DefaultUserLimit == 0 {
		c.DefaultUserLimit = 1000 // 1000 requests per hour
	}
	if c.DefaultUserWindow == 0 {
		c.DefaultUserWindow = 1 * time.Hour
	}

	// Memory management defaults
	if c.MaxActiveKeys == 0 {
		c.MaxActiveKeys = 10000 // Maximum 10,000 unique IPs/users in memory
	}
	if c.CleanupInterval == 0 {
		c.CleanupInterval = 5 * time.Minute // Cleanup every 5 minutes
	}
	if c.CleanupMaxAge == 0 {
		c.CleanupMaxAge = 1 * time.Hour // Remove entries older than 1 hour
	}

	// Circuit breaker defaults
	if c.CircuitBreakerFailureThreshold == 0 {
		c.CircuitBreakerFailureThreshold = 10 // Open circuit after 10 consecutive failures
	}
	if c.CircuitBreakerResetTimeout == 0 {
		c.CircuitBreakerResetTimeout = 30 * time.Second // Try half-open after 30 seconds
	}

	// Enabled by default
	if !c.Enabled {
		c.Enabled = true
	}
}

// GetTierLimit returns the rate limit configuration for a specific user tier.
//
// If no tier-specific limit is configured, it returns the default user limit.
//
// Parameters:
//   - tier: The user tier to look up
//
// Returns the limit and window for the tier.
func (c *RateLimitConfig) GetTierLimit(tier UserTier) (limit int, window time.Duration) {
	// Search for tier-specific configuration
	for _, tierLimit := range c.TierLimits {
		if tierLimit.Tier == tier {
			return tierLimit.Limit, tierLimit.Window
		}
	}

	// Fall back to default user limit
	return c.DefaultUserLimit, c.DefaultUserWindow
}

// GetEndpointLimit returns the rate limit configuration for a specific endpoint.
//
// If no endpoint-specific override is configured, it returns the default limits.
//
// Parameters:
//   - pathPattern: The endpoint path pattern to look up
//
// Returns IP and user limits and windows for the endpoint.
func (c *RateLimitConfig) GetEndpointLimit(pathPattern string) (ipLimit int, ipWindow time.Duration, userLimit int, userWindow time.Duration) {
	// Search for endpoint-specific configuration
	for _, override := range c.EndpointOverrides {
		if override.PathPattern == pathPattern {
			return override.IPLimit, override.IPWindow, override.UserLimit, override.UserWindow
		}
	}

	// Fall back to defaults
	return c.DefaultIPLimit, c.DefaultIPWindow, c.DefaultUserLimit, c.DefaultUserWindow
}

// DefaultConfig returns a RateLimitConfig with safe default values.
//
// This is useful for testing and as a starting point for configuration.
func DefaultConfig() *RateLimitConfig {
	config := &RateLimitConfig{}
	config.ApplyDefaults()
	return config
}
