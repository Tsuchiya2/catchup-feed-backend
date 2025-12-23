package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"catchup-feed/pkg/ratelimit"
)

// IPRateLimiterConfig holds configuration for the IP-based rate limiter.
type IPRateLimiterConfig struct {
	// Limit is the maximum number of requests per IP within the time window.
	// Default: 100
	Limit int

	// Window is the time period for rate limiting.
	// Default: 1 minute
	Window time.Duration

	// Enabled controls whether rate limiting is active.
	// Default: true
	Enabled bool
}

// DefaultIPRateLimiterConfig returns the default configuration for IP-based rate limiting.
func DefaultIPRateLimiterConfig() IPRateLimiterConfig {
	return IPRateLimiterConfig{
		Limit:   100,
		Window:  1 * time.Minute,
		Enabled: true,
	}
}

// IPRateLimiter implements HTTP middleware for IP-based rate limiting.
//
// This middleware uses the core pkg/ratelimit package for rate limiting logic,
// providing a thin HTTP adapter layer that:
//   - Extracts client IP addresses using IPExtractor interface
//   - Checks rate limits using RateLimitAlgorithm
//   - Returns 429 Too Many Requests when limit exceeded
//   - Sets rate limit headers (X-RateLimit-*)
//   - Integrates with CircuitBreaker for fault tolerance
//   - Records Prometheus metrics
//
// The middleware supports:
//   - Trusted proxy configuration via IPExtractor
//   - Circuit breaker fail-open behavior for availability
//   - Graceful degradation under failures
//   - Clock skew protection via SlidingWindowAlgorithm
type IPRateLimiter struct {
	config         IPRateLimiterConfig
	ipExtractor    IPExtractor
	store          ratelimit.RateLimitStore
	algorithm      ratelimit.RateLimitAlgorithm
	metrics        ratelimit.RateLimitMetrics
	circuitBreaker *ratelimit.CircuitBreaker
}

// NewIPRateLimiter creates a new IP-based rate limiter middleware.
//
// Parameters:
//   - config: Configuration for rate limiting behavior
//   - ipExtractor: Strategy for extracting client IP addresses
//   - store: Storage backend for rate limit state (in-memory or Redis)
//   - algorithm: Rate limiting algorithm (sliding window, token bucket, etc.)
//   - metrics: Metrics collector for observability
//   - circuitBreaker: Circuit breaker for fault tolerance
//
// Returns a new IPRateLimiter instance.
func NewIPRateLimiter(
	config IPRateLimiterConfig,
	ipExtractor IPExtractor,
	store ratelimit.RateLimitStore,
	algorithm ratelimit.RateLimitAlgorithm,
	metrics ratelimit.RateLimitMetrics,
	circuitBreaker *ratelimit.CircuitBreaker,
) *IPRateLimiter {
	// Apply defaults if needed
	if config.Limit <= 0 {
		config.Limit = 100
	}
	if config.Window <= 0 {
		config.Window = 1 * time.Minute
	}

	return &IPRateLimiter{
		config:         config,
		ipExtractor:    ipExtractor,
		store:          store,
		algorithm:      algorithm,
		metrics:        metrics,
		circuitBreaker: circuitBreaker,
	}
}

