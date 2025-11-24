package notify

import (
	"context"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/infra/notifier"
)

// DiscordChannel implements the Channel interface for Discord notifications.
// It wraps the existing DiscordNotifier from the infrastructure layer to provide
// the Channel abstraction for the notification use case.
//
// This adapter pattern allows Discord notifications to integrate seamlessly with
// the multi-channel notification system while reusing the existing, battle-tested
// Discord webhook implementation.
type DiscordChannel struct {
	notifier notifier.Notifier
	enabled  bool
}

// NewDiscordChannel creates a new Discord channel with the specified configuration.
//
// If Discord notifications are disabled (config.Enabled = false), a NoOpNotifier
// is used instead to avoid null checks and ensure the Channel interface contract
// is always satisfied.
//
// Parameters:
//   - config: Discord configuration (webhook URL, timeout, enabled state)
//
// Returns:
//   - *DiscordChannel: Configured Discord channel instance
func NewDiscordChannel(config notifier.DiscordConfig) *DiscordChannel {
	var n notifier.Notifier
	if config.Enabled {
		n = notifier.NewDiscordNotifier(config)
	} else {
		n = notifier.NewNoOpNotifier()
	}

	return &DiscordChannel{
		notifier: n,
		enabled:  config.Enabled,
	}
}

// Name returns the channel identifier "discord".
// This is used for logging, metrics labels, and health check endpoints.
func (c *DiscordChannel) Name() string {
	return "discord"
}

// IsEnabled returns whether Discord notifications are enabled via configuration.
// Disabled channels are skipped during notification dispatching.
func (c *DiscordChannel) IsEnabled() bool {
	return c.enabled
}

// Send sends a notification about a new article to Discord.
//
// This method performs input validation and delegates to the underlying
// DiscordNotifier for the actual webhook request. The notifier handles:
//   - Rate limiting (0.5 req/s with burst of 3)
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
//   - Other errors: Network errors, rate limit errors, Discord API errors
func (c *DiscordChannel) Send(ctx context.Context, article *entity.Article, source *entity.Source) error {
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
