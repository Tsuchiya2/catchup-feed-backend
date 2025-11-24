package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"catchup-feed/internal/domain/entity"

	"github.com/google/uuid"
)

// SlackConfig contains configuration for Slack webhook notifications.
type SlackConfig struct {
	// Enabled indicates whether Slack notifications are enabled
	Enabled bool

	// WebhookURL is the Slack Incoming Webhook URL (includes authentication token)
	WebhookURL string

	// Timeout is the HTTP request timeout for Slack API calls
	Timeout time.Duration
}

// SlackNotifier sends article notifications to Slack via Incoming Webhook.
type SlackNotifier struct {
	config      SlackConfig
	httpClient  *http.Client
	rateLimiter *RateLimiter
}

// NewSlackNotifier creates a new SlackNotifier with the specified configuration.
//
// The notifier is initialized with:
//   - HTTP client with configured timeout
//   - Rate limiter set to 1 request/second with burst of 1
//     (Slack Webhook limit: 1 message per second)
//
// Parameters:
//   - config: Slack configuration including webhook URL and timeout
//
// Returns:
//   - *SlackNotifier: Configured Slack notifier instance
func NewSlackNotifier(config SlackConfig) *SlackNotifier {
	return &SlackNotifier{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		rateLimiter: NewRateLimiter(1.0, 1), // 1 req/s, burst of 1
	}
}

// SlackWebhookPayload represents the JSON payload sent to Slack webhook using Block Kit.
type SlackWebhookPayload struct {
	Text   string       `json:"text"`   // Fallback text (required)
	Blocks []SlackBlock `json:"blocks"` // Rich formatting blocks
}

// SlackBlock represents a Slack Block Kit block.
type SlackBlock struct {
	Type     string            `json:"type"`               // "section", "context", "divider"
	Text     *SlackTextObject  `json:"text,omitempty"`     // Text content (for section)
	Elements []SlackTextObject `json:"elements,omitempty"` // Elements (for context)
}

// SlackTextObject represents a text object in Slack Block Kit.
type SlackTextObject struct {
	Type string `json:"type"` // "mrkdwn" or "plain_text"
	Text string `json:"text"` // Actual text content
}

// SlackErrorResponse represents the error response from Slack API.
type SlackErrorResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

const (
	// Slack Block Kit limits
	maxSectionTextLength = 3000
	maxContextTextLength = 2000
	maxFallbackLength    = 150

	// Truncation suffix
	slackTruncationSuffix = "..."
)

// buildBlockKitPayload creates a Slack webhook payload from an article and source.
//
// The payload includes:
//   - Text: Fallback text for notifications (Article title + source)
//   - Section Block: Article title (bold, linked) + summary text
//   - Context Block: Source name + publication timestamp
//
// Summary is truncated to 3000 characters if needed to fit Block Kit limits.
func (s *SlackNotifier) buildBlockKitPayload(article *entity.Article, source *entity.Source) SlackWebhookPayload {
	// Build fallback text (used in notifications)
	fallbackText := fmt.Sprintf("%s - %s", article.Title, source.Name)
	if len(fallbackText) > maxFallbackLength {
		fallbackText = fallbackText[:maxFallbackLength-len(slackTruncationSuffix)] + slackTruncationSuffix
	}

	// Build section block text (title with link + summary)
	// Format: *<url|title>*\n\nsummary
	titleLink := fmt.Sprintf("*<%s|%s>*", article.URL, article.Title)
	sectionText := fmt.Sprintf("%s\n\n%s", titleLink, article.Summary)

	// Truncate section text if needed
	sectionText = truncateSummary(sectionText, maxSectionTextLength, slackTruncationSuffix)

	// Build context block text (source + timestamp)
	contextText := fmt.Sprintf("%s • %s", source.Name, article.PublishedAt.Format(time.RFC3339))

	// Create section block
	sectionBlock := SlackBlock{
		Type: "section",
		Text: &SlackTextObject{
			Type: "mrkdwn",
			Text: sectionText,
		},
	}

	// Create context block
	contextBlock := SlackBlock{
		Type: "context",
		Elements: []SlackTextObject{
			{
				Type: "mrkdwn",
				Text: contextText,
			},
		},
	}

	return SlackWebhookPayload{
		Text:   fallbackText,
		Blocks: []SlackBlock{sectionBlock, contextBlock},
	}
}

