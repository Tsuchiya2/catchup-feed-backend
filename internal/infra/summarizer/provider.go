package summarizer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"
)

// Provider is a single generation backend (gemini / groq / ollama).
// Implementations must be safe for concurrent use.
type Provider interface {
	// Name returns the stable provider identifier recorded in
	// summaries.provider (e.g. "gemini", "groq", "ollama").
	Name() string

	// Summarize generates a Japanese summary of the given public article text.
	// Errors are returned as-is; there is no retry (C-3) — the fallback
	// chain or the next cron run handles failures.
	Summarize(ctx context.Context, text string) (string, error)

	// Generate sends the prompt verbatim and returns the raw completion.
	// It is the generic entry point used by the radio script generator
	// (D-3: same chain as summaries). The caller owns prompt size; no
	// truncation is applied. Only public-article-derived text may be
	// embedded in prompts sent to cloud providers (C-12).
	Generate(ctx context.Context, prompt string) (string, error)
}

// maxInputChars limits prompt input to avoid provider token limits.
// Inherited from the old Claude/OpenAI implementations (~10,000 chars).
const maxInputChars = 10000

// buildPrompt constructs the Japanese summarization prompt.
// Only the public article text is embedded (C-12: private data never
// goes through this path).
//
// Example output:
//
//	"以下のテキストを日本語で900文字以内で要約してください：\n{text}"
func buildPrompt(charLimit int, text string) string {
	return fmt.Sprintf("以下のテキストを日本語で%d文字以内で要約してください：\n%s", charLimit, text)
}

// truncateInput truncates overly long article text before prompting.
// The cut is backed off to a rune boundary so multi-byte characters
// (Japanese article bodies) are never split into invalid UTF-8.
func truncateInput(provider, text string) string {
	if len(text) <= maxInputChars {
		return text
	}
	cut := maxInputChars
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	truncated := text[:cut] + "...\n(内容が長いため切り詰めました)"
	slog.Warn("text truncated for summarization",
		slog.String("provider", provider),
		slog.Int("original_length", len(text)),
		slog.Int("truncated_length", len(truncated)))
	return truncated
}

// newHTTPClient returns the shared http.Client configuration for providers.
// The per-request deadline comes from context (Options.Timeout), not the client.
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 5,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}
}

// rateLimitError is an HTTP 429 from a provider, carrying the provider's
// retry hint when one was recoverable (D-26 (2)). The chain uses it to wait
// a bounded time and retry the same provider once before falling back;
// every other consumer sees a plain error with the same message format as
// any non-2xx response.
type rateLimitError struct {
	provider string
	// retryAfter is the provider-suggested wait; 0 means no usable hint
	// (the chain then falls back immediately, current behavior).
	retryAfter time.Duration
	msg        string
}

func (e *rateLimitError) Error() string { return e.msg }

// retryHintPatterns extract a seconds value from a 429 response body, in
// priority order. Gemini writes "... Please retry in 37.5s." in the error
// message and a structured `"retryDelay": "37s"` detail; Groq writes
// "... Please try again in 4.028s.".
var retryHintPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:retry|try again) in\s+([0-9]+(?:\.[0-9]+)?)\s*s`),
	regexp.MustCompile(`"retryDelay"\s*:\s*"([0-9]+(?:\.[0-9]+)?)s"`),
}

// parseRetryAfterHint extracts the suggested wait from a 429 response:
// the Retry-After header (delta-seconds form only — both providers use it)
// wins over the body message patterns. Returns 0 when no positive hint is
// found; the HTTP-date Retry-After form is deliberately not parsed (neither
// provider sends it, and a clock-dependent parse is a worse failure mode
// than falling back immediately).
func parseRetryAfterHint(header string, body []byte) time.Duration {
	if header != "" {
		if secs, err := strconv.ParseFloat(header, 64); err == nil {
			return secondsToDuration(secs)
		}
	}
	for _, re := range retryHintPatterns {
		if m := re.FindSubmatch(body); m != nil {
			if secs, err := strconv.ParseFloat(string(m[1]), 64); err == nil {
				return secondsToDuration(secs)
			}
		}
	}
	return 0
}

// secondsToDuration converts a positive seconds hint to a Duration, rounded
// up to a whole second so a fractional hint ("4.028s") never retries early.
func secondsToDuration(secs float64) time.Duration {
	if secs <= 0 || math.IsInf(secs, 1) || secs > (math.MaxInt64/float64(time.Second)) {
		return 0
	}
	return time.Duration(math.Ceil(secs)) * time.Second
}

// postJSON sends a JSON POST request and decodes a JSON response into out.
// Non-2xx responses are returned as errors including status and a body
// snippet (rate limits and auth failures become visible in logs, and the
// chain moves on to the next provider). A 429 becomes a *rateLimitError so
// the chain can honor the provider's retry hint (D-26 (2)); its message is
// format-identical to any other non-2xx error.
func postJSON(ctx context.Context, client *http.Client, provider, url string, headers map[string]string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("%s: marshal request: %w", provider, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: build request: %w", provider, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: request failed: %w", provider, err)
	}
	defer func() { _ = resp.Body.Close() }()

	const (
		maxErrBody = 512
		// maxHintBody is the read limit for 429 bodies: Gemini buries the
		// "Please retry in Xs" hint after a long quota description, so the
		// hint scan needs more than the logged snippet.
		maxHintBody = 4096
	)
	if resp.StatusCode == http.StatusTooManyRequests {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxHintBody))
		snippet := body
		if len(snippet) > maxErrBody {
			snippet = snippet[:maxErrBody]
		}
		return &rateLimitError{
			provider:   provider,
			retryAfter: parseRetryAfterHint(resp.Header.Get("Retry-After"), body),
			msg:        fmt.Sprintf("%s: api error: status %d: %s", provider, resp.StatusCode, string(snippet)),
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
		return fmt.Errorf("%s: api error: status %d: %s", provider, resp.StatusCode, string(snippet))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode response: %w", provider, err)
	}
	return nil
}
