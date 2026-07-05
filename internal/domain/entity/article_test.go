package entity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestArticle_Fields pins the pulse schema shape (§4): articles carry
// content and crawled_at; the summary field is a read-only join value.
func TestArticle_Fields(t *testing.T) {
	publishedAt := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	crawledAt := time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)

	article := Article{
		ID:          123,
		SourceID:    456,
		Title:       "Complete Article",
		URL:         "https://example.com/complete",
		Content:     "full text extracted by go-readability",
		Summary:     "日本語要約",
		PublishedAt: publishedAt,
		CrawledAt:   crawledAt,
	}

	assert.Equal(t, int64(123), article.ID)
	assert.Equal(t, int64(456), article.SourceID)
	assert.Equal(t, "Complete Article", article.Title)
	assert.Equal(t, "https://example.com/complete", article.URL)
	assert.Equal(t, "full text extracted by go-readability", article.Content)
	assert.Equal(t, "日本語要約", article.Summary)
	assert.Equal(t, publishedAt, article.PublishedAt)
	assert.Equal(t, crawledAt, article.CrawledAt)
}

// TestArticle_ZeroValue documents that published_at may stay zero (the
// column is nullable in §4: feeds without dates).
func TestArticle_ZeroValue(t *testing.T) {
	var article Article

	assert.True(t, article.PublishedAt.IsZero())
	assert.True(t, article.CrawledAt.IsZero())
	assert.Empty(t, article.Content)
	assert.Empty(t, article.Summary)
}
