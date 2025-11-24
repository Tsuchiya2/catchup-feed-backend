package scraper

import (
	"net/http"

	"catchup-feed/internal/usecase/fetch"
)

// ScraperFactory creates web scraper instances for different source types.
// It provides a centralized way to instantiate scrapers with consistent configuration.
type ScraperFactory struct {
	client *http.Client
}

// NewScraperFactory creates a new ScraperFactory with the given HTTP client.
// The HTTP client should be configured with appropriate timeouts and security settings.
func NewScraperFactory(client *http.Client) *ScraperFactory {
	return &ScraperFactory{client: client}
}

// CreateScrapers creates and returns a map of all available scrapers.
// The keys are source type names (e.g., "Webflow", "NextJS", "Remix")
// and the values are the corresponding FeedFetcher implementations.
//
// This map is used by the fetch service to route sources to the appropriate scraper.
func (f *ScraperFactory) CreateScrapers() map[string]fetch.FeedFetcher {
	return map[string]fetch.FeedFetcher{
		"Webflow": NewWebflowScraper(f.client),
		"NextJS":  NewNextJSScraper(f.client),
		"Remix":   NewRemixScraper(f.client),
	}
}
