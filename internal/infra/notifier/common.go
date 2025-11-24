package notifier

import (
	"errors"
	"fmt"
	"time"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const requestIDKey contextKey = "request_id"

// Common webhook error types used by Discord and Slack notifiers

// RateLimitError represents a 429 rate limit error from a webhook service.
type RateLimitError struct {
	RetryAfter time.Duration
	Message    string // Optional custom message
}

func (e *RateLimitError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s (retry after %v)", e.Message, e.RetryAfter)
	}
	return fmt.Sprintf("rate limit exceeded (retry after %v)", e.RetryAfter)
}

// ClientError represents a 4xx client error from a webhook service.
type ClientError struct {
	StatusCode int
	Message    string
}

func (e *ClientError) Error() string {
	return e.Message
}

// ServerError represents a 5xx server error from a webhook service.
type ServerError struct {
	StatusCode int
	Message    string
}

func (e *ServerError) Error() string {
	return e.Message
}

// is429Error checks if the error is a rate limit error and extracts retry_after.
func is429Error(err error) (*RateLimitError, bool) {
	var rateLimitErr *RateLimitError
	if errors.As(err, &rateLimitErr) {
		return rateLimitErr, true
	}
	return nil, false
}

// isRetryableError checks if the error is worth retrying (5xx server errors, network errors).
// Client errors (4xx) are not retryable except for rate limits (429).
func isRetryableError(err error) bool {
	// Server errors are retryable
	var serverErr *ServerError
	if errors.As(err, &serverErr) {
		return true
	}

	// Client errors are NOT retryable
	var clientErr *ClientError
	if errors.As(err, &clientErr) {
		return false
	}

	// Rate limit errors are handled separately
	var rateLimitErr *RateLimitError
	if errors.As(err, &rateLimitErr) {
		return false // Handled by is429Error
	}

	// Network errors, context errors, etc. are retryable
	return true
}

// truncateSummary truncates text to maxLength characters.
// If truncated, appends suffix to indicate continuation.
func truncateSummary(text string, maxLength int, suffix string) string {
	if len(text) <= maxLength {
		return text
	}

	// Reserve space for suffix
	truncateAt := maxLength - len(suffix)
	if truncateAt < 0 {
		truncateAt = 0
	}

	return text[:truncateAt] + suffix
}
