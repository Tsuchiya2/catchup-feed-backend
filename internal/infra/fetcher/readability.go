package fetcher

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"catchup-feed/internal/resilience/circuitbreaker"
	"catchup-feed/internal/usecase/fetch"

	"github.com/go-shiori/go-readability"
)

// ReadabilityFetcher implements the ContentFetcher interface using Mozilla Readability algorithm.
// It fetches HTML content from URLs and extracts clean article text using go-shiori/go-readability.
//
// Features:
//   - SSRF prevention via URL validation
//   - Circuit breaker for fault tolerance
//   - Size limiting to prevent memory exhaustion
//   - Timeout protection against slow servers
//   - Redirect validation for security
//
// Thread safety: ReadabilityFetcher is safe for concurrent use.
type ReadabilityFetcher struct {
	client         *http.Client
	circuitBreaker *circuitbreaker.CircuitBreaker
	config         ContentFetchConfig
}

// NewReadabilityFetcher creates a new ReadabilityFetcher with the given configuration.
//
// The fetcher is configured with:
//   - Custom HTTP client with timeout and TLS settings
//   - Circuit breaker for fault tolerance
//   - Redirect validation for security
//   - Custom User-Agent for identification
//
// Parameters:
//   - config: Configuration for content fetching (timeouts, limits, security settings)
//
// Returns:
//   - *ReadabilityFetcher: Ready-to-use content fetcher
//
// Example:
//
//	config := DefaultConfig()
//	fetcher := NewReadabilityFetcher(config)
//	content, err := fetcher.FetchContent(ctx, "https://example.com/article")
func NewReadabilityFetcher(config ContentFetchConfig) *ReadabilityFetcher {
	// Create circuit breaker with custom configuration for content fetching
	cbConfig := circuitbreaker.Config{
		Name:             "content-fetch",
		MaxRequests:      5,
		Interval:         60 * time.Second,
		Timeout:          60 * time.Second,
		FailureThreshold: 0.6,
		MinRequests:      5,
	}
	cb := circuitbreaker.New(cbConfig)

	fetcher := &ReadabilityFetcher{
		circuitBreaker: cb,
		config:         config,
	}

	// Create HTTP client with redirect validation
	// Each redirect target is validated for security (SSRF check)
	client := &http.Client{
		Timeout: 30 * time.Second, // Overall request timeout
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12, // Enforce TLS 1.2+
			},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Check redirect limit
			if len(via) >= fetcher.config.MaxRedirects {
				return fmt.Errorf("%w: %d redirects", fetch.ErrTooManyRedirects, len(via))
			}

			// Validate each redirect target for SSRF
			if err := validateURL(req.URL.String(), fetcher.config.DenyPrivateIPs); err != nil {
				return fmt.Errorf("redirect target validation failed: %w", err)
			}

			return nil
		},
	}

	fetcher.client = client
	return fetcher
}

// FetchContent fetches and extracts article content from the given URL.
// This method implements the ContentFetcher interface.
//
// The fetch process:
//  1. Validates URL for security (SSRF prevention)
//  2. Executes HTTP request through circuit breaker
//  3. Enforces size limit while reading response
//  4. Extracts article content using Readability algorithm
//  5. Returns clean article text
//
// Security features:
//   - URL validation blocks private IPs (SSRF prevention)
//   - Size limiting prevents memory exhaustion
//   - Timeout prevents resource starvation
//   - Redirect validation ensures all targets are safe
//   - Circuit breaker prevents cascading failures
//
// Parameters:
//   - ctx: Context for cancellation and timeout control
//   - url: Article URL to fetch (must be http:// or https://)
//
// Returns:
//   - string: Extracted article content (plain text)
//   - error: Error if fetching or extraction fails
//
// Example:
//
//	content, err := fetcher.FetchContent(ctx, "https://example.com/article")
//	if err != nil {
//	    // Fall back to RSS content
//	    content = rssContent
//	}
func (f *ReadabilityFetcher) FetchContent(ctx context.Context, urlStr string) (string, error) {
	// Step 1: Validate URL for security
	if err := validateURL(urlStr, f.config.DenyPrivateIPs); err != nil {
		return "", err
	}

	// Step 2: Execute fetch through circuit breaker
	result, err := f.circuitBreaker.Execute(func() (interface{}, error) {
		return f.doFetch(ctx, urlStr)
	})

	if err != nil {
		return "", err
	}

	return result.(string), nil
}

// doFetch performs the actual HTTP request and content extraction.
// This is called by FetchContent through the circuit breaker.
//
// Steps:
//  1. Create HTTP request with context and custom User-Agent
//  2. Execute HTTP request
//  3. Read response body with size limiting
//  4. Extract article content using Readability
//  5. Return clean text
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - urlStr: Article URL to fetch
//
// Returns:
//   - interface{}: Extracted article content (as interface{} for circuit breaker)
//   - error: Error if fetching or extraction fails
func (f *ReadabilityFetcher) doFetch(ctx context.Context, urlStr string) (interface{}, error) {
	// Apply per-request timeout from config
	reqCtx, cancel := context.WithTimeout(ctx, f.config.Timeout)
	defer cancel()

	// Create HTTP request
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("%w: failed to create request: %v", fetch.ErrInvalidURL, err)
	}

	// Set custom User-Agent to identify our bot
	req.Header.Set("User-Agent", "CatchUpFeedBot/1.0")

	// Execute HTTP request
	resp, err := f.client.Do(req)
	if err != nil {
		// Check if error is timeout
		if reqCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("%w: request exceeded %v", fetch.ErrTimeout, f.config.Timeout)
		}
		// Check if error is due to redirect validation
		if urlErr, ok := err.(*url.Error); ok && urlErr.Err != nil {
			return "", urlErr.Err
		}
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check HTTP status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	// Read response body with size limit
	// This prevents memory exhaustion from oversized responses
	limitedReader := io.LimitReader(resp.Body, f.config.MaxBodySize+1)
	htmlBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check if response exceeded size limit
	if int64(len(htmlBytes)) > f.config.MaxBodySize {
		return "", fmt.Errorf("%w: response size %d bytes exceeds limit %d bytes",
			fetch.ErrBodyTooLarge, len(htmlBytes), f.config.MaxBodySize)
	}

	// Parse the final URL (may have changed due to redirects)
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		parsedURL = nil // Readability can work without URL
	}
	if resp.Request != nil && resp.Request.URL != nil {
		parsedURL = resp.Request.URL
	}

	// Extract article content using Readability
	// Create a new reader from the bytes we read
	htmlReader := io.NopCloser(bytes.NewReader(htmlBytes))
	article, err := readability.FromReader(htmlReader, parsedURL)
	if err != nil {
		return "", fmt.Errorf("%w: %v", fetch.ErrReadabilityFailed, err)
	}

	// Return clean article text
	// The Text field contains extracted content without HTML tags
	if article.TextContent == "" {
		// Fallback to Content if TextContent is empty
		if article.Content == "" {
			return "", fmt.Errorf("%w: no readable content found", fetch.ErrReadabilityFailed)
		}
		slog.Debug("using article Content instead of TextContent",
			slog.String("url", urlStr),
			slog.Int("content_length", len(article.Content)))
		return article.Content, nil
	}

	return article.TextContent, nil
}
