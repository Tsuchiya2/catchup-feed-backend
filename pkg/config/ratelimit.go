package config

import (
	"fmt"
	"log/slog"
	"time"

	"catchup-feed/pkg/ratelimit"
)

// LoadRateLimitConfig loads rate limiting configuration from environment variables.
//
// This function reads all rate limiting configuration from environment variables
// and returns a validated RateLimitConfig. If any values are invalid, it logs
// warnings and uses safe defaults instead of failing.
//
// Environment variables:
//   - RATELIMIT_ENABLED: Enable/disable rate limiting (default: true)
//   - RATELIMIT_IP_ENABLED: Enable IP-based rate limiting (default: true)
//   - RATELIMIT_USER_ENABLED: Enable user-based rate limiting (default: true)
//   - RATELIMIT_IP_LIMIT: IP rate limit (requests per window) (default: 100)
//   - RATELIMIT_IP_WINDOW: IP rate limit window (default: 1m)
//   - RATELIMIT_USER_LIMIT: User rate limit (requests per window) (default: 1000)
//   - RATELIMIT_USER_WINDOW: User rate limit window (default: 1h)
//   - RATELIMIT_MAX_KEYS: Maximum keys in memory (default: 10000)
//   - RATELIMIT_CLEANUP_INTERVAL: Cleanup interval (default: 5m)
//   - RATELIMIT_CB_FAILURE_THRESHOLD: Circuit breaker failure threshold (default: 10)
//   - RATELIMIT_CB_RECOVERY_TIMEOUT: Circuit breaker recovery timeout (default: 30s)
//
// Returns:
//   - *ratelimit.RateLimitConfig: Validated configuration with defaults applied
//   - error: Always nil (validation failures result in warnings and defaults)
//
// Example:
//
//	config, err := LoadRateLimitConfig()
//	if err != nil {
//	    return fmt.Errorf("failed to load rate limit config: %w", err)
//	}
func LoadRateLimitConfig() (*ratelimit.RateLimitConfig, error) {
	config := &ratelimit.RateLimitConfig{}

	// Feature flags
	config.Enabled = GetEnvBool("RATELIMIT_ENABLED", true)

	// IP-based rate limiting
	ipLimit := GetEnvInt("RATELIMIT_IP_LIMIT", 100)
	if ipLimit < 0 {
		slog.Warn("invalid RATELIMIT_IP_LIMIT, using default",
			slog.Int("value", ipLimit),
			slog.Int("default", 100))
		ipLimit = 100
	}
	config.DefaultIPLimit = ipLimit

	ipWindow := GetEnvDuration("RATELIMIT_IP_WINDOW", 1*time.Minute)
	if err := ValidatePositiveDuration(ipWindow); err != nil {
		slog.Warn("invalid RATELIMIT_IP_WINDOW, using default",
			slog.String("value", ipWindow.String()),
			slog.String("default", "1m"),
			slog.String("error", err.Error()))
		ipWindow = 1 * time.Minute
	}
	config.DefaultIPWindow = ipWindow

	// User-based rate limiting
	userLimit := GetEnvInt("RATELIMIT_USER_LIMIT", 1000)
	if userLimit < 0 {
		slog.Warn("invalid RATELIMIT_USER_LIMIT, using default",
			slog.Int("value", userLimit),
			slog.Int("default", 1000))
		userLimit = 1000
	}
	config.DefaultUserLimit = userLimit

	userWindow := GetEnvDuration("RATELIMIT_USER_WINDOW", 1*time.Hour)
	if err := ValidatePositiveDuration(userWindow); err != nil {
		slog.Warn("invalid RATELIMIT_USER_WINDOW, using default",
			slog.String("value", userWindow.String()),
			slog.String("default", "1h"),
			slog.String("error", err.Error()))
		userWindow = 1 * time.Hour
	}
	config.DefaultUserWindow = userWindow

	// Tier-based limits (per hour)
	config.TierLimits = loadTierLimits()

	// Memory management
	maxKeys := GetEnvInt("RATELIMIT_MAX_KEYS", 10000)
	if maxKeys < 0 {
		slog.Warn("invalid RATELIMIT_MAX_KEYS, using default",
			slog.Int("value", maxKeys),
			slog.Int("default", 10000))
		maxKeys = 10000
	}
	config.MaxActiveKeys = maxKeys

	cleanupInterval := GetEnvDuration("RATELIMIT_CLEANUP_INTERVAL", 5*time.Minute)
	if err := ValidatePositiveDuration(cleanupInterval); err != nil {
		slog.Warn("invalid RATELIMIT_CLEANUP_INTERVAL, using default",
			slog.String("value", cleanupInterval.String()),
			slog.String("default", "5m"),
			slog.String("error", err.Error()))
		cleanupInterval = 5 * time.Minute
	}
	config.CleanupInterval = cleanupInterval

	// CleanupMaxAge - not exposed as env var, use 1 hour default
	config.CleanupMaxAge = 1 * time.Hour

	// Circuit breaker
	cbFailureThreshold := GetEnvInt("RATELIMIT_CB_FAILURE_THRESHOLD", 10)
	if cbFailureThreshold < 0 {
		slog.Warn("invalid RATELIMIT_CB_FAILURE_THRESHOLD, using default",
			slog.Int("value", cbFailureThreshold),
			slog.Int("default", 10))
		cbFailureThreshold = 10
	}
	config.CircuitBreakerFailureThreshold = cbFailureThreshold

	cbResetTimeout := GetEnvDuration("RATELIMIT_CB_RECOVERY_TIMEOUT", 30*time.Second)
	if err := ValidatePositiveDuration(cbResetTimeout); err != nil {
		slog.Warn("invalid RATELIMIT_CB_RECOVERY_TIMEOUT, using default",
			slog.String("value", cbResetTimeout.String()),
			slog.String("default", "30s"),
			slog.String("error", err.Error()))
		cbResetTimeout = 30 * time.Second
	}
	config.CircuitBreakerResetTimeout = cbResetTimeout

	// Validate the entire configuration
	if err := config.Validate(); err != nil {
		slog.Warn("rate limit configuration validation failed, applying defaults",
			slog.String("error", err.Error()))
		config.ApplyDefaults()
	}

	return config, nil
}

