package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"catchup-feed/pkg/ratelimit"
)

// UserExtractor defines the interface for extracting user information from request context.
//
// This abstraction allows different authentication systems to provide user information
// to the rate limiter without coupling to specific JWT implementations.
type UserExtractor interface {
	// ExtractUser extracts the user identifier and tier from the request context.
	//
	// Parameters:
	//   - ctx: Request context containing user information
	//
	// Returns:
	//   - userID: User identifier (e.g., email address)
	//   - tier: User tier (admin, premium, basic, viewer)
	//   - ok: true if user was found in context, false otherwise
	ExtractUser(ctx context.Context) (userID string, tier ratelimit.UserTier, ok bool)
}

// JWTUserExtractor extracts user information from JWT claims stored in context.
//
// This implementation works with the existing JWT authentication middleware
// that stores the user email in context using the "user" key.
type JWTUserExtractor struct {
	// contextKey is the key used to retrieve user from context.
	// Default: "user" (matches auth middleware)
	contextKey interface{}

	// tierProvider optionally provides user tier lookup.
	// If nil, defaults to TierBasic for all users.
	tierProvider UserTierProvider
}

// UserTierProvider defines the interface for looking up user tiers.
//
// This allows integration with user management systems to determine
// user service tiers dynamically.
type UserTierProvider interface {
	// GetUserTier returns the service tier for a given user.
	//
	// Parameters:
	//   - ctx: Request context
	//   - userID: User identifier
	//
	// Returns the user tier or TierBasic if tier cannot be determined.
	GetUserTier(ctx context.Context, userID string) ratelimit.UserTier
}

// DefaultTierProvider is a simple tier provider that returns TierBasic for all users.
type DefaultTierProvider struct{}

// GetUserTier returns TierBasic for all users.
func (p *DefaultTierProvider) GetUserTier(ctx context.Context, userID string) ratelimit.UserTier {
	return ratelimit.TierBasic
}

// NewJWTUserExtractor creates a JWTUserExtractor with the specified context key.
//
// Parameters:
//   - contextKey: The key used in context.WithValue for storing user
//   - tierProvider: Provider for looking up user tiers (can be nil for default)
//
// If tierProvider is nil, all users are assigned TierBasic.
func NewJWTUserExtractor(contextKey interface{}, tierProvider UserTierProvider) *JWTUserExtractor {
	if tierProvider == nil {
		tierProvider = &DefaultTierProvider{}
	}

	return &JWTUserExtractor{
		contextKey:   contextKey,
		tierProvider: tierProvider,
	}
}

// ExtractUser retrieves user information from the request context.
//
// This method expects the auth middleware to have already validated the JWT
// and stored the user email in context.
func (e *JWTUserExtractor) ExtractUser(ctx context.Context) (userID string, tier ratelimit.UserTier, ok bool) {
	// Extract user from context (set by auth middleware)
	userValue := ctx.Value(e.contextKey)
	if userValue == nil {
		return "", "", false
	}

	userID, ok = userValue.(string)
	if !ok || userID == "" {
		return "", "", false
	}

	// Get user tier from provider
	tier = e.tierProvider.GetUserTier(ctx, userID)

	return userID, tier, true
}

// UserRateLimiterConfig holds configuration for user-based rate limiting.
type UserRateLimiterConfig struct {
	// Store is the storage backend for rate limit state
	Store ratelimit.RateLimitStore

	// Algorithm is the rate limiting algorithm
	Algorithm ratelimit.RateLimitAlgorithm

	// Metrics records rate limiting events
	Metrics ratelimit.RateLimitMetrics

	// CircuitBreaker protects against failures
	CircuitBreaker *ratelimit.CircuitBreaker

	// UserExtractor extracts user info from request context
	UserExtractor UserExtractor

	// TierLimits maps user tiers to their rate limits
	// Format: map[UserTier]TierLimit
	TierLimits map[ratelimit.UserTier]TierLimit

	// DefaultLimit is used when tier is not found in TierLimits
	DefaultLimit int

	// DefaultWindow is used when tier is not found in TierLimits
	DefaultWindow time.Duration

	// SkipUnauthenticated determines whether to skip rate limiting for
	// unauthenticated requests (no user in context).
	// Use SkipUnauthenticatedPtr for explicit control, or leave nil for default (true).
	// This field is deprecated, use SkipUnauthenticatedPtr instead.
	SkipUnauthenticated bool

	// SkipUnauthenticatedPtr allows explicit control over unauthenticated request handling.
	// - nil: Use default behavior (skip rate limiting for unauthenticated requests)
	// - *true: Skip rate limiting for unauthenticated requests
	// - *false: Apply rate limiting to unauthenticated requests as "anonymous" user
	SkipUnauthenticatedPtr *bool

	// Clock provides time abstraction for testing
	Clock ratelimit.Clock
}

