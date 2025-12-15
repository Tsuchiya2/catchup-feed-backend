// Package retry provides retry logic with exponential backoff and jitter.
// It helps handle transient failures gracefully by automatically retrying failed operations.
package retry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"syscall"
	"time"
)

// Config holds the configuration for retry logic.
type Config struct {
	// MaxAttempts is the maximum number of retry attempts
	MaxAttempts int

	// InitialDelay is the delay before the first retry
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries
	MaxDelay time.Duration

	// Multiplier is the multiplier for exponential backoff
	Multiplier float64

	// JitterFraction is the fraction of delay to add as random jitter (0.0 to 1.0)
	JitterFraction float64
}

// DefaultConfig returns a default retry configuration.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:    3,
		InitialDelay:   1 * time.Second,
		MaxDelay:       30 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}
}

// FeedFetchConfig returns configuration optimized for RSS feed fetching.
// Aggressive retry for transient network issues.
func FeedFetchConfig() Config {
	return Config{
		MaxAttempts:    5,
		InitialDelay:   1 * time.Second,
		MaxDelay:       30 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}
}

// AIAPIConfig returns configuration optimized for AI API calls.
// Moderate retry due to cost considerations.
func AIAPIConfig() Config {
	return Config{
		MaxAttempts:    3,
		InitialDelay:   2 * time.Second,
		MaxDelay:       10 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}
}

// DBConfig returns configuration optimized for database operations.
// Fast retry for transient connection issues.
func DBConfig() Config {
	return Config{
		MaxAttempts:    3,
		InitialDelay:   100 * time.Millisecond,
		MaxDelay:       1 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}
}

// WebScraperConfig returns configuration optimized for web scraping.
// Moderate retry for network issues and transient site failures.
func WebScraperConfig() Config {
	return Config{
		MaxAttempts:    3,
		InitialDelay:   1 * time.Second,
		MaxDelay:       10 * time.Second,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}
}

// WithBackoff executes the given function with retry logic and exponential backoff.
// It returns nil if the function succeeds, or the last error if all attempts fail.
func WithBackoff(ctx context.Context, cfg Config, fn func() error) error {
	var lastErr error
	delay := cfg.InitialDelay

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		// Execute the function
		lastErr = fn()

		// Success - return immediately
		if lastErr == nil {
			if attempt > 1 {
				slog.Info("operation succeeded after retry",
					slog.Int("attempt", attempt))
			}
			return nil
		}

		// Check if error is retryable
		if !IsRetryable(lastErr) {
			slog.Warn("non-retryable error, aborting",
				slog.Int("attempt", attempt),
				slog.Any("error", lastErr))
			return lastErr
		}

		// Don't wait after last attempt
		if attempt == cfg.MaxAttempts {
			break
		}

		// Log retry attempt
		slog.Warn("operation failed, retrying",
			slog.Int("attempt", attempt),
			slog.Int("max_attempts", cfg.MaxAttempts),
			slog.Duration("delay", delay),
			slog.Any("error", lastErr))

		// Wait with context cancellation support
		select {
		case <-time.After(delay):
			// Continue to next attempt
		case <-ctx.Done():
			return fmt.Errorf("retry aborted: %w", ctx.Err())
		}

		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * cfg.Multiplier)
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}

		// Add jitter to prevent thundering herd
		delay = addJitter(delay, cfg.JitterFraction)
	}

	return fmt.Errorf("max retry attempts (%d) exceeded: %w", cfg.MaxAttempts, lastErr)
}

// IsRetryable determines if an error is worth retrying.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context errors are not retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Network errors (timeout)
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// Syscall errors
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ETIMEDOUT) ||
		errors.Is(err, syscall.ENETUNREACH) {
		return true
	}

	// HTTP status codes
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		// 5xx server errors are retryable
		if httpErr.StatusCode >= 500 && httpErr.StatusCode < 600 {
			return true
		}
		// 429 Too Many Requests is retryable
		if httpErr.StatusCode == http.StatusTooManyRequests {
			return true
		}
		// 408 Request Timeout is retryable
		if httpErr.StatusCode == http.StatusRequestTimeout {
			return true
		}
	}

	return false
}

// HTTPError represents an HTTP error with status code.
type HTTPError struct {
	StatusCode int
	Message    string
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

// addJitter adds random jitter to a duration to prevent thundering herd.
func addJitter(duration time.Duration, jitterFraction float64) time.Duration {
	if jitterFraction <= 0 {
		return duration
	}
	if jitterFraction > 1.0 {
		jitterFraction = 1.0
	}
	// #nosec G404 -- Using math/rand is acceptable for jitter calculation.
	// Cryptographic randomness is not required for retry backoff jitter.
	jitter := time.Duration(rand.Float64() * float64(duration) * jitterFraction)
	return duration + jitter
}
