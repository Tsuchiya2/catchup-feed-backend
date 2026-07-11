// Package scraper provides implementations for fetching RSS/Atom feeds.
// It uses the gofeed library to parse feed content.
package scraper

import (
	"context"
	"net/http"
	"strings"
	"time"

	"catchup-feed/internal/infra/fetcher"
	"catchup-feed/internal/usecase/fetch"

	"github.com/mmcdole/gofeed"
)

// RSSFetcher implements FeedFetcher using the gofeed library.
type RSSFetcher struct {
	client *http.Client
}

// NewRSSFetcher creates a new RSSFetcher with the given HTTP client.
func NewRSSFetcher(client *http.Client) *RSSFetcher {
	return &RSSFetcher{
		client: client,
	}
}

// Fetch retrieves and parses an RSS/Atom feed from the given URL.
// Failures are returned as-is; the hourly cron simply retries on the next run.
// Returns a slice of FeedItem containing the parsed feed entries.
func (f *RSSFetcher) Fetch(ctx context.Context, feedURL string) ([]fetch.FeedItem, error) {
	return f.doFetch(ctx, feedURL)
}

// doFetch performs the actual feed fetch without retry or circuit breaker.
func (f *RSSFetcher) doFetch(ctx context.Context, feedURL string) ([]fetch.FeedItem, error) {
	fp := gofeed.NewParser()
	fp.UserAgent = fetcher.UserAgent
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
			Title:        it.Title,
			URL:          it.Link,
			Content:      content,
			PublishedAt:  pubAt,
			EnclosureURL: enclosureURL(it.Enclosures),
		})
	}

	return items, nil
}

// enclosureURL picks the media URL from the item enclosures (Phase 2 §5.2:
// podcast の media_url は enclosure の音声 URL). Audio enclosures win;
// otherwise the first video enclosure is used. Anything else (image/jpeg
// cover art etc.) is not media: such items return "" and the caller skips
// them (SkippedNoMedia).
func enclosureURL(encs []*gofeed.Enclosure) string {
	firstVideo := ""
	for _, enc := range encs {
		if enc == nil || enc.URL == "" {
			continue
		}
		if strings.HasPrefix(enc.Type, "audio/") {
			return enc.URL
		}
		if firstVideo == "" && strings.HasPrefix(enc.Type, "video/") {
			firstVideo = enc.URL
		}
	}
	return firstVideo
}
