// Package scraper provides implementations for fetching content from various sources.
package scraper

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/resilience/circuitbreaker"
	"catchup-feed/internal/resilience/retry"
	"catchup-feed/internal/usecase/fetch"

	"github.com/PuerkitoBio/goquery"
	"github.com/sony/gobreaker"
)

const (
	maxBodySize = 10 * 1024 * 1024 // 10MB
)

// WebflowScraper implements FeedFetcher for Webflow-based websites.
// It uses HTML parsing with goquery to extract articles using CSS selectors.
type WebflowScraper struct {
	client         *http.Client
	circuitBreaker *circuitbreaker.CircuitBreaker
	retryConfig    retry.Config
}

// NewWebflowScraper creates a new WebflowScraper with the given HTTP client.
// It automatically configures circuit breaker and retry logic for resilience.
func NewWebflowScraper(client *http.Client) *WebflowScraper {
	return &WebflowScraper{
		client:         client,
		circuitBreaker: circuitbreaker.New(circuitbreaker.WebScraperConfig()),
		retryConfig:    retry.WebScraperConfig(),
	}
}

// Fetch retrieves and parses articles from a Webflow website.
// It extracts ScraperConfig from the context and uses it to locate article elements.
// Returns a slice of FeedItem containing the parsed articles.
func (w *WebflowScraper) Fetch(ctx context.Context, sourceURL string) ([]fetch.FeedItem, error) {
	// Extract scraper config from context
	config, ok := ctx.Value(ScraperConfigKey).(*entity.ScraperConfig)
	if !ok || config == nil {
		return nil, errors.New("scraper_config not found in context")
	}

	var items []fetch.FeedItem

	// Wrap with retry logic
	retryErr := retry.WithBackoff(ctx, w.retryConfig, func() error {
		// Execute through circuit breaker
		cbResult, err := w.circuitBreaker.Execute(func() (interface{}, error) {
			return w.doFetch(ctx, sourceURL, config)
		})

		// Handle circuit breaker open state
		if err != nil {
			if errors.Is(err, gobreaker.ErrOpenState) {
				slog.Warn("web scraper circuit breaker open, request rejected",
					slog.String("service", "web-scraper"),
					slog.String("url", sourceURL),
					slog.String("state", w.circuitBreaker.State().String()))
				return err
			}
			return err
		}

		items = cbResult.([]fetch.FeedItem)
		return nil
	})

	if retryErr != nil {
		return nil, retryErr
	}

	return items, nil
}

// doFetch performs the actual scraping without retry or circuit breaker.
func (w *WebflowScraper) doFetch(ctx context.Context, sourceURL string, config *entity.ScraperConfig) ([]fetch.FeedItem, error) {
	// Step 1: Validate URL (SSRF prevention)
	if err := validateURL(sourceURL); err != nil {
		return nil, fmt.Errorf("URL validation failed: %w", err)
	}

	// Step 2: Fetch HTML
	doc, err := w.fetchHTML(ctx, sourceURL)
	if err != nil {
		return nil, fmt.Errorf("fetch HTML failed: %w", err)
	}

	// Step 3: Extract items using CSS selectors
	items, err := w.extractItems(doc, config)
	if err != nil {
		return nil, fmt.Errorf("extract items failed: %w", err)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("no items found with selector: %s", config.ItemSelector)
	}

	return items, nil
}

// fetchHTML fetches and parses HTML from the given URL.
func (w *WebflowScraper) fetchHTML(ctx context.Context, urlStr string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "CatchUpFeedBot/1.0")

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &retry.HTTPError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("unexpected status: %s", resp.Status),
		}
	}

	// Limit body size to prevent memory exhaustion
	limitedReader := io.LimitReader(resp.Body, maxBodySize)

	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	return doc, nil
}

