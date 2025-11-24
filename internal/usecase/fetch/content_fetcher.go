package fetch

import (
	"context"
	"errors"
)

// ContentFetcher is an interface for fetching full article content from URLs.
// Implementations should extract clean article text from web pages using
// various extraction algorithms (e.g., Mozilla Readability, Mercury Parser).
//
// This interface supports content enhancement for RSS feeds that only provide
// article summaries instead of full content. By fetching the full article from
// the source URL, AI summarization quality can be significantly improved.
//
// Example usage:
//
//	fetcher := NewReadabilityFetcher(config)
//	content, err := fetcher.FetchContent(ctx, "https://example.com/article")
//	if err != nil {
//	    // Handle error, fall back to RSS content
//	}
//
// Security considerations:
//   - Implementations MUST prevent Server-Side Request Forgery (SSRF) attacks
//   - Implementations MUST enforce size limits to prevent memory exhaustion
//   - Implementations MUST enforce timeouts to prevent resource starvation
//   - Implementations MUST validate redirect targets
type ContentFetcher interface {
	// FetchContent fetches and extracts article content from the given URL.
	//
	// The implementation should:
	// 1. Validate the URL for security (prevent SSRF)
	// 2. Fetch the HTML content via HTTP/HTTPS
	// 3. Extract the article content using an extraction algorithm
	// 4. Return clean article text without HTML tags or navigation elements
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout control
	//   - url: The article URL to fetch (must be http:// or https://)
	//
	// Returns:
	//   - string: Extracted article content (plain text)
	//   - error: Error if fetching or extraction fails
	//
	// Errors:
	//   - ErrInvalidURL: URL format is invalid or uses unsupported scheme
	//   - ErrPrivateIP: URL resolves to a private IP address (SSRF prevention)
	//   - ErrTooManyRedirects: Redirect chain exceeds configured maximum
	//   - ErrBodyTooLarge: Response body exceeds size limit
	//   - ErrTimeout: Request timed out
	//   - ErrReadabilityFailed: Content extraction failed
	//   - gobreaker.ErrOpenState: Circuit breaker is open (too many failures)
	//
	// The caller should handle errors gracefully and fall back to RSS content.
	FetchContent(ctx context.Context, url string) (string, error)
}

// Sentinel errors for content fetching operations.
// These errors allow callers to distinguish between different failure modes
// and implement appropriate fallback strategies.
var (
	// ErrInvalidURL indicates the URL format is invalid or uses an unsupported scheme.
	// Only http:// and https:// schemes are supported.
	//
	// Example:
	//   - "not-a-url" → ErrInvalidURL
	//   - "file:///etc/passwd" → ErrInvalidURL
	//   - "ftp://example.com" → ErrInvalidURL
	ErrInvalidURL = errors.New("invalid URL or unsupported scheme")

	// ErrPrivateIP indicates the URL resolves to a private IP address.
	// This error prevents Server-Side Request Forgery (SSRF) attacks.
	//
	// Blocked IP ranges:
	//   - 127.0.0.0/8 (loopback)
	//   - 10.0.0.0/8 (private)
	//   - 172.16.0.0/12 (private)
	//   - 192.168.0.0/16 (private)
	//   - 169.254.0.0/16 (link-local)
	//   - ::1 (IPv6 loopback)
	//   - fc00::/7 (IPv6 private)
	//   - fe80::/10 (IPv6 link-local)
	//
	// Example:
	//   - "http://localhost" → ErrPrivateIP
	//   - "http://192.168.1.1" → ErrPrivateIP
	//   - "http://10.0.0.1" → ErrPrivateIP
	ErrPrivateIP = errors.New("private IP access denied (SSRF prevention)")

	// ErrTooManyRedirects indicates the redirect chain exceeded the configured maximum.
	// This prevents infinite redirect loops and redirect-based attacks.
	//
	// Example:
	//   - URL redirects 6 times when max is 5 → ErrTooManyRedirects
	ErrTooManyRedirects = errors.New("too many redirects")

	// ErrBodyTooLarge indicates the response body exceeded the size limit.
	// This prevents memory exhaustion attacks from oversized responses.
	//
	// Example:
	//   - Response is 15MB when max is 10MB → ErrBodyTooLarge
	ErrBodyTooLarge = errors.New("response body too large")

	// ErrTimeout indicates the request exceeded the configured timeout.
	// This prevents resource starvation from slow or unresponsive servers.
	//
	// Example:
	//   - Request takes 15s when timeout is 10s → ErrTimeout
	ErrTimeout = errors.New("request timeout")

	// ErrReadabilityFailed indicates content extraction failed.
	// This can occur when:
	//   - HTML structure is invalid or cannot be parsed
	//   - No article content found (page has no readable text)
	//   - Extraction algorithm failed
	//
	// Callers should fall back to RSS content when this error occurs.
	ErrReadabilityFailed = errors.New("content extraction failed")
)
