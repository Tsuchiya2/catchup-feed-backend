package summarizer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	openai "github.com/sashabaranov/go-openai"
	"github.com/sony/gobreaker"

	"catchup-feed/internal/resilience/circuitbreaker"
	"catchup-feed/internal/resilience/retry"
	"catchup-feed/internal/utils/text"
)

// OpenAIConfig holds configuration parameters for the OpenAI summarizer.
// Configuration is loaded from environment variables with fallback to defaults.
type OpenAIConfig struct {
	// CharacterLimit is the maximum number of characters allowed in a summary.
	// Loaded from SUMMARIZER_CHAR_LIMIT environment variable.
	// Valid range: 100-5000 characters. Default: 900.
	CharacterLimit int

	// Language is the target language for summaries.
	// Currently hardcoded to "japanese". Future enhancement: support multiple languages.
	Language string

	// Model is the OpenAI API model identifier to use for summarization.
	Model string

	// MaxTokens is the maximum number of tokens for the API response.
	MaxTokens int

	// Timeout is the maximum duration for a single summarization API call.
	Timeout time.Duration
}

// GetCharacterLimit implements SummarizerConfig interface.
// Returns the configured maximum character limit for summaries.
func (c *OpenAIConfig) GetCharacterLimit() int {
	return c.CharacterLimit
}

// Validate implements SummarizerConfig interface.
// Validates the configuration and returns an error if invalid.
func (c *OpenAIConfig) Validate() error {
	// Validate character limit using shared helper
	if err := ValidateCharacterLimit(c.CharacterLimit); err != nil {
		return fmt.Errorf("invalid character limit: %w", err)
	}

	// Validate other fields
	if c.Language == "" {
		return fmt.Errorf("language cannot be empty")
	}

	if c.Model == "" {
		return fmt.Errorf("model cannot be empty")
	}

	if c.MaxTokens <= 0 {
		return fmt.Errorf("max tokens must be positive, got %d", c.MaxTokens)
	}

	if c.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive, got %v", c.Timeout)
	}

	return nil
}

// LoadOpenAIConfig loads configuration from environment variables.
// It performs validation on the character limit to ensure it's within a valid range (100-5000).
// Returns an error if the configuration is invalid.
//
// Environment variables:
//   - SUMMARIZER_CHAR_LIMIT: Character limit (default: 900, range: 100-5000)
//
// Returns:
//   - OpenAIConfig with validated settings
//   - error if validation fails (fail-closed behavior)
func LoadOpenAIConfig() (*OpenAIConfig, error) {
	const (
		defaultCharLimit = 900
	)

	charLimit := defaultCharLimit

	if envLimit := os.Getenv("SUMMARIZER_CHAR_LIMIT"); envLimit != "" {
		parsed, err := strconv.Atoi(envLimit)
		if err != nil {
			return nil, fmt.Errorf("invalid SUMMARIZER_CHAR_LIMIT format: %s: %w", envLimit, err)
		}

		// Validate character limit using shared helper
		if err := ValidateCharacterLimit(parsed); err != nil {
			return nil, fmt.Errorf("SUMMARIZER_CHAR_LIMIT out of valid range: %w", err)
		}

		charLimit = parsed
	}

	config := &OpenAIConfig{
		CharacterLimit: charLimit,
		Language:       "japanese",
		Model:          "gpt-3.5-turbo",
		MaxTokens:      1024,
		Timeout:        60 * time.Second,
	}

	// Validate the entire configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid OpenAI configuration: %w", err)
	}

	return config, nil
}

// OpenAI implements the Summarizer interface using OpenAI's GPT API.
// It includes circuit breaker and retry logic for improved reliability,
// and supports configurable character limits with comprehensive observability.
type OpenAI struct {
	client          *openai.Client
	circuitBreaker  *circuitbreaker.CircuitBreaker
	retryConfig     retry.Config
	config          SummarizerConfig
	metricsRecorder SummaryMetricsRecorder
}

// NewOpenAI creates a new OpenAI summarizer with the given API key.
// It automatically configures circuit breaker, retry logic, character limit configuration,
// and metrics recording.
func NewOpenAI(apiKey string, config SummarizerConfig) *OpenAI {
	slog.Info("Initialized OpenAI summarizer with configuration",
		slog.Int("character_limit", config.GetCharacterLimit()))

	return &OpenAI{
		client:          openai.NewClient(apiKey),
		circuitBreaker:  circuitbreaker.New(circuitbreaker.OpenAIAPIConfig()),
		retryConfig:     retry.AIAPIConfig(),
		config:          config,
		metricsRecorder: NewPrometheusSummaryMetrics(),
	}
}