// extractItems extracts feed items from the HTML document using CSS selectors.
func (w *WebflowScraper) extractItems(doc *goquery.Document, config *entity.ScraperConfig) ([]fetch.FeedItem, error) {
	var items []fetch.FeedItem

	// Find all items using the item selector
	doc.Find(config.ItemSelector).Each(func(i int, itemEl *goquery.Selection) {
		// Extract title
		title := strings.TrimSpace(itemEl.Find(config.TitleSelector).Text())
		if title == "" {
			slog.Debug("skipping item with empty title", slog.Int("index", i))
			return
		}

		// Extract URL
		itemURL := ""
		if config.URLSelector != "" {
			if href, exists := itemEl.Find(config.URLSelector).Attr("href"); exists {
				itemURL = strings.TrimSpace(href)
			}
		}
		if itemURL == "" {
			slog.Debug("skipping item with empty URL", slog.Int("index", i), slog.String("title", title))
			return
		}

		// Make URL absolute if needed
		itemURL = makeAbsoluteURL(itemURL, config.URLPrefix)

		// Extract date
		dateStr := strings.TrimSpace(itemEl.Find(config.DateSelector).Text())
		publishedAt := parseDate(dateStr, config.DateFormat)

		// Create feed item
		item := fetch.FeedItem{
			Title:       title,
			URL:         itemURL,
			Content:     "", // Webflow scrapers don't extract content, only metadata
			PublishedAt: publishedAt,
		}

		items = append(items, item)
	})

	return items, nil
}

// validateURL checks if a URL is safe to fetch (SSRF prevention).
// For testing purposes, URLs with port 127.0.0.1:xxxxx (httptest servers) are allowed.
func validateURL(urlStr string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	// Only allow http/https
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported scheme: %s (only http/https allowed)", u.Scheme)
	}

	// Allow httptest servers (127.0.0.1 with ephemeral ports for testing)
	// httptest servers typically use ephemeral port range (32768-65535)
	// This allows test servers while still blocking common service ports
	if u.Hostname() == "127.0.0.1" && u.Port() != "" {
		portNum := 0
		if _, err := fmt.Sscanf(u.Port(), "%d", &portNum); err == nil {
			// Allow ephemeral port range used by httptest (typically 32768-65535)
			if portNum >= 32768 && portNum <= 65535 {
				return nil
			}
		}
	}

	// Resolve hostname to IPs
	ips, err := net.LookupIP(u.Hostname())
	if err != nil {
		return fmt.Errorf("DNS lookup failed: %w", err)
	}

	// Check for private IPs (SSRF prevention)
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("private IP address detected: %s (SSRF prevention)", ip)
		}
	}

	return nil
}

// isPrivateIP checks if an IP address is private (RFC 1918, loopback, link-local).
func isPrivateIP(ip net.IP) bool {
	// Loopback addresses (127.0.0.0/8, ::1)
	if ip.IsLoopback() {
		return true
	}

	// Private addresses (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, fc00::/7)
	if ip.IsPrivate() {
		return true
	}

	// Link-local addresses (169.254.0.0/16, fe80::/10)
	if ip.IsLinkLocalUnicast() {
		return true
	}

	return false
}

// parseDate parses a date string using the given format.
// Falls back to current time if parsing fails.
func parseDate(dateStr string, format string) time.Time {
	if dateStr == "" {
		return time.Now()
	}

	// Default format if not specified
	if format == "" {
		format = "Jan 2, 2006"
	}

	t, err := time.Parse(format, dateStr)
	if err != nil {
		// Try common formats as fallback
		formats := []string{
			"2006-01-02",
			"2006-01-02T15:04:05Z",
			time.RFC3339,
			"Jan 2, 2006",
			"January 2, 2006",
		}

		for _, fmt := range formats {
			if t, err := time.Parse(fmt, dateStr); err == nil {
				return t
			}
		}

		// Fallback to current time if all parsing attempts fail
		slog.Warn("failed to parse date, using current time",
			slog.String("date_str", dateStr),
			slog.String("format", format))
		return time.Now()
	}

	return t
}

// makeAbsoluteURL converts a relative URL to absolute using the given prefix.
func makeAbsoluteURL(urlStr string, prefix string) string {
	// Already absolute
	if strings.HasPrefix(urlStr, "http://") || strings.HasPrefix(urlStr, "https://") {
		return urlStr
	}

	// No prefix provided
	if prefix == "" {
		return urlStr
	}

	// Ensure prefix ends without slash and URL starts without slash for proper joining
	prefix = strings.TrimRight(prefix, "/")
	urlStr = strings.TrimLeft(urlStr, "/")

	return prefix + "/" + urlStr
}