// loadTierLimits loads tier-based rate limits from environment variables.
//
// Environment variables:
//   - RATELIMIT_TIER_ADMIN: Admin tier limit (default: 10000)
//   - RATELIMIT_TIER_PREMIUM: Premium tier limit (default: 5000)
//   - RATELIMIT_TIER_BASIC: Basic tier limit (default: 1000)
//   - RATELIMIT_TIER_VIEWER: Viewer tier limit (default: 500)
//
// All tier limits use a 1-hour window.
//
// Returns:
//   - []ratelimit.TierRateLimitConfig: Tier-based limits
func loadTierLimits() []ratelimit.TierRateLimitConfig {
	tierLimits := []ratelimit.TierRateLimitConfig{}

	// Admin tier (highest limits)
	adminLimit := GetEnvInt("RATELIMIT_TIER_ADMIN", 10000)
	if adminLimit < 0 {
		slog.Warn("invalid RATELIMIT_TIER_ADMIN, using default",
			slog.Int("value", adminLimit),
			slog.Int("default", 10000))
		adminLimit = 10000
	}
	tierLimits = append(tierLimits, ratelimit.TierRateLimitConfig{
		Tier:   ratelimit.TierAdmin,
		Limit:  adminLimit,
		Window: 1 * time.Hour,
	})

	// Premium tier
	premiumLimit := GetEnvInt("RATELIMIT_TIER_PREMIUM", 5000)
	if premiumLimit < 0 {
		slog.Warn("invalid RATELIMIT_TIER_PREMIUM, using default",
			slog.Int("value", premiumLimit),
			slog.Int("default", 5000))
		premiumLimit = 5000
	}
	tierLimits = append(tierLimits, ratelimit.TierRateLimitConfig{
		Tier:   ratelimit.TierPremium,
		Limit:  premiumLimit,
		Window: 1 * time.Hour,
	})

	// Basic tier
	basicLimit := GetEnvInt("RATELIMIT_TIER_BASIC", 1000)
	if basicLimit < 0 {
		slog.Warn("invalid RATELIMIT_TIER_BASIC, using default",
			slog.Int("value", basicLimit),
			slog.Int("default", 1000))
		basicLimit = 1000
	}
	tierLimits = append(tierLimits, ratelimit.TierRateLimitConfig{
		Tier:   ratelimit.TierBasic,
		Limit:  basicLimit,
		Window: 1 * time.Hour,
	})

	// Viewer tier (lowest limits)
	viewerLimit := GetEnvInt("RATELIMIT_TIER_VIEWER", 500)
	if viewerLimit < 0 {
		slog.Warn("invalid RATELIMIT_TIER_VIEWER, using default",
			slog.Int("value", viewerLimit),
			slog.Int("default", 500))
		viewerLimit = 500
	}
	tierLimits = append(tierLimits, ratelimit.TierRateLimitConfig{
		Tier:   ratelimit.TierViewer,
		Limit:  viewerLimit,
		Window: 1 * time.Hour,
	})

	return tierLimits
}