// sendWebhookRequest sends a Slack webhook request with the given article and source.
//
// Returns:
//   - nil: Request succeeded (200 OK with "ok" response)
//   - error: Request failed (non-2xx status or network error)
//
// Error types:
//   - 429: Rate limit error (retryable, contains retry_after duration)
//   - 4xx (non-429): Client error (non-retryable)
//   - 5xx: Server error (retryable)
//   - Network error: Connection/timeout error (retryable)
func (s *SlackNotifier) sendWebhookRequest(ctx context.Context, article *entity.Article, source *entity.Source) error {
	payload := s.buildBlockKitPayload(article, source)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.WebhookURL, bytes.NewReader(jsonData))
	if err != nil {
		return fmt.Errorf("create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body for error messages
	body, _ := io.ReadAll(resp.Body)

	// Success (Slack returns "ok" as plain text on success)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// Rate limit error (429)
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfter := extractRetryAfter(resp, body)
		return &RateLimitError{
			Message:    "Slack rate limit exceeded",
			RetryAfter: retryAfter,
		}
	}

	// Client error (4xx, non-retryable)
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return &ClientError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("Slack API client error: %s", string(body)),
		}
	}

	// Server error (5xx, retryable)
	if resp.StatusCode >= 500 {
		return &ServerError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("Slack API server error: %s", string(body)),
		}
	}

	return fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
}

// sendWebhookRequestWithRetry sends a Slack webhook request with retry logic.
//
// Retry strategy:
//   - Max attempts: 2
//   - Base delay: 5 seconds
//   - 429 errors: Use retry_after from Slack response (or Retry-After header)
//   - Server errors (5xx): Exponential backoff (5s, 10s)
//   - Client errors (4xx): No retry, fail immediately
//
// All attempts are logged with request_id for tracing.
func (s *SlackNotifier) sendWebhookRequestWithRetry(ctx context.Context, article *entity.Article, source *entity.Source) error {
	const (
		maxAttempts = 2
		baseDelay   = 5 * time.Second
	)

	requestID, _ := ctx.Value(requestIDKey).(string)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := s.sendWebhookRequest(ctx, article, source)

		// Success
		if err == nil {
			slog.Info("Slack notification successful",
				slog.String("request_id", requestID),
				slog.Int64("article_id", article.ID),
				slog.String("url", article.URL),
				slog.Int("attempt", attempt))
			return nil
		}

		lastErr = err

		// Handle rate limit error (429)
		if rateLimitErr, ok := is429Error(err); ok {
			slog.Warn("Slack rate limit hit, backing off",
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
			slog.Error("Slack notification failed with non-retryable error",
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
			slog.Warn("Slack API request failed, retrying",
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
	slog.Error("Slack notification failed after all retries",
		slog.String("request_id", requestID),
		slog.Int64("article_id", article.ID),
		slog.String("url", article.URL),
		slog.Any("error", lastErr),
		slog.Int("max_attempts", maxAttempts))

	return fmt.Errorf("slack notification failed after %d attempts: %w", maxAttempts, lastErr)
}

// NotifyArticle sends a Slack notification for a newly fetched article.
// This method implements the Notifier interface.
//
// It performs the following steps:
//  1. Generate unique request_id for tracing
//  2. Add request_id to context
//  3. Apply rate limiting to prevent API abuse (1 req/s, burst of 1)
//  4. Send webhook request with retry logic
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - article: The article to notify about (must not be nil)
//   - source: The feed source of the article (must not be nil)
//
// Returns:
//   - error: Non-nil if notification failed after all retry attempts or rate limiting failed
func (s *SlackNotifier) NotifyArticle(ctx context.Context, article *entity.Article, source *entity.Source) error {
	// Generate unique request ID for tracing
	requestID := uuid.New().String()
	ctx = context.WithValue(ctx, requestIDKey, requestID)

	slog.Info("Starting Slack notification",
		slog.String("request_id", requestID),
		slog.Int64("article_id", article.ID),
		slog.Int64("source_id", source.ID),
		slog.String("url", article.URL))

	// Apply rate limiting
	if err := s.rateLimiter.Allow(ctx); err != nil {
		slog.Error("Rate limiter error",
			slog.String("request_id", requestID),
			slog.Int64("article_id", article.ID),
			slog.Any("error", err))
		return fmt.Errorf("rate limiter error: %w", err)
	}

	// Send webhook request with retry logic
	return s.sendWebhookRequestWithRetry(ctx, article, source)
}
