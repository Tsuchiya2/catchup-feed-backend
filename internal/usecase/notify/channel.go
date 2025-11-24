// Package notify provides use cases for dispatching notifications across multiple channels.
// It implements business logic for sending notifications about new articles to various
// delivery channels (Discord, Slack, Email, etc.) with features like circuit breakers,
// rate limiting, and observability.
package notify

import (
	"context"

	"catchup-feed/internal/domain/entity"
)

// Channel represents a notification delivery channel (Discord, Slack, Email, etc.).
// Each channel implementation handles its own rate limiting, retries, and
// error handling.
//
// Retry Policy Contract:
//   - Transient failures (5xx, network errors): Retry with exponential backoff (max 2 attempts)
//   - Rate limits (429): Sleep for retry_after duration, then retry (max 3 attempts)
//   - Client errors (4xx except 429): No retry
//   - Context timeout: No retry
//
// Thread Safety:
//   - All methods must be safe for concurrent use by multiple goroutines
//
// Context Handling:
//   - Implementations must respect context cancellation and timeout
//   - request_id should be extracted from context for logging
type Channel interface {
	// Name returns the human-readable name of the channel (e.g., "Discord", "Slack").
	// This is used for logging, metrics, and health check endpoints.
	//
	// Returns:
	//   - string: Channel identifier (lowercase, alphanumeric)
	Name() string

	// IsEnabled returns true if this channel is enabled via configuration.
	// Disabled channels will be skipped during notification dispatching.
	//
	// Returns:
	//   - bool: true if channel is enabled and should receive notifications
	IsEnabled() bool

	// Send sends a notification about a new article to this channel.
	//
	// Implementations must:
	//   - Respect context cancellation/timeout
	//   - Apply rate limiting
	//   - Retry transient failures according to retry policy
	//   - Log all attempts with request_id from context
	//   - Sanitize sensitive data (webhook URLs, API keys) in error messages
	//
	// Parameters:
	//   - ctx: Context with timeout and request_id (accessible via ctx.Value("request_id"))
	//   - article: The article to notify about (must not be nil)
	//   - source: The feed source of the article (must not be nil)
	//
	// Returns:
	//   - error: Non-nil if notification failed after all retries
	//     - ErrChannelDisabled: If Send() called on disabled channel
	//     - ErrInvalidArticle: If article is nil or missing required fields
	//     - ErrInvalidSource: If source is nil
	//     - Network/API errors: Wrapped with context
	Send(ctx context.Context, article *entity.Article, source *entity.Source) error
}