// CSPConfig contains the configuration for Content Security Policy.
//
// This struct holds settings for CSP headers, which help prevent
// cross-site scripting (XSS) and other code injection attacks.
type CSPConfig struct {
	// Enabled controls whether CSP headers are applied
	Enabled bool

	// ReportOnly sets the header to Content-Security-Policy-Report-Only
	// instead of Content-Security-Policy, which logs violations but does not enforce
	ReportOnly bool

	// TrustedScriptSources lists additional trusted script sources (e.g., CDN URLs)
	TrustedScriptSources []string

	// TrustedStyleSources lists additional trusted style sources (e.g., CDN URLs)
	TrustedStyleSources []string
}

// LoadCSPConfig loads Content Security Policy configuration from environment variables.
//
// Environment variables:
//   - CSP_ENABLED: Enable/disable CSP headers (default: true)
//   - CSP_REPORT_ONLY: Use report-only mode (default: false)
//
// Returns:
//   - *CSPConfig: CSP configuration
//   - error: Always nil
//
// Example:
//
//	config, err := LoadCSPConfig()
//	if err != nil {
//	    return fmt.Errorf("failed to load CSP config: %w", err)
//	}
func LoadCSPConfig() (*CSPConfig, error) {
	config := &CSPConfig{
		Enabled:    GetEnvBool("CSP_ENABLED", true),
		ReportOnly: GetEnvBool("CSP_REPORT_ONLY", false),
	}

	return config, nil
}

// ValidateTrustedProxies validates a list of CIDR ranges for trusted proxies.
//
// Each CIDR range must be in valid CIDR notation (e.g., "10.0.0.0/8").
//
// Parameters:
//   - cidrs: List of CIDR ranges to validate
//
// Returns:
//   - error: nil if all CIDRs are valid, error otherwise
//
// Example:
//
//	cidrs := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}
//	if err := ValidateTrustedProxies(cidrs); err != nil {
//	    return fmt.Errorf("invalid trusted proxies: %w", err)
//	}
func ValidateTrustedProxies(cidrs []string) error {
	// For now, this is a placeholder
	// A full implementation would parse each CIDR using net.ParseCIDR
	// and verify it's a valid IP range
	for _, cidr := range cidrs {
		if cidr == "" {
			return fmt.Errorf("CIDR cannot be empty")
		}
		// TODO: Add actual CIDR parsing and validation
	}
	return nil
}
