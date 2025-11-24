package notify

import "errors"

// Sentinel errors for notify use case operations.
var (
	// ErrChannelDisabled indicates that Send() was called on a disabled channel.
	// This error is returned when attempting to send a notification through a channel
	// that is not enabled in the configuration.
	ErrChannelDisabled = errors.New("channel is disabled")

	// ErrInvalidArticle indicates that the article data is invalid or missing required fields.
	// This error is returned when:
	//   - article is nil
	//   - article.Title is empty
	//   - article.URL is empty
	ErrInvalidArticle = errors.New("invalid article data")

	// ErrInvalidSource indicates that the source data is invalid or nil.
	// This error is returned when:
	//   - source is nil
	//   - source.Name is empty
	ErrInvalidSource = errors.New("invalid source data")

	// ErrNotificationDropped indicates that a notification was dropped due to
	// goroutine pool saturation or timeout waiting for a worker slot.
	// This is a non-critical error used for observability.
	ErrNotificationDropped = errors.New("notification dropped due to pool saturation")

	// ErrCircuitBreakerOpen indicates that the circuit breaker is open for this channel
	// and notifications are being rejected to prevent continuous failures.
	// The circuit breaker will automatically close after the timeout period.
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open for this channel")
)
