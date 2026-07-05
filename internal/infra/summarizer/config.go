// Package summarizer provides AI-powered text summarization.
//
// It implements a fallback chain over free-tier providers
// (Gemini -> Groq -> local Ollama). Each provider is a plain
// net/http client; no vendor SDK, no retry, no circuit breaker (C-3).
// Only public article content may be sent to cloud providers (C-12).
package summarizer

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

const (
	// minCharLimit is the minimum allowed character limit for summaries.
	minCharLimit = 100

	// maxCharLimit is the maximum allowed character limit for summaries.
	maxCharLimit = 5000

	// defaultCharLimit is used when SUMMARIZER_CHAR_LIMIT is unset or invalid.
	defaultCharLimit = 900

	// defaultTimeout is the per-request timeout, inherited from the old
	// Claude/OpenAI implementations. Override with SUMMARIZER_TIMEOUT.
	defaultTimeout = 60 * time.Second
)

// Options holds settings shared by all summarization providers.
type Options struct {
	// CharacterLimit is the maximum number of characters requested for a summary.
	// Valid range: 100-5000. Default: 900.
	CharacterLimit int

	// Timeout is the maximum duration of a single provider API call.
	Timeout time.Duration
}

// DefaultOptions returns the built-in defaults (900 chars, 60s timeout).
func DefaultOptions() Options {
	return Options{
		CharacterLimit: defaultCharLimit,
		Timeout:        defaultTimeout,
	}
}

// withDefaults fills zero-valued fields with the built-in defaults so that
// providers constructed with a partial Options struct still behave sanely.
func (o Options) withDefaults() Options {
	if o.CharacterLimit == 0 {
		o.CharacterLimit = defaultCharLimit
	}
	if o.Timeout == 0 {
		o.Timeout = defaultTimeout
	}
	return o
}

// LoadOptions loads shared summarizer settings from environment variables.
// Invalid values fall back to defaults with a warning log (fail-open:
// a bad tuning knob must not stop the hourly crawl).
//
// Environment variables:
//   - SUMMARIZER_CHAR_LIMIT: summary length in characters (default 900, range 100-5000)
//   - SUMMARIZER_TIMEOUT: per-request timeout as a Go duration, e.g. "60s" (default 60s)
func LoadOptions() Options {
	opts := DefaultOptions()

	if envLimit := os.Getenv("SUMMARIZER_CHAR_LIMIT"); envLimit != "" {
		parsed, err := strconv.Atoi(envLimit)
		switch {
		case err != nil:
			slog.Warn("Invalid SUMMARIZER_CHAR_LIMIT format, using default",
				slog.String("value", envLimit),
				slog.Int("default", defaultCharLimit),
				slog.String("error", err.Error()))
		case ValidateCharacterLimit(parsed) != nil:
			slog.Warn("SUMMARIZER_CHAR_LIMIT out of valid range, using default",
				slog.Int("value", parsed),
				slog.Int("min", minCharLimit),
				slog.Int("max", maxCharLimit),
				slog.Int("default", defaultCharLimit))
		default:
			opts.CharacterLimit = parsed
		}
	}

	if envTimeout := os.Getenv("SUMMARIZER_TIMEOUT"); envTimeout != "" {
		parsed, err := time.ParseDuration(envTimeout)
		if err != nil || parsed <= 0 {
			slog.Warn("Invalid SUMMARIZER_TIMEOUT, using default",
				slog.String("value", envTimeout),
				slog.Duration("default", defaultTimeout))
		} else {
			opts.Timeout = parsed
		}
	}

	return opts
}

// ValidateCharacterLimit validates that the character limit is within the
// valid range (100-5000). Returns a descriptive error if out of range.
func ValidateCharacterLimit(limit int) error {
	if limit < minCharLimit {
		return fmt.Errorf("character limit %d is below minimum %d", limit, minCharLimit)
	}
	if limit > maxCharLimit {
		return fmt.Errorf("character limit %d exceeds maximum %d", limit, maxCharLimit)
	}
	return nil
}
