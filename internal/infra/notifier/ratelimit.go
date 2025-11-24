package notifier

import (
	"context"

	"golang.org/x/time/rate"
)

// RateLimiter implements token bucket algorithm for rate limiting.
// It prevents notification APIs from being overwhelmed with too many requests.
type RateLimiter struct {
	rate    rate.Limit
	burst   int
	limiter *rate.Limiter
}

// NewRateLimiter creates a new RateLimiter with the specified rate and burst capacity.
//
// Parameters:
//   - requestsPerSecond: Maximum sustained request rate (e.g., 2.0 for 2 requests per second)
//   - burst: Maximum number of requests that can be made in a burst (e.g., 5)
//
// The token bucket algorithm allows up to 'burst' requests immediately,
// then refills tokens at 'requestsPerSecond' rate.
//
// Example:
//
//	limiter := NewRateLimiter(2.0, 5)  // 2 req/s with burst of 5
func NewRateLimiter(requestsPerSecond float64, burst int) *RateLimiter {
	r := rate.Limit(requestsPerSecond)
	l := rate.NewLimiter(r, burst)

	return &RateLimiter{
		rate:    r,
		burst:   burst,
		limiter: l,
	}
}

// Allow blocks until a token is available or the context is canceled.
// It should be called before making a rate-limited request.
//
// Parameters:
//   - ctx: Context for cancellation control
//
// Returns:
//   - error: Non-nil if context was canceled or deadline exceeded
//
// Example:
//
//	if err := limiter.Allow(ctx); err != nil {
//	    return fmt.Errorf("rate limit error: %w", err)
//	}
//	// Proceed with the request
func (r *RateLimiter) Allow(ctx context.Context) error {
	return r.limiter.Wait(ctx)
}
