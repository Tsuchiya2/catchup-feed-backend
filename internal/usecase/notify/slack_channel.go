package notify

import (
	"context"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/infra/notifier"
)

// SlackChannel implements the Channel interface for Slack notifications.
// It wraps the existing SlackNotifier from the infrastructure layer to provide
// the Channel abstraction for the notification use case.
//
// This adapter pattern allows Slack notifications to integrate seamlessly with
// the multi-channel notification system while reusing the existing, battle-tested
// Slack webhook implementation.
type SlackChannel struct {
	notifier notifier.Notifier
	enabled  bool
}

// NewSlackChannel creates a new Slack channel with the specified configuration.
//
// If Slack notifications are disabled (config.Enabled = false), a NoOpNotifier
// is used instead to avoid null checks and ensure the Channel interface contract
// is always satisfied.
//
// Parameters:
//   - config: Slack configuration (webhook URL, timeout, enabled state)
//
// Returns:
//   - *SlackChannel: Configured Slack channel instance
func NewSlackChannel(config notifier.SlackConfig) *SlackChannel {
	var n notifier.Notifier
	if config.Enabled {
		n = notifier.NewSlackNotifier(config)
	} else {
		n = notifier.NewNoOpNotifier()
	}

	return &SlackChannel{
		notifier: n,
		enabled:  config.Enabled,
	}
}

// Name returns the channel identifier "slack".
// This is used for logging, metrics labels, and health check endpoints.
func (c *SlackChannel) Name() string {
	return "slack"
}

// IsEnabled returns whether Slack notifications are enabled via configuration.
// Disabled channels are skipped during notification dispatching.
func (c *SlackChannel) IsEnabled() bool {
	return c.enabled
}

// Send sends a notification about a new article to Slack.
//
// This method performs input validation and delegates to the underlying
// SlackNotifier for the actual webhook request. The notifier handles:
//   - Rate limiting (1 req/s with burst of 1)
//   - Retry logic (max 2 attempts with exponential backoff)
//   - Context timeout and cancellation
//   - Request ID generation and logging
//
// Parameters:
//   - ctx: Context with timeout and optional request_id
//   - article: The article to notify about (must not be nil)
//   - source: The feed source of the article (must not be nil)
//
// Returns:
//   - nil: Notification sent successfully
//   - ErrChannelDisabled: If called on disabled channel
//   - ErrInvalidArticle: If article is nil
//   - ErrInvalidSource: If source is nil
//   - Other errors: Network errors, rate limit errors, Slack API errors
func (c *SlackChannel) Send(ctx context.Context, article *entity.Article, source *entity.Source) error {
	// Validate that channel is enabled
	if !c.enabled {
		return ErrChannelDisabled
	}

	// Validate article
	if article == nil {
		return ErrInvalidArticle
	}

	// Validate source
	if source == nil {
		return ErrInvalidSource
	}

	// Delegate to underlying notifier
	return c.notifier.NotifyArticle(ctx, article, source)
}
