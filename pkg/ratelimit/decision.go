package ratelimit

import (
	"fmt"
	"time"
)

// RateLimitDecision represents the result of a rate limit check.
//
// This domain model encapsulates all information about whether a request
// should be allowed, along with metadata for the client to understand
// the current rate limit state.
type RateLimitDecision struct {
	// Key is the identifier used for rate limiting (e.g., IP address, user ID).
	Key string

	// Allowed indicates whether the request should be permitted.
	// - true: Request is within the rate limit
	// - false: Request exceeds the rate limit and should be rejected
	Allowed bool

	// Limit is the maximum number of requests allowed in the time window.
	Limit int

	// Remaining is the number of requests remaining in the current window.
	// - 0 means the limit has been reached
	// - Negative values indicate the request exceeded the limit
	Remaining int

	// ResetAt is the time when the rate limit window will reset.
	// Clients should wait until this time before retrying.
	ResetAt time.Time

	// RetryAfter is the duration the client should wait before retrying.
	// This is calculated as ResetAt - Now.
	RetryAfter time.Duration

	// LimiterType identifies which rate limiter made this decision.
	// Examples: "ip", "user", "endpoint"
	LimiterType string
}

// String returns a human-readable representation of the decision.
func (d *RateLimitDecision) String() string {
	if d.Allowed {
		return fmt.Sprintf(
			"RateLimitDecision{Allowed: true, Key: %s, Type: %s, Remaining: %d/%d, ResetAt: %s}",
			d.Key,
			d.LimiterType,
			d.Remaining,
			d.Limit,
			d.ResetAt.Format(time.RFC3339),
		)
	}

	return fmt.Sprintf(
		"RateLimitDecision{Allowed: false, Key: %s, Type: %s, Limit: %d, RetryAfter: %s, ResetAt: %s}",
		d.Key,
		d.LimiterType,
		d.Limit,
		d.RetryAfter.String(),
		d.ResetAt.Format(time.RFC3339),
	)
}

// IsAllowed returns true if the request is allowed.
//
// This is a convenience method equivalent to checking the Allowed field.
func (d *RateLimitDecision) IsAllowed() bool {
	return d.Allowed
}

// IsDenied returns true if the request is denied.
//
// This is a convenience method equivalent to checking !Allowed.
func (d *RateLimitDecision) IsDenied() bool {
	return !d.Allowed
}

// HasRemaining returns true if there are requests remaining in the current window.
func (d *RateLimitDecision) HasRemaining() bool {
	return d.Remaining > 0
}

// ResetAtUnix returns the reset time as a Unix timestamp.
//
// This is useful for HTTP headers like X-RateLimit-Reset.
func (d *RateLimitDecision) ResetAtUnix() int64 {
	return d.ResetAt.Unix()
}

// RetryAfterSeconds returns the retry delay in seconds.
//
// This is useful for HTTP headers like Retry-After.
func (d *RateLimitDecision) RetryAfterSeconds() int64 {
	seconds := int64(d.RetryAfter.Seconds())
	if seconds < 0 {
		return 0
	}
	return seconds
}

// NewAllowedDecision creates a RateLimitDecision for an allowed request.
//
// Parameters:
//   - key: The rate limit key (e.g., IP address, user ID)
//   - limiterType: Type of rate limiter (e.g., "ip", "user")
//   - limit: Maximum requests allowed in the window
//   - remaining: Requests remaining in the current window
//   - resetAt: Time when the rate limit window resets
//
// Returns a RateLimitDecision with Allowed=true.
func NewAllowedDecision(key, limiterType string, limit, remaining int, resetAt time.Time) *RateLimitDecision {
	retryAfter := time.Until(resetAt)
	if retryAfter < 0 {
		retryAfter = 0
	}

	return &RateLimitDecision{
		Key:         key,
		Allowed:     true,
		Limit:       limit,
		Remaining:   remaining,
		ResetAt:     resetAt,
		RetryAfter:  retryAfter,
		LimiterType: limiterType,
	}
}

// NewDeniedDecision creates a RateLimitDecision for a denied request.
//
// Parameters:
//   - key: The rate limit key (e.g., IP address, user ID)
//   - limiterType: Type of rate limiter (e.g., "ip", "user")
//   - limit: Maximum requests allowed in the window
//   - resetAt: Time when the rate limit window resets
//
// Returns a RateLimitDecision with Allowed=false and Remaining=0.
func NewDeniedDecision(key, limiterType string, limit int, resetAt time.Time) *RateLimitDecision {
	retryAfter := time.Until(resetAt)
	if retryAfter < 0 {
		retryAfter = 0
	}

	return &RateLimitDecision{
		Key:         key,
		Allowed:     false,
		Limit:       limit,
		Remaining:   0,
		ResetAt:     resetAt,
		RetryAfter:  retryAfter,
		LimiterType: limiterType,
	}
}
