// Package notifier provides abstraction for sending notifications about articles.
// It defines the Notifier interface which allows different notification mechanisms
// (Discord, Slack, email, etc.) to be used interchangeably through dependency injection.
//
// The package includes implementations for Discord webhooks and a no-op notifier
// for when notifications are disabled.
package notifier

import (
	"context"

	"catchup-feed/internal/domain/entity"
)

// Notifier is an interface for sending article notifications.
// Implementations should handle rate limiting, retries, and error logging internally.
type Notifier interface {
	// NotifyArticle sends a notification about a newly fetched and summarized article.
	// The notification should include article metadata (title, URL, summary) and source information.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - article: The article to notify about (must not be nil)
	//   - source: The feed source of the article (must not be nil)
	//
	// Returns:
	//   - error: Non-nil if the notification failed after all retry attempts
	//
	// Implementations should:
	//   - Generate a unique request ID for tracing
	//   - Apply rate limiting to prevent API abuse
	//   - Retry transient failures with exponential backoff
	//   - Log all attempts with the request ID for debugging
	//   - Respect context cancellation
	NotifyArticle(ctx context.Context, article *entity.Article, source *entity.Source) error
}
