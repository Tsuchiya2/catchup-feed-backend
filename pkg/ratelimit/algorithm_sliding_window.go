package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// SlidingWindowAlgorithm implements a sliding window rate limiting algorithm.
//
// This algorithm tracks individual request timestamps and calculates the count
// of requests within a sliding time window. It provides accurate rate limiting
// without the burst spikes that can occur with fixed window algorithms.
//
// Features:
//   - Accurate request counting within time windows
//   - Clock skew protection to handle system time changes
//   - Thread-safe implementation with mutex protection
//   - Memory efficient (only stores timestamps)
//
// Algorithm:
//  1. Get current time from Clock interface
//  2. Calculate window start time (now - windowDuration)
//  3. Count requests in the time window using store
//  4. If count < limit, allow request and add to store
//  5. If count >= limit, deny request and calculate retry-after time
//
// Clock Skew Protection:
//   - Tracks the last seen timestamp per key
//   - If current time < last seen time (clock went backwards), use last seen time
//   - Logs warning when clock skew is detected
//   - Prevents rate limit bypass through time manipulation
type SlidingWindowAlgorithm struct {
	// clock provides time operations for testability
	clock Clock

	// mu protects lastTimestamps map
	mu sync.RWMutex

	// lastTimestamps tracks the last valid timestamp for each key
	// Used for clock skew protection
	lastTimestamps map[string]time.Time

	// windowDuration is the time window for rate limiting
	// This is set when IsAllowed is called with the window parameter
	windowDuration time.Duration
}

// NewSlidingWindowAlgorithm creates a new sliding window rate limiting algorithm.
//
// Parameters:
//   - clock: Clock interface for time operations (use SystemClock for production)
//
// Returns a new SlidingWindowAlgorithm instance.
func NewSlidingWindowAlgorithm(clock Clock) *SlidingWindowAlgorithm {
	if clock == nil {
		clock = &SystemClock{}
	}

	return &SlidingWindowAlgorithm{
		clock:          clock,
		lastTimestamps: make(map[string]time.Time),
	}
}

// IsAllowed determines whether a request should be allowed based on the
// sliding window rate limiting algorithm.
//
// Algorithm Steps:
//  1. Get validated timestamp (with clock skew protection)
//  2. Calculate window start time (now - window)
//  3. Atomically check count and add request if within limit
//  4. If count < limit, allow and record request
//  5. If count >= limit, deny and calculate retry time
//
// Thread Safety:
// This method uses atomic check-and-add operations when the store supports
// the AtomicRateLimitStore interface. This prevents TOCTOU race conditions
// where concurrent requests could bypass the rate limit.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - key: Unique identifier for rate limit subject (e.g., IP, user ID)
//   - store: Storage backend containing rate limit state
//   - limit: Maximum number of requests allowed in the window
//   - window: Time window for rate limiting
//
// Returns:
//   - *RateLimitDecision: Decision with metadata (allowed/denied, remaining, reset time)
//   - error: Error if the operation fails
func (a *SlidingWindowAlgorithm) IsAllowed(
	ctx context.Context,
	key string,
	store RateLimitStore,
	limit int,
	window time.Duration,
) (*RateLimitDecision, error) {
	// Store window duration for GetWindowDuration()
	a.windowDuration = window

	// Get validated timestamp with clock skew protection
	now := a.getValidTimestamp(key)

	// Calculate window start time (sliding window)
	cutoff := now.Add(-window)

	// Calculate reset time (when the oldest request will expire)
	resetAt := now.Add(window)

	// Check if store supports atomic operations to prevent TOCTOU race conditions
	if atomicStore, ok := store.(AtomicRateLimitStore); ok {
		return a.isAllowedAtomic(ctx, key, atomicStore, limit, cutoff, now, resetAt)
	}

	// Fall back to non-atomic operation for stores that don't support it
	return a.isAllowedNonAtomic(ctx, key, store, limit, cutoff, now, resetAt)
}

// isAllowedAtomic performs atomic check-and-add to prevent race conditions.
//
// This method uses the AtomicRateLimitStore.CheckAndAddRequest method to
// atomically check the count and add the request in a single operation.
func (a *SlidingWindowAlgorithm) isAllowedAtomic(
	ctx context.Context,
	key string,
	store AtomicRateLimitStore,
	limit int,
	cutoff time.Time,
	now time.Time,
	resetAt time.Time,
) (*RateLimitDecision, error) {
	// Atomically check and add request
	allowed, count, err := store.CheckAndAddRequest(ctx, key, now, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to check and add request: %w", err)
	}

	if allowed {
		// Calculate remaining requests (count is after adding)
		remaining := limit - count

		return NewAllowedDecision(key, "unknown", limit, remaining, resetAt), nil
	}

	// Request is denied - limit exceeded
	retryAfter := resetAt.Sub(now)

	decision := NewDeniedDecision(key, "unknown", limit, resetAt)
	decision.RetryAfter = retryAfter

	return decision, nil
}

