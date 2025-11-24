package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"catchup-feed/internal/domain/entity"

	"github.com/google/uuid"
)

// DiscordConfig contains configuration for Discord webhook notifications.
type DiscordConfig struct {
	// Enabled indicates whether Discord notifications are enabled
	Enabled bool

	// WebhookURL is the Discord webhook URL (includes authentication token)
	WebhookURL string

	// Timeout is the HTTP request timeout for Discord API calls
	Timeout time.Duration
}

// DiscordNotifier sends article notifications to Discord via webhook.
type DiscordNotifier struct {
	config      DiscordConfig
	httpClient  *http.Client
	rateLimiter *RateLimiter
}

// NewDiscordNotifier creates a new DiscordNotifier with the specified configuration.
//
// The notifier is initialized with:
//   - HTTP client with configured timeout
//   - Rate limiter set to 0.5 requests/second with burst of 3
//     (Discord Webhook limit: 30 requests per minute = 0.5 req/s)
//
// Parameters:
//   - config: Discord configuration including webhook URL and timeout
//
// Returns:
//   - *DiscordNotifier: Configured Discord notifier instance
func NewDiscordNotifier(config DiscordConfig) *DiscordNotifier {
	return &DiscordNotifier{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		rateLimiter: NewRateLimiter(0.5, 3), // 0.5 req/s (30 req/min), burst of 3
	}
}

// DiscordWebhookPayload represents the JSON payload sent to Discord webhook.
type DiscordWebhookPayload struct {
	Embeds []DiscordEmbed `json:"embeds"`
}

// DiscordEmbed represents a Discord embed message.
type DiscordEmbed struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	URL         string             `json:"url"`
	Color       int                `json:"color"`
	Footer      DiscordEmbedFooter `json:"footer"`
	Timestamp   string             `json:"timestamp"`
}

// DiscordEmbedFooter represents the footer of a Discord embed.
type DiscordEmbedFooter struct {
	Text string `json:"text"`
}

// DiscordErrorResponse represents the error response from Discord API.
type DiscordErrorResponse struct {
	Message    string  `json:"message"`
	Code       int     `json:"code"`
	RetryAfter float64 `json:"retry_after"` // In seconds
}

const (
	// Discord limits
	maxTitleLength       = 256
	maxDescriptionLength = 4096
	truncationSuffix     = "..."

	// Discord blue color (#5865F2)
	discordBlueColor = 5793266
)

// buildEmbedPayload creates a Discord webhook payload from an article and source.
//
// The payload includes:
//   - Title: Article title (truncated to 256 chars if needed)
//   - Description: Article summary (truncated to 4090 chars + "..." if needed)
//   - URL: Article URL
//   - Color: Discord blue (#5865F2)
//   - Footer: Source name
//   - Timestamp: Article publication time in RFC3339 format
func (d *DiscordNotifier) buildEmbedPayload(article *entity.Article, source *entity.Source) DiscordWebhookPayload {
	title := article.Title
	if len(title) > maxTitleLength {
		title = title[:maxTitleLength]
	}

	description := truncateSummary(article.Summary, maxDescriptionLength, truncationSuffix)

	embed := DiscordEmbed{
		Title:       title,
		Description: description,
		URL:         article.URL,
		Color:       discordBlueColor,
		Footer: DiscordEmbedFooter{
			Text: source.Name,
		},
		Timestamp: article.PublishedAt.Format(time.RFC3339),
	}

	return DiscordWebhookPayload{
		Embeds: []DiscordEmbed{embed},
	}
}

// sendWebhookRequest sends a Discord webhook request with the given article and source.
//
// Returns:
//   - nil: Request succeeded (2xx status)
//   - error: Request failed (non-2xx status or network error)
//
// Error types:
//   - 429: Rate limit error (retryable, contains retry_after duration)
//   - 4xx (non-429): Client error (non-retryable)
//   - 5xx: Server error (retryable)
//   - Network error: Connection/timeout error (retryable)
func (d *DiscordNotifier) sendWebhookRequest(ctx context.Context, article *entity.Article, source *entity.Source) error {
	payload := d.buildEmbedPayload(article, source)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.config.WebhookURL, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body for error messages
	body, _ := io.ReadAll(resp.Body)

	// Success
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// Rate limit error (429)
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := extractRetryAfter(resp, body)
		return &RateLimitError{
			Message:    "Discord rate limit exceeded",
			RetryAfter: retryAfter,
		}
	}

	// Client error (4xx, non-retryable)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return &ClientError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("Discord API client error: %s", string(body)),
		}
	}

	// Server error (5xx, retryable)
	if resp.StatusCode >= 500 {
		return &ServerError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("Discord API server error: %s", string(body)),
		}
	}

	return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
}

