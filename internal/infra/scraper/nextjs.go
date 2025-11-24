package scraper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/resilience/circuitbreaker"
	"catchup-feed/internal/resilience/retry"
	"catchup-feed/internal/usecase/fetch"

	"github.com/PuerkitoBio/goquery"
	"github.com/sony/gobreaker"
)

// NextJSScraper implements FeedFetcher for Next.js-based websites.
// It extracts JSON data from the __NEXT_DATA__ script tag and parses it into feed items.
type NextJSScraper struct {
	client         *http.Client
	circuitBreaker *circuitbreaker.CircuitBreaker
	retryConfig    retry.Config
}

// NewNextJSScraper creates a new NextJSScraper with the given HTTP client.
// It automatically configures circuit breaker and retry logic for resilience.
func NewNextJSScraper(client *http.Client) *NextJSScraper {
	return &NextJSScraper{
		client:         client,
		circuitBreaker: circuitbreaker.New(circuitbreaker.WebScraperConfig()),
		retryConfig:    retry.WebScraperConfig(),
	}
}

// Fetch retrieves and parses articles from a Next.js website.
// It extracts the __NEXT_DATA__ JSON from the page and parses it into feed items.
func (n *NextJSScraper) Fetch(ctx context.Context, sourceURL string) ([]fetch.FeedItem, error) {
	// Extract scraper config from context
	config, ok := ctx.Value(ScraperConfigKey).(*entity.ScraperConfig)
	if !ok || config == nil {
		return nil, errors.New("scraper_config not found in context")
	}

	var items []fetch.FeedItem

	// Wrap with retry logic
	retryErr := retry.WithBackoff(ctx, n.retryConfig, func() error {
		// Execute through circuit breaker
		cbResult, err := n.circuitBreaker.Execute(func() (interface{}, error) {
			return n.doFetch(ctx, sourceURL, config)
		})

		// Handle circuit breaker open state
		if err != nil {
			if errors.Is(err, gobreaker.ErrOpenState) {
				slog.Warn("nextjs scraper circuit breaker open, request rejected",
					slog.String("service", "nextjs-scraper"),
					slog.String("url", sourceURL),
					slog.String("state", n.circuitBreaker.State().String()))
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
func (n *NextJSScraper) doFetch(ctx context.Context, sourceURL string, config *entity.ScraperConfig) ([]fetch.FeedItem, error) {
	// Step 1: Validate URL (SSRF prevention)
	if err := validateURL(sourceURL); err != nil {
		return nil, fmt.Errorf("URL validation failed: %w", err)
	}

	// Step 2: Fetch HTML
	html, err := n.fetchHTML(ctx, sourceURL)
	if err != nil {
		return nil, fmt.Errorf("fetch HTML failed: %w", err)
	}

	// Step 3: Extract JSON from __NEXT_DATA__
	jsonData, err := n.extractJSON(html)
	if err != nil {
		return nil, fmt.Errorf("extract JSON failed: %w", err)
	}

	// Step 4: Parse items from JSON
	items, err := n.parseItems(jsonData, config)
	if err != nil {
		return nil, fmt.Errorf("parse items failed: %w", err)
	}

	if len(items) == 0 {
		return nil, errors.New("no items found in JSON data")
	}

	return items, nil
}

// fetchHTML fetches HTML from the given URL.
func (n *NextJSScraper) fetchHTML(ctx context.Context, urlStr string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "CatchUpFeedBot/1.0")

	resp, err := n.client.Do(req)
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

// extractJSON extracts and parses JSON from the __NEXT_DATA__ script tag.
func (n *NextJSScraper) extractJSON(html string) (map[string]interface{}, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	// Find __NEXT_DATA__ script tag
	var jsonText string
	doc.Find("script#__NEXT_DATA__").Each(func(i int, s *goquery.Selection) {
		jsonText = s.Text()
	})

	if jsonText == "" {
		return nil, errors.New("__NEXT_DATA__ script tag not found")
	}

	// Parse JSON
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonText), &data); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}

	return data, nil
}

// parseItems parses feed items from the Next.js JSON data structure.
func (n *NextJSScraper) parseItems(jsonData map[string]interface{}, config *entity.ScraperConfig) ([]fetch.FeedItem, error) {
	var items []fetch.FeedItem

	// Navigate to props.pageProps.initialSeedData.items
	props, ok := jsonData["props"].(map[string]interface{})
	if !ok {
		return nil, errors.New("props not found in JSON")
	}

	pageProps, ok := props["pageProps"].(map[string]interface{})
	if !ok {
		return nil, errors.New("pageProps not found in JSON")
	}

	// Use dataKey from config (default: "initialSeedData")
	dataKey := config.DataKey
	if dataKey == "" {
		dataKey = "initialSeedData"
	}

	seedData, ok := pageProps[dataKey].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("%s not found in pageProps", dataKey)
	}

	itemsArray, ok := seedData["items"].([]interface{})
	if !ok {
		return nil, errors.New("items array not found in seed data")
	}

	// Parse each item
	for i, itemData := range itemsArray {
		itemMap, ok := itemData.(map[string]interface{})
		if !ok {
			slog.Warn("skipping non-object item", slog.Int("index", i))
			continue
		}

		// Extract title
		title, _ := itemMap["title"].(string)
		if title == "" {
			slog.Debug("skipping item with empty title", slog.Int("index", i))
			continue
		}

		// Extract slug
		slug, _ := itemMap["slug"].(string)
		if slug == "" {
			slog.Debug("skipping item with empty slug", slog.Int("index", i), slog.String("title", title))
			continue
		}

		// Build URL from slug and prefix
		itemURL := makeAbsoluteURL(slug, config.URLPrefix)

		// Extract published date
		publishedStr, _ := itemMap["publishedOn"].(string)
		publishedAt := time.Now()
		if publishedStr != "" {
			if t, err := time.Parse(time.RFC3339, publishedStr); err == nil {
				publishedAt = t
			} else if t, err := time.Parse("2006-01-02", publishedStr); err == nil {
				publishedAt = t
			}
		}

		// Extract summary/content
		summary, _ := itemMap["summary"].(string)

		item := fetch.FeedItem{
			Title:       title,
			URL:         itemURL,
			Content:     summary,
			PublishedAt: publishedAt,
		}

		items = append(items, item)
	}

	return items, nil
}