// isAllowedNonAtomic performs non-atomic check-and-add for backward compatibility.
//
// WARNING: This method has a TOCTOU race condition. Use AtomicRateLimitStore
// implementations in production for proper thread safety.
func (a *SlidingWindowAlgorithm) isAllowedNonAtomic(
	ctx context.Context,
	key string,
	store RateLimitStore,
	limit int,
	cutoff time.Time,
	now time.Time,
	resetAt time.Time,
) (*RateLimitDecision, error) {
	// Get count of requests within the window
	count, err := store.GetRequestCount(ctx, key, cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to get request count: %w", err)
	}

	// Check if request is allowed
	if count < limit {
		// Request is allowed - record it
		if err := store.AddRequest(ctx, key, now); err != nil {
			return nil, fmt.Errorf("failed to add request: %w", err)
		}

		// Calculate remaining requests
		remaining := limit - count - 1 // -1 for the current request

		return NewAllowedDecision(key, "unknown", limit, remaining, resetAt), nil
	}

	// Request is denied - limit exceeded
	// Calculate retry-after time
	retryAfter := resetAt.Sub(now)

	decision := NewDeniedDecision(key, "unknown", limit, resetAt)
	decision.RetryAfter = retryAfter

	return decision, nil
}

// GetWindowDuration returns the effective time window used by this algorithm.
//
// This is the window duration that was last passed to IsAllowed().
// It is useful for calculating reset times and retry delays.
//
// Returns the time window duration.
func (a *SlidingWindowAlgorithm) GetWindowDuration() time.Duration {
	return a.windowDuration
}

// getValidTimestamp returns the current time with clock skew protection.
//
// Clock skew protection prevents rate limit bypass when the system clock
// goes backwards (e.g., due to NTP adjustment, manual time change, or
// clock synchronization issues).
//
// Algorithm:
//  1. Get current time from clock
//  2. Compare with last seen timestamp for this key
//  3. If current time < last seen time (clock went backwards):
//     - Log warning with clock skew details
//     - Use last seen time instead of current time
//  4. Update last seen timestamp
//  5. Return validated timestamp
//
// Parameters:
//   - key: Unique identifier for rate limit subject
//
// Returns:
//   - time.Time: Validated timestamp (either current time or last seen time)
//
// Thread Safety:
//   - Protected by mutex to ensure thread-safe access to lastTimestamps map
func (a *SlidingWindowAlgorithm) getValidTimestamp(key string) time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Get current time
	now := a.clock.Now()

	// Get last seen timestamp for this key
	lastSeen, exists := a.lastTimestamps[key]

	if exists && now.Before(lastSeen) {
		// Clock skew detected - current time is before last seen time
		skew := lastSeen.Sub(now)

		slog.Warn("clock skew detected, using last valid timestamp",
			slog.String("key", key),
			slog.Time("now", now),
			slog.Time("last_seen", lastSeen),
			slog.Duration("skew", skew),
		)

		// Use last seen time to prevent rate limit bypass
		return lastSeen
	}

	// Update last seen timestamp
	a.lastTimestamps[key] = now

	return now
}

// CleanupExpiredTimestamps removes expired timestamp tracking entries.
//
// This method should be called periodically to prevent memory leaks from
// accumulating lastTimestamps entries for keys that are no longer active.
//
// Parameters:
//   - maxAge: Remove entries older than this duration
//
// Returns the number of entries removed.
func (a *SlidingWindowAlgorithm) CleanupExpiredTimestamps(maxAge time.Duration) int {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.clock.Now()
	cutoff := now.Add(-maxAge)
	removed := 0

	for key, timestamp := range a.lastTimestamps {
		if timestamp.Before(cutoff) {
			delete(a.lastTimestamps, key)
			removed++
		}
	}

	if removed > 0 {
		slog.Debug("cleaned up expired timestamp entries",
			slog.Int("removed", removed),
			slog.Int("remaining", len(a.lastTimestamps)),
		)
	}

	return removed
}

// GetTrackedKeysCount returns the number of keys currently tracked for clock skew protection.
//
// This is useful for monitoring memory usage and triggering cleanup.
//
// Returns the count of tracked keys.
func (a *SlidingWindowAlgorithm) GetTrackedKeysCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return len(a.lastTimestamps)
}