// extractRetryAfter extracts retry_after duration from Discord error response.
// It tries to parse from JSON body first, then falls back to Retry-After header.
//
// Returns:
//   - time.Duration: Retry after duration (default 5s if not found)
func extractRetryAfter(resp *http.Response, body []byte) time.Duration {
	// Try to parse from JSON response
	var discordErr DiscordErrorResponse
	if err := json.Unmarshal(body, &discordErr); err == nil && discordErr.RetryAfter > 0 {
		return time.Duration(discordErr.RetryAfter * float64(time.Second))
	}

	// Fall back to Retry-After header (in seconds)
	if retryAfterHeader := resp.Header.Get("Retry-After"); retryAfterHeader != "" {
		if seconds, err := strconv.Atoi(retryAfterHeader); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	// Default retry after 5 seconds
	return 5 * time.Second
}

// sendWebhookRequestWithRetry sends a Discord webhook request with retry logic.
//
// Retry strategy:
//   - Max attempts: 2
//   - Base delay: 5 seconds
//   - 429 errors: Use retry_after from Discord response
//   - Server errors (5xx): Exponential backoff (5s, 10s)
//   - Client errors (4xx): No retry, fail immediately
//
// All attempts are logged with request_id for tracing.
func (d *DiscordNotifier) sendWebhookRequestWithRetry(ctx context.Context, article *entity.Article, source *entity.Source) error {
	const (
		maxAttempts = 2
		baseDelay   = 5 * time.Second
	)

	requestID, _ := ctx.Value(requestIDKey).(string)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := d.sendWebhookRequest(ctx, article, source)

		// Success
		if err == nil {
			slog.Info("Discord notification successful",
				slog.String("request_id", requestID),
				slog.Int64("article_id", article.ID),
				slog.String("url", article.URL),
				slog.Int("attempt", attempt))
			return nil
		}

		lastErr = err

		// Handle rate limit error (429)
		if rateLimitErr, ok := is429Error(err); ok {
			slog.Warn("Discord rate limit hit, backing off",
				slog.String("request_id", requestID),
				slog.Int64("article_id", article.ID),
				slog.Duration("retry_after", rateLimitErr.RetryAfter),
				slog.Int("attempt", attempt))

			// Sleep for retry_after duration
			select {
			case <-time.After(rateLimitErr.RetryAfter):
				continue
			case <-ctx.Done():
				return fmt.Errorf("context canceled during rate limit backoff: %w", ctx.Err())
			}
		}

		// Handle non-retryable errors (4xx client errors)
		if !isRetryableError(err) {
			slog.Error("Discord notification failed with non-retryable error",
				slog.String("request_id", requestID),
				slog.Int64("article_id", article.ID),
				slog.String("url", article.URL),
				slog.Any("error", err),
				slog.Int("attempt", attempt))
			return err
		}

		// Retry on retryable errors (5xx server errors, network errors)
		if attempt < maxAttempts {
			delay := baseDelay * time.Duration(attempt)
			slog.Warn("Discord API request failed, retrying",
				slog.String("request_id", requestID),
				slog.Int64("article_id", article.ID),
				slog.String("url", article.URL),
				slog.Any("error", err),
				slog.Int("attempt", attempt),
				slog.Duration("delay", delay))

			select {
			case <-time.After(delay):
				continue
			case <-ctx.Done():
				return fmt.Errorf("context canceled during retry backoff: %w", ctx.Err())
			}
		}
	}

	// All retries exhausted
	slog.Error("Discord notification failed after all retries",
		slog.String("request_id", requestID),
		slog.Int64("article_id", article.ID),
		slog.String("url", article.URL),
		slog.Any("error", lastErr),
		slog.Int("max_attempts", maxAttempts))

	return fmt.Errorf("discord notification failed after %d attempts: %w", maxAttempts, lastErr)
}

// NotifyArticle sends a Discord notification for a newly fetched article.
// This method implements the Notifier interface.
//
// It performs the following steps:
//  1. Generate unique request_id for tracing
//  2. Add request_id to context
//  3. Apply rate limiting to prevent API abuse
//  4. Send webhook request with retry logic
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - article: The article to notify about (must not be nil)
//   - source: The feed source of the article (must not be nil)
//
// Returns:
//   - error: Non-nil if notification failed after all retry attempts or rate limiting failed
func (d *DiscordNotifier) NotifyArticle(ctx context.Context, article *entity.Article, source *entity.Source) error {
	// Generate unique request ID for tracing
	requestID := uuid.New().String()
	ctx = context.WithValue(ctx, requestIDKey, requestID)

	slog.Info("Starting Discord notification",
		slog.String("request_id", requestID),
		slog.Int64("article_id", article.ID),
		slog.Int64("source_id", source.ID),
		slog.String("url", article.URL))

	// Apply rate limiting
	if err := d.rateLimiter.Allow(ctx); err != nil {
		slog.Error("Rate limiter error",
			slog.String("request_id", requestID),
			slog.Int64("article_id", article.ID),
			slog.Any("error", err))
		return fmt.Errorf("rate limiter error: %w", err)
	}

	// Send webhook request with retry logic
	return d.sendWebhookRequestWithRetry(ctx, article, source)
}