// TierLimit defines the rate limit for a specific user tier.
type TierLimit struct {
	Limit  int
	Window time.Duration
}

// UserRateLimiter implements user-based rate limiting with tier support.
//
// This middleware applies rate limits based on the authenticated user's identity
// and service tier. It integrates with the JWT authentication middleware to
// extract user information from request context.
//
// Features:
// - Tier-based rate limits (admin, premium, basic, viewer)
// - Graceful handling of unauthenticated requests
// - Circuit breaker for fault tolerance
// - Prometheus metrics integration
// - Standard rate limit headers (X-RateLimit-*)
type UserRateLimiter struct {
	config UserRateLimiterConfig
}

// NewUserRateLimiter creates a new user-based rate limiter.
//
// Parameters:
//   - config: Configuration for the user rate limiter
//
// If config.DefaultLimit is 0, defaults to 1000 requests per hour.
// If config.DefaultWindow is 0, defaults to 1 hour.
// If config.Clock is nil, defaults to SystemClock.
//
// For SkipUnauthenticated handling:
//   - If config.SkipUnauthenticatedPtr is set, that value is used (recommended)
//   - If config.SkipUnauthenticatedPtr is nil:
//   - If config.SkipUnauthenticated is explicitly set to true, use true
//   - Otherwise, the value of config.SkipUnauthenticated is used directly
//     (false means rate limit anonymous users, true means skip them)
func NewUserRateLimiter(config UserRateLimiterConfig) *UserRateLimiter {
	// Apply defaults
	if config.DefaultLimit == 0 {
		config.DefaultLimit = 1000
	}
	if config.DefaultWindow == 0 {
		config.DefaultWindow = 1 * time.Hour
	}
	if config.Clock == nil {
		config.Clock = &ratelimit.SystemClock{}
	}

	// Handle SkipUnauthenticated logic:
	// If SkipUnauthenticatedPtr is already set, use it.
	// Otherwise, convert the deprecated SkipUnauthenticated field to pointer.
	// This allows explicit false values to work correctly.
	if config.SkipUnauthenticatedPtr == nil {
		// Convert deprecated field to pointer
		// Note: If neither field is set, SkipUnauthenticated defaults to false (Go zero value)
		// which means anonymous users WILL be rate limited by default.
		// For backward compatibility, most users should set SkipUnauthenticatedPtr = BoolPtr(true)
		// or SkipUnauthenticated = true explicitly if they want to skip anonymous users.
		config.SkipUnauthenticatedPtr = &config.SkipUnauthenticated
	}

	return &UserRateLimiter{
		config: config,
	}
}

