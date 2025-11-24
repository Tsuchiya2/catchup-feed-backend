// Package summarizer provides AI-powered text summarization implementations.
// It includes adapters for Claude (Anthropic) and OpenAI APIs with reliability patterns.
// This package supports configurable character limits for summaries with comprehensive
// observability through structured logging and Prometheus metrics.
package summarizer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/uuid"
	"github.com/sony/gobreaker"

	"catchup-feed/internal/resilience/circuitbreaker"
	"catchup-feed/internal/resilience/retry"
	"catchup-feed/internal/utils/text"
)

// ClaudeConfig holds configuration parameters for the Claude summarizer.
// Configuration is loaded from environment variables with fallback to defaults.
type ClaudeConfig struct {
	// CharacterLimit is the maximum number of characters allowed in a summary.
	// Loaded from SUMMARIZER_CHAR_LIMIT environment variable.
	// Valid range: 100-5000 characters. Default: 900.
	CharacterLimit int

	// Language is the target language for summaries.
	// Currently hardcoded to "japanese". Future enhancement: support multiple languages.
	Language string

	// Model is the Claude API model identifier to use for summarization.
	Model string

	// MaxTokens is the maximum number of tokens for the API response.
	MaxTokens int

	// Timeout is the maximum duration for a single summarization API call.
	Timeout time.Duration
}

// LoadClaudeConfig loads configuration from environment variables.
// It performs validation on the character limit to ensure it's within a valid range (100-5000).
// Invalid values fall back to the default (900) with a warning log.
//
// Environment variables:
//   - SUMMARIZER_CHAR_LIMIT: Character limit (default: 900, range: 100-5000)
//
// Returns ClaudeConfig with validated settings.
func LoadClaudeConfig() ClaudeConfig {
	const (
		defaultCharLimit = 900
		minCharLimit     = 100
		maxCharLimit     = 5000
	)

	charLimit := defaultCharLimit

	if envLimit := os.Getenv("SUMMARIZER_CHAR_LIMIT"); envLimit != "" {
		parsed, err := strconv.Atoi(envLimit)
		if err != nil {
			slog.Warn("Invalid SUMMARIZER_CHAR_LIMIT format, using default",
				slog.String("value", envLimit),
				slog.Int("default", defaultCharLimit),
				slog.String("error", err.Error()))
		} else if parsed < minCharLimit || parsed > maxCharLimit {
			slog.Warn("SUMMARIZER_CHAR_LIMIT out of valid range, using default",
				slog.Int("value", parsed),
				slog.Int("min", minCharLimit),
				slog.Int("max", maxCharLimit),
				slog.Int("default", defaultCharLimit))
		} else {
			charLimit = parsed
		}
	}

	return ClaudeConfig{
		CharacterLimit: charLimit,
		Language:       "japanese",
		Model:          string(anthropic.ModelClaudeSonnet4_5_20250929),
		MaxTokens:      1024,
		Timeout:        60 * time.Second,
	}
}

// Claude implements the Summarizer interface using Anthropic's Claude API.
// It includes circuit breaker and retry logic for improved reliability,
// and supports configurable character limits with comprehensive observability.
type Claude struct {
	client          anthropic.Client
	circuitBreaker  *circuitbreaker.CircuitBreaker
	retryConfig     retry.Config
	config          ClaudeConfig
	metricsRecorder SummaryMetricsRecorder
}

// NewClaude creates a new Claude summarizer with the given API key.
// It automatically configures circuit breaker, retry logic, character limit configuration,
// and metrics recording.
func NewClaude(apiKey string) *Claude {
	config := LoadClaudeConfig()

	slog.Info("Initialized Claude summarizer with configuration",
		slog.Int("character_limit", config.CharacterLimit),
		slog.String("language", config.Language),
		slog.String("model", config.Model))

	return &Claude{
		client:          anthropic.NewClient(option.WithAPIKey(apiKey)),
		circuitBreaker:  circuitbreaker.New(circuitbreaker.ClaudeAPIConfig()),
		retryConfig:     retry.AIAPIConfig(),
		config:          config,
		metricsRecorder: NewPrometheusSummaryMetrics(),
	}
}