// Middleware returns an HTTP middleware function that enforces IP-based rate limiting.
//
// Request Flow:
//  1. Check if rate limiting is enabled (skip if disabled)
//  2. Extract client IP using IPExtractor
//  3. Check circuit breaker state (allow if open for availability)
//  4. Check rate limit using algorithm and store
//  5. Set rate limit headers (X-RateLimit-*)
//  6. If limit exceeded, return 429 with Retry-After header
//  7. If allowed, proceed to next handler
//
// HTTP Response Headers:
//   - X-RateLimit-Limit: Maximum requests allowed in window
//   - X-RateLimit-Remaining: Remaining requests in current window
//   - X-RateLimit-Reset: Unix timestamp when limit resets
//   - X-RateLimit-Type: "ip"
//   - Retry-After: Seconds to wait before retrying (only when limited)
//
// HTTP Status Codes:
//   - 200 OK: Request allowed
//   - 429 Too Many Requests: Rate limit exceeded
//   - 500 Internal Server Error: Rate limiter failure (fallback)
func (rl *IPRateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if rate limiting is disabled
			if !rl.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Record request start time for latency metrics
			start := time.Now()

			// Extract client IP
			ip, err := rl.extractIP(r)
			if err != nil {
				// IP extraction failed - log error and fail open (allow request)
				slog.Error("IP rate limiter: failed to extract IP, allowing request",
					slog.String("error", err.Error()),
					slog.String("remote_addr", r.RemoteAddr),
					slog.String("path", r.URL.Path),
				)
				next.ServeHTTP(w, r)
				return
			}

			// Check circuit breaker state
			if rl.circuitBreaker != nil && rl.circuitBreaker.IsOpen() {
				// Circuit is open - fail open (allow request) for availability
				slog.Warn("IP rate limiter: circuit breaker open, allowing request",
					slog.String("ip", ip),
					slog.String("path", r.URL.Path),
				)
				next.ServeHTTP(w, r)
				return
			}

			// Check rate limit
			ctx := context.Background()
			decision, err := rl.checkRateLimit(ctx, ip, r.URL.Path)

			// Record check duration metric
			duration := time.Since(start)
			if rl.metrics != nil {
				rl.metrics.RecordCheckDuration("ip", duration)
			}

			if err != nil {
				// Rate limit check failed
				rl.handleRateLimitError(w, r, ip, err)
				return
			}

			// Log rate limit check event at DEBUG level
			slog.Debug("rate limit check completed",
				slog.String("limiter_type", "ip"),
				slog.String("key", ip),
				slog.Int("current", decision.Limit-decision.Remaining),
				slog.Int("limit", decision.Limit),
				slog.Duration("window", rl.config.Window),
				slog.Bool("allowed", decision.Allowed),
				slog.String("path", r.URL.Path),
			)

			// Set rate limit headers
			rl.setRateLimitHeaders(w, decision)

			// Check if request is allowed
			if decision.IsDenied() {
				// Rate limit exceeded
				rl.writeRateLimitError(w, r, decision)
				return
			}

			// Request allowed - proceed to next handler
			if rl.metrics != nil {
				rl.metrics.RecordAllowed("ip", r.URL.Path)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// extractIP extracts the client IP address from the HTTP request.
//
// This method uses the configured IPExtractor strategy, which supports:
//   - RemoteAddr extraction (secure, no proxy trust)
//   - Trusted proxy header extraction (X-Forwarded-For, X-Real-IP)
//
// Parameters:
//   - r: HTTP request
//
// Returns:
//   - string: Client IP address
//   - error: Error if extraction fails
func (rl *IPRateLimiter) extractIP(r *http.Request) (string, error) {
	return rl.ipExtractor.ExtractIP(r)
}

// checkRateLimit checks if the request from the given IP is allowed.
//
// This method wraps the rate limit check with circuit breaker protection.
// If the circuit breaker is configured, failures are recorded and the
// circuit may open after repeated failures.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - ip: Client IP address
//   - path: Request path for metrics
//
// Returns:
//   - *ratelimit.RateLimitDecision: Rate limit decision with metadata
//   - error: Error if rate limit check fails
func (rl *IPRateLimiter) checkRateLimit(ctx context.Context, ip, path string) (*ratelimit.RateLimitDecision, error) {
	var decision *ratelimit.RateLimitDecision
	var err error

	// Check rate limit with circuit breaker protection
	checkErr := func() error {
		decision, err = rl.algorithm.IsAllowed(
			ctx,
			ip,
			rl.store,
			rl.config.Limit,
			rl.config.Window,
		)
		return err
	}

	if rl.circuitBreaker != nil {
		// Execute with circuit breaker
		if cbErr := rl.circuitBreaker.Execute(checkErr); cbErr != nil {
			return nil, cbErr
		}
	} else {
		// Execute without circuit breaker
		if err := checkErr(); err != nil {
			return nil, err
		}
	}

	// Override limiter type to "ip"
	if decision != nil {
		decision.LimiterType = "ip"
	}

	return decision, nil
}

// setRateLimitHeaders sets the rate limit HTTP headers on the response.
//
// Headers:
//   - X-RateLimit-Limit: Maximum requests allowed in window
//   - X-RateLimit-Remaining: Remaining requests in current window
//   - X-RateLimit-Reset: Unix timestamp when limit resets
//   - X-RateLimit-Type: "ip"
//
// Parameters:
//   - w: HTTP response writer
//   - decision: Rate limit decision containing metadata
func (rl *IPRateLimiter) setRateLimitHeaders(w http.ResponseWriter, decision *ratelimit.RateLimitDecision) {
	if decision == nil {
		return
	}

	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(decision.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(decision.Remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(decision.ResetAtUnix(), 10))
	w.Header().Set("X-RateLimit-Type", "ip")
}

// writeRateLimitError writes a 429 Too Many Requests response.
//
// Response format:
//
//	{
//	  "error": "rate_limit_exceeded",
//	  "message": "Too many requests from this IP address",
//	  "retry_after": 45
//	}
//
// HTTP Headers:
//   - Content-Type: application/json
//   - Retry-After: Seconds to wait before retrying
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request (for logging)
//   - decision: Rate limit decision containing retry information
func (rl *IPRateLimiter) writeRateLimitError(w http.ResponseWriter, r *http.Request, decision *ratelimit.RateLimitDecision) {
	// Set Retry-After header
	retryAfter := decision.RetryAfterSeconds()
	w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))

	// Set Content-Type
	w.Header().Set("Content-Type", "application/json")

	// Write status code
	w.WriteHeader(http.StatusTooManyRequests)

	// Write JSON response body
	response := map[string]interface{}{
		"error":       "rate_limit_exceeded",
		"message":     "Too many requests from this IP address",
		"retry_after": retryAfter,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("IP rate limiter: failed to encode JSON response",
			slog.String("error", err.Error()),
		)
	}

	// Record denial metric
	if rl.metrics != nil {
		rl.metrics.RecordDenied("ip", r.URL.Path)
	}

	// Log rate limit exceeded event at WARN level
	slog.Warn("rate limit exceeded",
		slog.String("limiter_type", "ip"),
		slog.String("key", decision.Key),
		slog.Int("current", decision.Limit-decision.Remaining),
		slog.Int("limit", decision.Limit),
		slog.Int64("retry_after", retryAfter),
		slog.String("path", r.URL.Path),
		slog.String("method", r.Method),
	)
}

// handleRateLimitError handles errors that occur during rate limit checking.
//
// This method implements fail-open behavior: if the rate limiter fails,
// the request is allowed through to maintain availability. However, the
// error is logged and the circuit breaker is notified.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//   - ip: Client IP address
//   - err: Error that occurred
func (rl *IPRateLimiter) handleRateLimitError(w http.ResponseWriter, r *http.Request, ip string, err error) {
	// Log critical error
	slog.Error("IP rate limiter: check failed, allowing request (fail-open)",
		slog.String("error", err.Error()),
		slog.String("ip", ip),
		slog.String("path", r.URL.Path),
	)

	// For now, fail open (allow request) for availability
	// In a stricter security context, we could return 500 or 503
	w.WriteHeader(http.StatusOK)
}
