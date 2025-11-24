package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/resilience/circuitbreaker"
	"catchup-feed/internal/resilience/retry"
	"catchup-feed/internal/usecase/fetch"

	"github.com/sony/gobreaker"
)

// RemixScraper implements FeedFetcher for Remix-based websites.
// It extracts JSON data from the window.__remixContext embedded script.
type RemixScraper struct {
	client         *http.Client
	circuitBreaker *circuitbreaker.CircuitBreaker
	retryConfig    retry.Config
}

// NewRemixScraper creates a new RemixScraper with the given HTTP client.
// It automatically configures circuit breaker and retry logic for resilience.
func NewRemixScraper(client *http.Client) *RemixScraper {
	return &RemixScraper{
		client:         client,
		circuitBreaker: circuitbreaker.New(circuitbreaker.WebScraperConfig()),
		retryConfig:    retry.WebScraperConfig(),
	}
}

// Fetch retrieves and parses articles from a Remix website.
// It extracts the window.__remixContext JSON from the page and parses it into feed items.
func (r *RemixScraper) Fetch(ctx context.Context, sourceURL string) ([]fetch.FeedItem, error) {
	// Extract scraper config from context
	config, ok := ctx.Value(ScraperConfigKey).(*entity.ScraperConfig)
	if !ok || config == nil {
		return nil, errors.New("scraper_config not found in context")
	}

	var items []fetch.FeedItem

	// Wrap with retry logic
	retryErr := retry.WithBackoff(ctx, r.retryConfig, func() error {
		// Execute through circuit breaker
		cbResult, err := r.circuitBreaker.Execute(func() (interface{}, error) {
			return r.doFetch(ctx, sourceURL, config)
		})

		// Handle circuit breaker open state
		if err != nil {
			if errors.Is(err, gobreaker.ErrOpenState) {
				slog.Warn("remix scraper circuit breaker open, request rejected",
					slog.String("service", "remix-scraper"),
					slog.String("url", sourceURL),
					slog.String("state", r.circuitBreaker.State().String()))
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
func (r *RemixScraper) doFetch(ctx context.Context, sourceURL string, config *entity.ScraperConfig) ([]fetch.FeedItem, error) {
	// Step 1: Validate URL (SSRF prevention)
	if err := validateURL(sourceURL); err != nil {
		return nil, fmt.Errorf("URL validation failed: %w", err)
	}

	// Step 2: Fetch HTML
	html, err := r.fetchHTML(ctx, sourceURL)
	if err != nil {
		return nil, fmt.Errorf("fetch HTML failed: %w", err)
	}

	// Step 3: Extract Remix context JSON
	jsonData, err := r.extractRemixContext(html)
	if err != nil {
		return nil, fmt.Errorf("extract Remix context failed: %w", err)
	}

	// Step 4: Parse issues from JSON
	items, err := r.parseIssues(jsonData, config)
	if err != nil {
		return nil, fmt.Errorf("parse issues failed: %w", err)
	}

	if len(items) == 0 {
		return nil, errors.New("no issues found in Remix context")
	}

	return items, nil
}

// fetchHTML fetches HTML from the given URL.
func (r *RemixScraper) fetchHTML(ctx context.Context, urlStr string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "CatchUpFeedBot/1.0")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", &retry.HTTPError{
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("unexpected status: %s", resp.Status),
		}
	}

	// Limit body size to prevent memory exhaustion
	limitedReader := io.LimitReader(resp.Body, maxBodySize)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	return string(bodyBytes), nil
}

// extractRemixContext extracts and parses JSON from window.__remixContext.
func (r *RemixScraper) extractRemixContext(html string) (map[string]interface{}, error) {
	// Use regex to find window.__remixContext = {...};
	// Pattern handles various whitespace scenarios including newlines
	// (?s) flag makes . match newlines for multiline JSON
	pattern := regexp.MustCompile(`(?s)window\.__remixContext\s*=\s*(\{.*?\});`)
	matches := pattern.FindStringSubmatch(html)

	if len(matches) < 2 {
		return nil, errors.New("window.__remixContext not found in HTML")
	}

	jsonText := matches[1]

	// Parse JSON
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonText), &data); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	return data, nil
}

// parseIssues parses feed items from the Remix context JSON.
func (r *RemixScraper) parseIssues(jsonData map[string]interface{}, config *entity.ScraperConfig) ([]fetch.FeedItem, error) {
	var items []fetch.FeedItem

	// Navigate to routes[contextKey].loaderData.issues
	routes, ok := jsonData["routes"].(map[string]interface{})
	if !ok {
		return nil, errors.New("routes not found in Remix context")
	}

	// Use contextKey from config
	contextKey := config.ContextKey
	if contextKey == "" {
		// Try to find the first route with loaderData
		for key, routeData := range routes {
			if routeMap, ok := routeData.(map[string]interface{}); ok {
				if _, hasLoader := routeMap["loaderData"]; hasLoader {
					contextKey = key
					break
				}
			}
		}
		if contextKey == "" {
			return nil, errors.New("no route with loaderData found")
		}
	}

	routeData, ok := routes[contextKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("route %s not found in Remix context", contextKey)
	}

	loaderData, ok := routeData["loaderData"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("loaderData not found in route %s", contextKey)
	}

	issuesArray, ok := loaderData["issues"].([]interface{})
	if !ok {
		return nil, errors.New("issues array not found in loaderData")
	}

	// Parse each issue
	for i, issueData := range issuesArray {
		issueMap, ok := issueData.(map[string]interface{})
		if !ok {
			slog.Warn("skipping non-object issue", slog.Int("index", i))
			continue
		}

		// Extract title (web_title field)
		title, _ := issueMap["web_title"].(string)
		if title == "" {
			slog.Debug("skipping issue with empty title", slog.Int("index", i))
			continue
		}

		// Extract slug
		slug, _ := issueMap["slug"].(string)
		if slug == "" {
			slog.Debug("skipping issue with empty slug", slog.Int("index", i), slog.String("title", title))
			continue
		}

		// Build URL from slug and prefix
		itemURL := makeAbsoluteURL(slug, config.URLPrefix)

		// Extract published date (override_scheduled_at field)
		publishedStr, _ := issueMap["override_scheduled_at"].(string)
		publishedAt := time.Now()
		if publishedStr != "" {
			if t, err := time.Parse(time.RFC3339, publishedStr); err == nil {
				publishedAt = t
			} else if t, err := time.Parse("2006-01-02", publishedStr); err == nil {
				publishedAt = t
			}
		}

		// Create feed item
		item := fetch.FeedItem{
			Title:       title,
			URL:         itemURL,
			Content:     "", // Remix scrapers don't extract content
			PublishedAt: publishedAt,
		}

		items = append(items, item)
	}

	return items, nil
}
