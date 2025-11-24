// Package scraper provides implementations for fetching RSS/Atom feeds.
// It uses the gofeed library to parse feed content with reliability patterns.
package scraper

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"catchup-feed/internal/resilience/circuitbreaker"
	"catchup-feed/internal/resilience/retry"
	"catchup-feed/internal/usecase/fetch"

	"github.com/mmcdole/gofeed"
	"github.com/sony/gobreaker"
)

// RSSFetcher implements FeedFetcher using the gofeed library.
// It includes circuit breaker and retry logic for improved reliability.
type RSSFetcher struct {
	client         *http.Client
	circuitBreaker *circuitbreaker.CircuitBreaker
	retryConfig    retry.Config
}

// NewRSSFetcher creates a new RSSFetcher with the given HTTP client.
// It automatically configures circuit breaker and retry logic.
func NewRSSFetcher(client *http.Client) *RSSFetcher {
	return &RSSFetcher{
		client:         client,
		circuitBreaker: circuitbreaker.New(circuitbreaker.FeedFetchConfig()),
		retryConfig:    retry.FeedFetchConfig(),
	}
}

// Fetch retrieves and parses an RSS/Atom feed from the given URL.
// It uses circuit breaker and retry logic for improved reliability.
// Returns a slice of FeedItem containing the parsed feed entries.
func (f *RSSFetcher) Fetch(ctx context.Context, feedURL string) ([]fetch.FeedItem, error) {
	var items []fetch.FeedItem

	// Wrap with retry logic
	retryErr := retry.WithBackoff(ctx, f.retryConfig, func() error {
		// Execute through circuit breaker
		cbResult, err := f.circuitBreaker.Execute(func() (interface{}, error) {
			return f.doFetch(ctx, feedURL)
		})

		// Handle circuit breaker open state
		if err != nil {
			if errors.Is(err, gobreaker.ErrOpenState) {
				slog.Warn("feed fetch circuit breaker open, request rejected",
					slog.String("service", "feed-fetch"),
					slog.String("url", feedURL),
					slog.String("state", f.circuitBreaker.State().String()))
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

// doFetch performs the actual feed fetch without retry or circuit breaker.
func (f *RSSFetcher) doFetch(ctx context.Context, feedURL string) ([]fetch.FeedItem, error) {
	fp := gofeed.NewParser()
	fp.UserAgent = "CatchUpFeedBot"
	fp.Client = f.client

	feed, err := fp.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		return nil, err
	}

	items := make([]fetch.FeedItem, 0, len(feed.Items))
	for _, it := range feed.Items {
		pubAt := time.Now()
		if it.PublishedParsed != nil {
			pubAt = *it.PublishedParsed
		}

		// Content優先、なければDescriptionを使用
		content := it.Content
		if content == "" {
			content = it.Description
		}

		items = append(items, fetch.FeedItem{
			Title:       it.Title,
			URL:         it.Link,
			Content:     content,
			PublishedAt: pubAt,
		})
	}

	return items, nil
}
