package summarizer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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

// postJSON sends a JSON POST request and decodes a JSON response into out.
// Non-2xx responses are returned as errors including status and a body
// snippet (rate limits and auth failures become visible in logs, and the
// chain moves on to the next provider).
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

	const maxErrBody = 512
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBody))
		return fmt.Errorf("%s: api error: status %d: %s", provider, resp.StatusCode, string(snippet))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode response: %w", provider, err)
	}
	return nil
}
