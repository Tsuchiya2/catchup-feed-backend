package entity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestArticle_Struct(t *testing.T) {
	now := time.Now()

	article := Article{
		ID:          1,
		SourceID:    100,
		Title:       "Test Article",
		URL:         "https://example.com/article",
		Summary:     "This is a test article summary",
		PublishedAt: now,
		CreatedAt:   now,
	}

	assert.Equal(t, int64(1), article.ID)
	assert.Equal(t, int64(100), article.SourceID)
	assert.Equal(t, "Test Article", article.Title)
	assert.Equal(t, "https://example.com/article", article.URL)
	assert.Equal(t, "This is a test article summary", article.Summary)
	assert.Equal(t, now, article.PublishedAt)
	assert.Equal(t, now, article.CreatedAt)
}

func TestArticle_ZeroValue(t *testing.T) {
	var article Article

	assert.Equal(t, int64(0), article.ID)
	assert.Equal(t, int64(0), article.SourceID)
	assert.Equal(t, "", article.Title)
	assert.Equal(t, "", article.URL)
	assert.Equal(t, "", article.Summary)
	assert.True(t, article.PublishedAt.IsZero())
	assert.True(t, article.CreatedAt.IsZero())
}

func TestArticle_PartialInitialization(t *testing.T) {
	article := Article{
		Title: "Partial Article",
		URL:   "https://example.com/partial",
	}

	assert.Equal(t, int64(0), article.ID)
	assert.Equal(t, int64(0), article.SourceID)
	assert.Equal(t, "Partial Article", article.Title)
	assert.Equal(t, "https://example.com/partial", article.URL)
	assert.Equal(t, "", article.Summary)
	assert.True(t, article.PublishedAt.IsZero())
	assert.True(t, article.CreatedAt.IsZero())
}

func TestArticle_WithAllFields(t *testing.T) {
	publishedAt := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	createdAt := time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC)

	article := Article{
		ID:          123,
		SourceID:    456,
		Title:       "Complete Article",
		URL:         "https://example.com/complete",
		Summary:     "A complete article with all fields populated",
		PublishedAt: publishedAt,
		CreatedAt:   createdAt,
	}

	// Verify all fields are set correctly
	assert.NotZero(t, article.ID)
	assert.NotZero(t, article.SourceID)
	assert.NotEmpty(t, article.Title)
	assert.NotEmpty(t, article.URL)
	assert.NotEmpty(t, article.Summary)
	assert.False(t, article.PublishedAt.IsZero())
	assert.False(t, article.CreatedAt.IsZero())

	// Verify exact values
	assert.Equal(t, int64(123), article.ID)
	assert.Equal(t, int64(456), article.SourceID)
	assert.Equal(t, "Complete Article", article.Title)
	assert.Equal(t, "https://example.com/complete", article.URL)
	assert.Equal(t, "A complete article with all fields populated", article.Summary)
	assert.Equal(t, publishedAt, article.PublishedAt)
	assert.Equal(t, createdAt, article.CreatedAt)
}

func TestArticle_Comparison(t *testing.T) {
	now := time.Now()

	article1 := Article{
		ID:          1,
		SourceID:    100,
		Title:       "Article 1",
		URL:         "https://example.com/1",
		Summary:     "Summary 1",
		PublishedAt: now,
		CreatedAt:   now,
	}

	article2 := Article{
		ID:          1,
		SourceID:    100,
		Title:       "Article 1",
		URL:         "https://example.com/1",
		Summary:     "Summary 1",
		PublishedAt: now,
		CreatedAt:   now,
	}

	article3 := Article{
		ID:          2,
		SourceID:    100,
		Title:       "Article 2",
		URL:         "https://example.com/2",
		Summary:     "Summary 2",
		PublishedAt: now,
		CreatedAt:   now,
	}

	// Same articles should be equal
	assert.Equal(t, article1, article2)

	// Different articles should not be equal
	assert.NotEqual(t, article1, article3)
}

func TestArticle_Mutability(t *testing.T) {
	article := Article{
		ID:    1,
		Title: "Original Title",
		URL:   "https://example.com/original",
	}

	// Verify original values
	assert.Equal(t, "Original Title", article.Title)
	assert.Equal(t, "https://example.com/original", article.URL)

	// Modify fields
	article.Title = "Updated Title"
	article.URL = "https://example.com/updated"
	article.Summary = "New summary"

	// Verify updated values
	assert.Equal(t, "Updated Title", article.Title)
	assert.Equal(t, "https://example.com/updated", article.URL)
	assert.Equal(t, "New summary", article.Summary)
}

func TestArticle_TimeFields(t *testing.T) {
	// Test with specific times
	publishedAt := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	createdAt := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)

	article := Article{
		PublishedAt: publishedAt,
		CreatedAt:   createdAt,
	}

	// CreatedAt should be after PublishedAt in this example
	assert.True(t, article.CreatedAt.After(article.PublishedAt))

	// Both should be in the past
	assert.True(t, article.PublishedAt.Before(time.Now()))
	assert.True(t, article.CreatedAt.Before(time.Now()))
}

func TestArticle_LongContent(t *testing.T) {
	// Test with very long content
	longTitle := string(make([]byte, 1000))
	longURL := "https://example.com/" + string(make([]byte, 500))
	longSummary := string(make([]byte, 5000))

	article := Article{
		Title:   longTitle,
		URL:     longURL,
		Summary: longSummary,
	}

	// Verify that long content is stored correctly
	assert.Len(t, article.Title, 1000)
	assert.Greater(t, len(article.URL), 500)
	assert.Len(t, article.Summary, 5000)
}
