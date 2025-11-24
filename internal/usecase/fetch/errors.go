// Package fetch provides use cases for crawling and fetching articles from RSS/Atom feeds.
// It implements business logic for fetching feed items, summarizing content with AI,
// and storing articles in the repository.
package fetch

import "errors"

// Sentinel errors for fetch use case operations.
var (
	// ErrFeedFetchFailed indicates that fetching a feed from the source URL failed.
	// This can occur due to network issues, invalid URLs, or server errors.
	ErrFeedFetchFailed = errors.New("failed to fetch feed from source")

	// ErrInvalidFeedFormat indicates that the feed content could not be parsed.
	// This typically happens when the feed is not valid RSS or Atom format.
	ErrInvalidFeedFormat = errors.New("invalid feed format")

	// ErrSummarizationFailed indicates that AI summarization of an article failed.
	// This can occur due to API errors, rate limits, or invalid content.
	ErrSummarizationFailed = errors.New("failed to summarize article content")
)