// Middleware returns an HTTP middleware handler that enforces user-based rate limiting.
//
// Behavior:
// - If user is not in context (unauthenticated) and SkipUnauthenticated=true: Allow request
// - If user is in context: Check rate limit based on user tier
// - If rate limit exceeded: Return 429 Too Many Requests
// - If rate limit check fails and circuit is open: Allow request (fail-open)
// - If rate limit is within limit: Allow request and set rate limit headers
//
// Rate limit headers set on all responses:
// - X-RateLimit-Limit: Maximum requests allowed
// - X-RateLimit-Remaining: Remaining requests in current window
// - X-RateLimit-Reset: Unix timestamp when limit resets
// - X-RateLimit-Type: "user"
// - Retry-After: Seconds to wait (only when rate limit exceeded)
func (rl *UserRateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract user from context
			userID, tier, ok := rl.config.UserExtractor.ExtractUser(r.Context())

			// Handle unauthenticated requests
			if !ok {
				// Check if we should skip rate limiting for unauthenticated requests
				skipUnauthenticated := true // default
				if rl.config.SkipUnauthenticatedPtr != nil {
					skipUnauthenticated = *rl.config.SkipUnauthenticatedPtr
				}

				if skipUnauthenticated {
					// Skip rate limiting for unauthenticated requests
					slog.Debug("user rate limiter: skipping unauthenticated request",
						slog.String("path", r.URL.Path),
						slog.String("method", r.Method),
					)
					next.ServeHTTP(w, r)
					return
				}

				// If not skipping, treat as "anonymous" user with Viewer tier (most restrictive)
				userID = "anonymous"
				tier = ratelimit.TierBasic
			}

			// Get rate limit for this user's tier
			limit, window := rl.getTierLimit(tier)

			// Hash user ID for privacy in storage
			hashedUserID := hashUserID(userID)

			// Track rate limit check start time for metrics
			startTime := rl.config.Clock.Now()

			// Check rate limit via circuit breaker
			var decision *ratelimit.RateLimitDecision
			var checkErr error

			circuitErr := rl.config.CircuitBreaker.Execute(func() error {
				decision, checkErr = rl.config.Algorithm.IsAllowed(
					r.Context(),
					hashedUserID,
					rl.config.Store,
					limit,
					window,
				)
				return checkErr
			})

			// Record check duration
			duration := rl.config.Clock.Now().Sub(startTime)
			rl.config.Metrics.RecordCheckDuration("user", duration)

			// Handle circuit breaker open (fail-open behavior)
			if rl.config.CircuitBreaker.IsOpen() {
				slog.Warn("user rate limiter: circuit breaker open, allowing request",
					slog.String("user_hash", hashedUserID[:16]), // First 16 chars for logging
					slog.String("tier", tier.String()),
					slog.String("path", r.URL.Path),
				)

				// Allow request but don't add timestamp
				next.ServeHTTP(w, r)
				return
			}

			// Handle rate limit check error (shouldn't happen if circuit breaker works)
			if circuitErr != nil {
				slog.Error("user rate limiter: check failed",
					slog.String("error", circuitErr.Error()),
					slog.String("user_hash", hashedUserID[:16]),
					slog.String("tier", tier.String()),
				)

				// Fail-open: allow request
				next.ServeHTTP(w, r)
				return
			}

			// Ensure decision is set
			if decision == nil {
				slog.Error("user rate limiter: nil decision returned",
					slog.String("user_hash", hashedUserID[:16]),
					slog.String("tier", tier.String()),
				)

				// Fail-open: allow request
				next.ServeHTTP(w, r)
				return
			}

			// Update decision metadata
			decision.LimiterType = "user"

			// Log rate limit check event at DEBUG level
			slog.Debug("rate limit check completed",
				slog.String("limiter_type", "user"),
				slog.String("key", hashedUserID[:16]),
				slog.String("tier", tier.String()),
				slog.Int("current", decision.Limit-decision.Remaining),
				slog.Int("limit", decision.Limit),
				slog.Duration("window", window),
				slog.Bool("allowed", decision.Allowed),
				slog.String("path", r.URL.Path),
			)

			// Set rate limit headers
			rl.setRateLimitHeaders(w, decision)

			// Check if request is allowed
			if !decision.Allowed {
				// Record denial
				rl.config.Metrics.RecordDenied("user", r.URL.Path)

				// Log rate limit exceeded event at WARN level
				slog.Warn("rate limit exceeded",
					slog.String("limiter_type", "user"),
					slog.String("key", hashedUserID[:16]),
					slog.String("tier", tier.String()),
					slog.Int("current", decision.Limit-decision.Remaining),
					slog.Int("limit", decision.Limit),
					slog.Int64("retry_after", decision.RetryAfterSeconds()),
					slog.String("path", r.URL.Path),
					slog.String("method", r.Method),
				)

				// Write error response
				rl.writeRateLimitError(w, decision)
				return
			}

			// Record allowed request
			rl.config.Metrics.RecordAllowed("user", r.URL.Path)

			// Request is within rate limit, proceed
			next.ServeHTTP(w, r)
		})
	}
}

