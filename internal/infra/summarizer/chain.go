package summarizer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"catchup-feed/internal/utils/text"
)

// ErrNoProviders is returned by NewChain / NewChainFromEnv when no provider
// is configured (no API keys and Ollama disabled).
var ErrNoProviders = errors.New("summarizer: no providers configured")

// Chain tries providers in order (Gemini -> Groq -> Ollama) and returns the
// first successful summary. Any provider error — 4xx/5xx, rate limit,
// timeout, connection refused — moves on to the next provider; only parent
// context cancellation aborts the chain. There is no retry and no circuit
// breaker (C-3): if every provider fails, the error is returned and the
// article is picked up again on the next cron run (§8 縮退許容).
//
// D-3: the radio script generator reuses this same chain.
type Chain struct {
	providers []Provider
	logger    *slog.Logger
}

// NewChain creates a fallback chain over the given providers, tried in order.
// Returns ErrNoProviders if the list is empty.
func NewChain(providers ...Provider) (*Chain, error) {
	if len(providers) == 0 {
		return nil, ErrNoProviders
	}
	return &Chain{providers: providers, logger: slog.Default()}, nil
}

// NewChainFromEnv builds the standard Gemini -> Groq -> Ollama chain from
// environment variables. Providers without an API key are excluded
// automatically; Ollama (keyless, local) is included unless
// OLLAMA_ENABLED=false. The resulting composition is logged at startup.
//
// Environment variables (see each provider's Load*Config for details):
//   - GEMINI_API_KEY / GEMINI_MODEL
//   - GROQ_API_KEY / GROQ_MODEL
//   - OLLAMA_ENABLED / OLLAMA_HOST / OLLAMA_MODEL
//   - SUMMARIZER_CHAR_LIMIT / SUMMARIZER_TIMEOUT
func NewChainFromEnv(logger *slog.Logger) (*Chain, error) {
	if logger == nil {
		logger = slog.Default()
	}
	opts := LoadOptions()

	var providers []Provider

	geminiCfg := LoadGeminiConfig(opts)
	if geminiCfg.APIKey != "" {
		providers = append(providers, NewGemini(geminiCfg))
	} else {
		logger.Info("summarizer provider excluded: GEMINI_API_KEY not set",
			slog.String("provider", ProviderGemini))
	}

	groqCfg := LoadGroqConfig(opts)
	if groqCfg.APIKey != "" {
		providers = append(providers, NewGroq(groqCfg))
	} else {
		logger.Info("summarizer provider excluded: GROQ_API_KEY not set",
			slog.String("provider", ProviderGroq))
	}

	if ollamaEnabled(logger) {
		ollamaCfg := LoadOllamaConfig(opts)
		providers = append(providers, NewOllama(ollamaCfg))
	} else {
		logger.Info("summarizer provider excluded: OLLAMA_ENABLED=false",
			slog.String("provider", ProviderOllama))
	}

	chain, err := NewChain(providers...)
	if err != nil {
		return nil, err
	}
	chain.logger = logger

	logger.Info("summarizer fallback chain configured",
		slog.String("order", strings.Join(chain.ProviderNames(), " -> ")),
		slog.Int("character_limit", opts.CharacterLimit),
		slog.Duration("timeout", opts.Timeout))

	return chain, nil
}

// ollamaEnabled parses OLLAMA_ENABLED with strconv.ParseBool. Unset means
// enabled (Ollama is the keyless last resort of the chain); an unparsable
// value falls back to enabled with a warning (fail-open, §8).
func ollamaEnabled(logger *slog.Logger) bool {
	v := os.Getenv("OLLAMA_ENABLED")
	if v == "" {
		return true
	}
	enabled, err := strconv.ParseBool(v)
	if err != nil {
		logger.Warn("Invalid OLLAMA_ENABLED, defaulting to enabled",
			slog.String("value", v))
		return true
	}
	return enabled
}

// ProviderNames returns the provider identifiers in trial order.
func (c *Chain) ProviderNames() []string {
	names := make([]string, len(c.providers))
	for i, p := range c.providers {
		names[i] = p.Name()
	}
	return names
}

// Summarize implements the fetch usecase Summarizer interface.
// The winning provider is logged; callers that need to persist it
// (summaries.provider) should use SummarizeWithProvider.
func (c *Chain) Summarize(ctx context.Context, articleText string) (string, error) {
	summary, _, err := c.SummarizeWithProvider(ctx, articleText)
	return summary, err
}

// SummarizeWithProvider tries each provider in order and returns the summary
// together with the name of the provider that produced it (for
// summaries.provider / fallback observability, §8).
func (c *Chain) SummarizeWithProvider(ctx context.Context, articleText string) (string, string, error) {
	var errs []error

	for _, p := range c.providers {
		start := time.Now()
		summary, err := p.Summarize(ctx, articleText)
		duration := time.Since(start)

		if err == nil {
			c.logger.InfoContext(ctx, "summarization completed",
				slog.String("provider", p.Name()),
				slog.Int("summary_length", text.CountRunes(summary)),
				slog.Duration("duration", duration))
			return summary, p.Name(), nil
		}

		// Provider errors already carry the provider name prefix.
		errs = append(errs, err)

		// Parent context is gone (shutdown / crawl deadline): abort instead
		// of hammering the remaining providers with a dead context.
		if ctx.Err() != nil {
			return "", "", fmt.Errorf("summarize aborted: %w", errors.Join(errs...))
		}

		c.logger.WarnContext(ctx, "summarization provider failed, falling back",
			slog.String("provider", p.Name()),
			slog.Duration("duration", duration),
			slog.String("error", err.Error()))
	}

	return "", "", fmt.Errorf("all summarizer providers failed: %w", errors.Join(errs...))
}
