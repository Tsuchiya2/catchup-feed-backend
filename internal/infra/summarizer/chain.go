package summarizer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"catchup-feed/internal/utils/text"
)

// ErrNoProviders is returned by NewChain / NewChainFromEnv when no provider
// is configured (no API keys and Ollama disabled).
var ErrNoProviders = errors.New("summarizer: no providers configured")

// Chain tries providers in order (Gemini -> Groq -> Ollama) and returns the
// first successful summary. Any provider error — 4xx/5xx, rate limit,
// timeout, connection refused — moves on to the next provider; only parent
// context cancellation aborts the chain. There is no circuit breaker (C-3):
// if every provider fails, the error is returned and the article is picked
// up again on the next cron run (§8 縮退許容).
//
// The single exception to "no retry" is D-26 (2), the 2026-07-13 欠番障害
// 恒久対応: a 429 that carries the provider's own retry hint waits
// min(hint, maxRetryAfterWait) and retries the same provider exactly once
// before falling back. The waits are bounded per process by
// retryWaitBudget; past the budget — and for any 429 without a usable
// hint, or any other error — the chain falls back immediately as before.
// The rationale: Gemini's minute quota resets in seconds, while falling
// back lands the huge outro prompt on Groq's tighter TPM and finally on
// Ollama, which is exactly the chain that killed the 7/13 episode.
//
// D-3: the radio script generator reuses this same chain (and the budget is
// shared with worker summaries by design — D-26: チェーンは radio/worker
// 共用のまま、遅延は累積予算で有界).
type Chain struct {
	providers []Provider
	logger    *slog.Logger

	// sleep waits for the 429 retry hint; injectable for tests. Returns
	// the context error when canceled mid-wait.
	sleep func(ctx context.Context, d time.Duration) error
	// retryWaited accumulates the nanoseconds already spent waiting on 429
	// hints in this process (D-26: 累積待機は retryWaitBudget で打ち切り).
	retryWaited atomic.Int64
}

const (
	// maxRetryAfterWait caps a single 429 hint wait (D-26: min(ヒント, 60s)).
	maxRetryAfterWait = 60 * time.Second
	// retryWaitBudget caps the total 429 wait time per process execution;
	// once spent, every 429 falls back immediately (current behavior).
	retryWaitBudget = 5 * time.Minute
)

// NewChain creates a fallback chain over the given providers, tried in order.
// Returns ErrNoProviders if the list is empty.
func NewChain(providers ...Provider) (*Chain, error) {
	if len(providers) == 0 {
		return nil, ErrNoProviders
	}
	return &Chain{providers: providers, logger: slog.Default(), sleep: sleepContext}, nil
}

// sleepContext blocks for d or until ctx is canceled, whichever comes first.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
	return c.fallback(ctx, "summarize", func(p Provider) (string, error) {
		return p.Summarize(ctx, articleText)
	})
}

// Generate tries each provider in order with the prompt sent verbatim and
// returns the completion together with the winning provider name. It is the
// generic entry point used by the radio script generator (D-3: 台本は要約と
// 同一のフォールバック連鎖). Same semantics as SummarizeWithProvider: no
// retry, no circuit breaker (C-3); only public-article-derived text may be
// embedded in the prompt (C-12).
func (c *Chain) Generate(ctx context.Context, prompt string) (string, string, error) {
	return c.fallback(ctx, "generate", func(p Provider) (string, error) {
		return p.Generate(ctx, prompt)
	})
}

// fallback runs the provider chain for one operation and returns the first
// successful output with the provider name. A provider gets at most two
// attempts: the second only after a hinted 429 within the retry-wait budget
// (D-26 (2)); everything else falls straight through to the next provider.
func (c *Chain) fallback(ctx context.Context, op string, call func(Provider) (string, error)) (string, string, error) {
	var errs []error

	for _, p := range c.providers {
		for attempt := 0; ; attempt++ {
			start := time.Now()
			out, err := call(p)
			duration := time.Since(start)

			if err == nil {
				c.logger.InfoContext(ctx, op+" completed",
					slog.String("provider", p.Name()),
					slog.Int("output_length", text.CountRunes(out)),
					slog.Duration("duration", duration))
				return out, p.Name(), nil
			}

			// Provider errors already carry the provider name prefix.
			errs = append(errs, err)

			// Parent context is gone (shutdown / crawl deadline): abort instead
			// of hammering the remaining providers with a dead context.
			if ctx.Err() != nil {
				return "", "", fmt.Errorf("%s aborted: %w", op, errors.Join(errs...))
			}

			// D-26 (2): a 429 with the provider's own retry hint gets one
			// bounded wait-and-retry on the same provider before falling
			// back — the quota that just tripped resets in seconds, while
			// the next provider downstream may be a strictly worse fit for
			// this prompt (7/13 欠番の三段連鎖). Second attempts never
			// retry again regardless of the error.
			if attempt == 0 {
				if wait, ok := c.reserveRetryWait(err); ok {
					c.logger.WarnContext(ctx, op+" provider rate limited with retry hint, waiting for one same-provider retry (D-26)",
						slog.String("provider", p.Name()),
						slog.Duration("wait", wait),
						slog.String("error", err.Error()))
					if serr := c.sleep(ctx, wait); serr != nil {
						errs = append(errs, serr)
						return "", "", fmt.Errorf("%s aborted: %w", op, errors.Join(errs...))
					}
					continue
				}
			}

			c.logger.WarnContext(ctx, op+" provider failed, falling back",
				slog.String("provider", p.Name()),
				slog.Duration("duration", duration),
				slog.String("error", err.Error()))
			break
		}
	}

	return "", "", fmt.Errorf("all %s providers failed: %w", op, errors.Join(errs...))
}

// reserveRetryWait decides whether err earns a same-provider retry (D-26
// (2)): it must be a 429 carrying a usable hint, and the capped wait —
// min(hint, maxRetryAfterWait) — must still fit into the process-wide
// retryWaitBudget. Fitting waits are reserved atomically (the chain is
// shared across worker goroutines) so the accumulated total never exceeds
// the budget; once it is spent, every 429 falls back immediately.
func (c *Chain) reserveRetryWait(err error) (time.Duration, bool) {
	var rle *rateLimitError
	if !errors.As(err, &rle) || rle.retryAfter <= 0 {
		return 0, false
	}
	wait := min(rle.retryAfter, maxRetryAfterWait)
	for {
		used := c.retryWaited.Load()
		if used+int64(wait) > int64(retryWaitBudget) {
			return 0, false
		}
		if c.retryWaited.CompareAndSwap(used, used+int64(wait)) {
			return wait, true
		}
	}
}