// Summarize generates a summary of the given text using OpenAI's GPT API.
// It uses circuit breaker and retry logic for improved reliability.
// Returns the summarized text in Japanese.
func (o *OpenAI) Summarize(ctx context.Context, text string) (string, error) {
	// Set individual timeout (60 seconds)
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var result string

	// Wrap with retry logic
	retryErr := retry.WithBackoff(ctx, o.retryConfig, func() error {
		// Execute through circuit breaker
		cbResult, err := o.circuitBreaker.Execute(func() (interface{}, error) {
			return o.doSummarize(ctx, text)
		})

		// Handle circuit breaker open state
		if err != nil {
			if errors.Is(err, gobreaker.ErrOpenState) {
				slog.Warn("openai api circuit breaker open, request rejected",
					slog.String("service", "openai-api"),
					slog.String("state", o.circuitBreaker.State().String()))
				return fmt.Errorf("openai api unavailable: circuit breaker open")
			}
			return err
		}

		result = cbResult.(string)
		return nil
	})

	if retryErr != nil {
		return "", fmt.Errorf("openai summarize failed after retries: %w", retryErr)
	}

	return result, nil
}

// buildPrompt constructs the summarization prompt using configured parameters.
// It instructs the AI to generate a summary in Japanese within the character limit.
//
// Example output:
//
//	"以下のテキストを日本語で900文字以内で要約してください：\n{text}"
func (o *OpenAI) buildPrompt(text string) string {
	return fmt.Sprintf("以下のテキストを日本語で%d文字以内で要約してください：\n%s",
		o.config.GetCharacterLimit(), text)
}

// doSummarize performs the actual API call without retry or circuit breaker.
// It includes comprehensive structured logging and metrics recording for observability.
func (o *OpenAI) doSummarize(ctx context.Context, inputText string) (string, error) {
	// Truncate text to avoid token limit (gpt-3.5-turbo max: 16,385 tokens)
	// Safe limit: ~10,000 chars (~2,500 tokens) to account for system prompt and response
	const maxChars = 10000
	truncatedText := inputText
	if len(inputText) > maxChars {
		truncatedText = inputText[:maxChars] + "...\n(内容が長いため切り詰めました)"
		slog.Warn("text truncated for openai api",
			slog.Int("original_length", len(inputText)),
			slog.Int("truncated_length", len(truncatedText)))
	}

	// Build prompt with configured character limit
	prompt := o.buildPrompt(truncatedText)
	inputLength := text.CountRunes(truncatedText)

	// Log summarization start
	slog.InfoContext(ctx, "Starting summarization",
		slog.Int("input_length", inputLength),
		slog.Int("character_limit", o.config.GetCharacterLimit()))

	// Record start time for duration measurement
	start := time.Now()

	// Call OpenAI API
	resp, err := o.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: "gpt-3.5-turbo",
		Messages: []openai.ChatCompletionMessage{{
			Role:    "system",
			Content: prompt,
		}},
	})

	duration := time.Since(start)

	if err != nil {
		slog.ErrorContext(ctx, "Summarization failed",
			slog.Duration("duration", duration),
			slog.String("error", err.Error()))
		return "", fmt.Errorf("openai api error: %w", err)
	}

	// Validate response structure (safety check to prevent panic on array access)
	if len(resp.Choices) == 0 {
		slog.ErrorContext(ctx, "OpenAI API returned empty response",
			slog.Duration("duration", duration))
		return "", fmt.Errorf("openai api returned empty response")
	}

	// Extract summary from response
	summary := resp.Choices[0].Message.Content
	summaryLength := text.CountRunes(summary)
	withinLimit := summaryLength <= o.config.GetCharacterLimit()

	// Log summary result
	slog.InfoContext(ctx, "Summarization completed",
		slog.Int("summary_length", summaryLength),
		slog.Int("character_limit", o.config.GetCharacterLimit()),
		slog.Bool("within_limit", withinLimit),
		slog.Duration("duration", duration))

	// Log warning if limit exceeded (soft limit, not hard rejection)
	if !withinLimit {
		excess := summaryLength - o.config.GetCharacterLimit()
		slog.WarnContext(ctx, "Summary exceeds character limit",
			slog.Int("summary_length", summaryLength),
			slog.Int("limit", o.config.GetCharacterLimit()),
			slog.Int("excess", excess))
	}

	// Record metrics
	o.metricsRecorder.RecordLength(summaryLength)
	o.metricsRecorder.RecordDuration(duration)
	o.metricsRecorder.RecordCompliance(withinLimit)
	if !withinLimit {
		o.metricsRecorder.RecordLimitExceeded()
	}

	return summary, nil
}
