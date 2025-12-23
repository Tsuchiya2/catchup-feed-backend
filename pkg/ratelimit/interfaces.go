// Package ratelimit provides framework-agnostic rate limiting functionality.
//
// This package implements rate limiting using pluggable storage backends,
// algorithms, and metrics collectors. It is designed to be reusable across
// different contexts (HTTP, gRPC, CLI, background jobs).
package ratelimit

import (
	"context"
	"time"
)

// RateLimitStore defines the interface for storing and retrieving rate limit state.
//
// Implementations can use in-memory storage, Redis, databases, or other backends.
// All methods must be thread-safe.
type RateLimitStore interface {
	// AddRequest records a new request timestamp for the given key.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - key: Unique identifier (e.g., IP address, user ID)
	//   - timestamp: Time when the request occurred
	//
	// Returns an error if the operation fails.
	AddRequest(ctx context.Context, key string, timestamp time.Time) error

	// GetRequests retrieves all request timestamps for the given key
	// that occurred after the cutoff time.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - key: Unique identifier
	//   - cutoff: Only return timestamps after this time
	//
	// Returns a slice of timestamps and an error if the operation fails.
	GetRequests(ctx context.Context, key string, cutoff time.Time) ([]time.Time, error)

	// GetRequestCount returns the number of requests for the given key
	// that occurred after the cutoff time.
	//
	// This is a convenience method that can be more efficient than GetRequests
	// when only the count is needed.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - key: Unique identifier
	//   - cutoff: Only count timestamps after this time
	//
	// Returns the count and an error if the operation fails.
	GetRequestCount(ctx context.Context, key string, cutoff time.Time) (int, error)

	// Cleanup removes expired request timestamps from storage.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - cutoff: Remove timestamps older than this time
	//
	// Returns an error if the operation fails.
	Cleanup(ctx context.Context, cutoff time.Time) error

	// KeyCount returns the number of active keys currently in storage.
	//
	// This is useful for monitoring memory usage and triggering eviction.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//
	// Returns the count and an error if the operation fails.
	KeyCount(ctx context.Context) (int, error)

	// MemoryUsage returns the estimated memory usage in bytes.
	//
	// This is used for monitoring and triggering memory management strategies.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//
	// Returns the estimated memory usage in bytes and an error if the operation fails.
	MemoryUsage(ctx context.Context) (int64, error)
}

// RateLimitAlgorithm defines the interface for rate limiting algorithms.
//
// Different algorithms (sliding window, token bucket, fixed window) can be
// implemented to provide different rate limiting behaviors.
type RateLimitAlgorithm interface {
	// IsAllowed determines whether a request should be allowed based on the
	// current rate limit state.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - key: Unique identifier for the rate limit subject
	//   - store: Storage backend containing rate limit state
	//   - limit: Maximum number of requests allowed in the window
	//   - window: Time window for rate limiting
	//
	// Returns a RateLimitDecision containing the verdict and metadata.
	IsAllowed(ctx context.Context, key string, store RateLimitStore, limit int, window time.Duration) (*RateLimitDecision, error)

	// GetWindowDuration returns the effective time window used by this algorithm.
	//
	// This is useful for calculating reset times and retry delays.
	GetWindowDuration() time.Duration
}

// RateLimitMetrics defines the interface for recording rate limiting metrics.
//
// Implementations can use Prometheus, StatsD, or custom metrics systems.
type RateLimitMetrics interface {
	// RecordRequest records a rate limit check that resulted in an allowed request.
	//
	// Parameters:
	//   - limiterType: Type of rate limiter (e.g., "ip", "user")
	//   - endpoint: API endpoint being accessed
	RecordRequest(limiterType, endpoint string)

	// RecordDenied records a rate limit violation (request denied).
	//
	// Parameters:
	//   - limiterType: Type of rate limiter (e.g., "ip", "user")
	//   - endpoint: API endpoint being accessed
	RecordDenied(limiterType, endpoint string)

	// RecordAllowed records a rate limit check that resulted in an allowed request.
	//
	// This is the same as RecordRequest but provides a more explicit name.
	//
	// Parameters:
	//   - limiterType: Type of rate limiter (e.g., "ip", "user")
	//   - endpoint: API endpoint being accessed
	RecordAllowed(limiterType, endpoint string)

	// RecordCheckDuration records the duration of a rate limit check operation.
	//
	// Parameters:
	//   - limiterType: Type of rate limiter (e.g., "ip", "user")
	//   - duration: Time taken to perform the rate limit check
	RecordCheckDuration(limiterType string, duration time.Duration)

	// SetActiveKeys records the current number of active keys in the rate limiter.
	//
	// Parameters:
	//   - limiterType: Type of rate limiter (e.g., "ip", "user")
	//   - count: Number of active keys
	SetActiveKeys(limiterType string, count int)

	// RecordCircuitState records the current state of the circuit breaker.
	//
	// Parameters:
	//   - limiterType: Type of rate limiter (e.g., "ip", "user")
	//   - state: Circuit state (e.g., "closed", "open", "half-open")
	RecordCircuitState(limiterType, state string)

	// RecordDegradationLevel records the current degradation level.
	//
	// Parameters:
	//   - limiterType: Type of rate limiter (e.g., "ip", "user")
	//   - level: Degradation level (0=normal, 1=relaxed, 2=minimal, 3=disabled)
	RecordDegradationLevel(limiterType string, level int)

	// RecordEviction records that keys were evicted from the store.
	//
	// Parameters:
	//   - limiterType: Type of rate limiter (e.g., "ip", "user")
	//   - count: Number of keys evicted
	RecordEviction(limiterType string, count int)
}

// Clock provides an abstraction for time operations to enable testing.
//
// This interface allows for dependency injection of time functions,
// making it easy to test time-dependent behavior with fake clocks.
type Clock interface {
	// Now returns the current time.
	//
	// Production implementations should return time.Now().
	// Test implementations can return fixed or controlled times.
	Now() time.Time
}

// SystemClock is a Clock implementation that uses the system time.
type SystemClock struct{}

// Now returns the current system time.
func (c *SystemClock) Now() time.Time {
	return time.Now()
}

// AtomicRateLimitStore extends RateLimitStore with atomic check-and-add operations.
//
// This interface provides atomic rate limiting operations that prevent
// TOCTOU (Time-of-Check to Time-of-Use) race conditions in concurrent scenarios.
// Implementations that support atomic operations should implement this interface.
type AtomicRateLimitStore interface {
	RateLimitStore

	// CheckAndAddRequest atomically checks if a request is within the rate limit
	// and adds it to the store if allowed.
	//
	// This method MUST be atomic - the check and add must happen within
	// a single lock acquisition to prevent race conditions.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout
	//   - key: Unique identifier for the rate limit subject
	//   - timestamp: Time when the request occurred
	//   - cutoff: Only count timestamps after this time
	//   - limit: Maximum number of requests allowed
	//
	// Returns:
	//   - allowed: true if the request was within limit and added
	//   - count: Current count of requests in the window (after adding if allowed)
	//   - err: Error if the operation fails
	CheckAndAddRequest(ctx context.Context, key string, timestamp time.Time, cutoff time.Time, limit int) (allowed bool, count int, err error)
}