// Summarize generates a summary of the given text using Claude AI.
// It uses circuit breaker and retry logic for improved reliability.
// Returns the summarized text in Japanese.
func (c *Claude) Summarize(ctx context.Context, text string) (string, error) {
	// Set individual timeout (60 seconds)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var result string

	// Wrap with retry logic
	retryErr := retry.WithBackoff(ctx, c.retryConfig, func() error {
		// Execute through circuit breaker
		cbResult, err := c.circuitBreaker.Execute(func() (interface{}, error) {
			return c.doSummarize(ctx, text)
		})

		// Handle circuit breaker open state
		if err != nil {
			if errors.Is(err, gobreaker.ErrOpenState) {
				slog.Warn("claude api circuit breaker open, request rejected",
					slog.String("service", "claude-api"),
					slog.String("state", c.circuitBreaker.State().String()))
				return fmt.Errorf("claude api unavailable: circuit breaker open")
			}
			return err
		}

		result = cbResult.(string)
		return nil
	})

	if retryErr != nil {
		return "", fmt.Errorf("claude summarize failed after retries: %w", retryErr)
	}

	return result, nil
}

// buildPrompt constructs the summarization prompt using configured parameters.
// It instructs the AI to generate a summary in the target language within the character limit.
//
// Example output:
//
//	"以下のテキストを日本語で900文字以内で要約してください：\n{text}"
func (c *Claude) buildPrompt(text string) string {
	return fmt.Sprintf("以下のテキストを%sで%d文字以内で要約してください：\n%s",
		c.config.Language, c.config.CharacterLimit, text)
}

// doSummarize performs the actual API call without retry or circuit breaker.
// It includes comprehensive structured logging and metrics recording for observability.
func (c *Claude) doSummarize(ctx context.Context, inputText string) (string, error) {
	// Generate unique request ID for tracing
	requestID := uuid.New().String()

	// Truncate text to avoid token limit (safety measure, even though Claude supports 200k tokens)
	// Safe limit: ~10,000 chars to maintain consistency with OpenAI implementation
	const maxChars = 10000
	truncatedText := inputText
	if len(inputText) > maxChars {
		truncatedText = inputText[:maxChars] + "...\n(内容が長いため切り詰めました)"
		slog.Warn("text truncated for claude api",
			slog.String("request_id", requestID),
			slog.Int("original_length", len(inputText)),
			slog.Int("truncated_length", len(truncatedText)))
	}

	// Build prompt with configured character limit
	prompt := c.buildPrompt(truncatedText)
	inputLength := text.CountRunes(truncatedText)

	// Log summarization start
	slog.InfoContext(ctx, "Starting summarization",
		slog.String("request_id", requestID),
		slog.Int("input_length", inputLength),
		slog.Int("character_limit", c.config.CharacterLimit))

	// Record start time for duration measurement
	start := time.Now()

	// Call Claude API
	message, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.config.Model),
		MaxTokens: int64(c.config.MaxTokens),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewTextBlock(prompt),
			),
		},
	})

	duration := time.Since(start)

	if err != nil {
		slog.ErrorContext(ctx, "Summarization failed",
			slog.String("request_id", requestID),
			slog.Duration("duration", duration),
			slog.String("error", err.Error()))
		return "", fmt.Errorf("claude api error: %w", err)
	}

	// Validate response structure
	if len(message.Content) == 0 {
		slog.ErrorContext(ctx, "Claude API returned empty response",
			slog.String("request_id", requestID),
			slog.Duration("duration", duration))
		return "", fmt.Errorf("claude api returned empty response")
	}

	// Extract text from response
	textBlock, ok := message.Content[0].AsAny().(anthropic.TextBlock)
	if !ok {
		slog.ErrorContext(ctx, "Claude API returned unexpected response type",
			slog.String("request_id", requestID),
			slog.Duration("duration", duration))
		return "", fmt.Errorf("claude api returned unexpected response type")
	}

	summary := textBlock.Text
	summaryLength := text.CountRunes(summary)
	withinLimit := summaryLength <= c.config.CharacterLimit

	// Log summary result
	slog.InfoContext(ctx, "Summarization completed",
		slog.String("request_id", requestID),
		slog.Int("summary_length", summaryLength),
		slog.Int("character_limit", c.config.CharacterLimit),
		slog.Bool("within_limit", withinLimit),
		slog.Duration("duration", duration))

	// Log warning if limit exceeded (should be rare)
	if !withinLimit {
		excess := summaryLength - c.config.CharacterLimit
		slog.WarnContext(ctx, "Summary exceeds character limit",
			slog.String("request_id", requestID),
			slog.Int("summary_length", summaryLength),
			slog.Int("limit", c.config.CharacterLimit),
			slog.Int("excess", excess))
	}

	// Record metrics
	c.metricsRecorder.RecordLength(summaryLength)
	c.metricsRecorder.RecordDuration(duration)
	c.metricsRecorder.RecordCompliance(withinLimit)
	if !withinLimit {
		c.metricsRecorder.RecordLimitExceeded()
	}

	return summary, nil
}