// getTierLimit returns the rate limit configuration for a user tier.
//
// Returns the tier-specific limit if configured, otherwise returns default.
func (rl *UserRateLimiter) getTierLimit(tier ratelimit.UserTier) (int, time.Duration) {
	if tierLimit, ok := rl.config.TierLimits[tier]; ok {
		return tierLimit.Limit, tierLimit.Window
	}

	// Fall back to default limit
	return rl.config.DefaultLimit, rl.config.DefaultWindow
}

// setRateLimitHeaders sets standard rate limit headers on the response.
//
// Headers set:
// - X-RateLimit-Limit: Maximum requests allowed
// - X-RateLimit-Remaining: Remaining requests in current window
// - X-RateLimit-Reset: Unix timestamp when limit resets
// - X-RateLimit-Type: "user"
func (rl *UserRateLimiter) setRateLimitHeaders(w http.ResponseWriter, decision *ratelimit.RateLimitDecision) {
	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", decision.Limit))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", decision.Remaining))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", decision.ResetAtUnix()))
	w.Header().Set("X-RateLimit-Type", decision.LimiterType)
}

// writeRateLimitError writes a 429 Too Many Requests error response.
//
// Response includes:
// - Status code: 429 Too Many Requests
// - Retry-After header: Seconds to wait before retrying
// - JSON body with error details
func (rl *UserRateLimiter) writeRateLimitError(w http.ResponseWriter, decision *ratelimit.RateLimitDecision) {
	// Set Retry-After header
	w.Header().Set("Retry-After", fmt.Sprintf("%d", decision.RetryAfterSeconds()))

	// Set Content-Type
	w.Header().Set("Content-Type", "application/json")

	// Write status code
	w.WriteHeader(http.StatusTooManyRequests)

	// Write JSON error body
	errorBody := fmt.Sprintf(`{
  "error": "rate limit exceeded",
  "message": "You have exceeded your hourly request quota. Please try again in %d seconds.",
  "retry_after_seconds": %d,
  "limit": %d,
  "window": "%s"
}`,
		decision.RetryAfterSeconds(),
		decision.RetryAfterSeconds(),
		decision.Limit,
		rl.config.DefaultWindow.String(),
	)

	if _, err := w.Write([]byte(errorBody)); err != nil {
		slog.Error("user rate limiter: failed to write error response",
			slog.String("error", err.Error()),
		)
	}
}

// hashUserID creates a SHA-256 hash of the user ID for privacy.
//
// This ensures user identifiers are not stored in plaintext in the rate limiter.
// The hash is deterministic, so the same user always gets the same hash.
//
// Parameters:
//   - userID: The user identifier (e.g., email address)
//
// Returns a hex-encoded SHA-256 hash.
func hashUserID(userID string) string {
	hash := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(hash[:])
}

// NewDefaultTierLimits returns default tier limits based on the design document.
//
// Default limits (per hour):
// - Admin: 10,000 requests/hour
// - Premium: 5,000 requests/hour
// - Basic: 1,000 requests/hour
// - Viewer: 500 requests/hour
func NewDefaultTierLimits() map[ratelimit.UserTier]TierLimit {
	return map[ratelimit.UserTier]TierLimit{
		ratelimit.TierAdmin: {
			Limit:  10000,
			Window: 1 * time.Hour,
		},
		ratelimit.TierPremium: {
			Limit:  5000,
			Window: 1 * time.Hour,
		},
		ratelimit.TierBasic: {
			Limit:  1000,
			Window: 1 * time.Hour,
		},
		ratelimit.TierViewer: {
			Limit:  500,
			Window: 1 * time.Hour,
		},
	}
}

// BoolPtr is a helper function to create a pointer to a bool value.
// This is useful for setting SkipUnauthenticatedPtr in UserRateLimiterConfig.
//
// Example:
//
//	config := UserRateLimiterConfig{
//	    SkipUnauthenticatedPtr: BoolPtr(false), // Explicitly rate limit anonymous users
//	}
func BoolPtr(v bool) *bool {
	return &v
}
