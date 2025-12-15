package entity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSource_Struct(t *testing.T) {
	now := time.Now()

	source := Source{
		ID:            1,
		Name:          "Test Source",
		FeedURL:       "https://example.com/feed.xml",
		LastCrawledAt: &now,
		Active:        true,
	}

	assert.Equal(t, int64(1), source.ID)
	assert.Equal(t, "Test Source", source.Name)
	assert.Equal(t, "https://example.com/feed.xml", source.FeedURL)
	assert.Equal(t, &now, source.LastCrawledAt)
	assert.True(t, source.Active)
}

func TestSource_ZeroValue(t *testing.T) {
	var source Source

	assert.Equal(t, int64(0), source.ID)
	assert.Equal(t, "", source.Name)
	assert.Equal(t, "", source.FeedURL)
	assert.Nil(t, source.LastCrawledAt)
	assert.False(t, source.Active) // bool zero value is false
}

func TestSource_ActiveFlag(t *testing.T) {
	tests := []struct {
		name   string
		active bool
	}{
		{
			name:   "active source",
			active: true,
		},
		{
			name:   "inactive source",
			active: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := Source{
				Name:    "Test Source",
				FeedURL: "https://example.com/feed.xml",
				Active:  tt.active,
			}

			assert.Equal(t, tt.active, source.Active)
		})
	}
}

func TestSource_LastCrawledAt(t *testing.T) {
	t.Run("never crawled", func(t *testing.T) {
		source := Source{
			Name:    "New Source",
			FeedURL: "https://example.com/feed.xml",
		}

		assert.Nil(t, source.LastCrawledAt)
	})

	t.Run("recently crawled", func(t *testing.T) {
		crawledAt := time.Now().Add(-1 * time.Hour)
		source := Source{
			Name:          "Active Source",
			FeedURL:       "https://example.com/feed.xml",
			LastCrawledAt: &crawledAt,
		}

		assert.NotNil(t, source.LastCrawledAt)
		assert.True(t, source.LastCrawledAt.Before(time.Now()))
	})

	t.Run("crawled in the past", func(t *testing.T) {
		crawledAt := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		source := Source{
			Name:          "Old Source",
			FeedURL:       "https://example.com/feed.xml",
			LastCrawledAt: &crawledAt,
		}

		assert.Equal(t, &crawledAt, source.LastCrawledAt)
		assert.True(t, source.LastCrawledAt.Before(time.Now()))
	})
}

func TestSource_Comparison(t *testing.T) {
	now := time.Now()

	source1 := Source{
		ID:            1,
		Name:          "Source 1",
		FeedURL:       "https://example.com/feed1.xml",
		LastCrawledAt: &now,
		Active:        true,
	}

	source2 := Source{
		ID:            1,
		Name:          "Source 1",
		FeedURL:       "https://example.com/feed1.xml",
		LastCrawledAt: &now,
		Active:        true,
	}

	source3 := Source{
		ID:            2,
		Name:          "Source 2",
		FeedURL:       "https://example.com/feed2.xml",
		LastCrawledAt: &now,
		Active:        false,
	}

	// Same sources should be equal
	assert.Equal(t, source1, source2)

	// Different sources should not be equal
	assert.NotEqual(t, source1, source3)
}

func TestSource_Mutability(t *testing.T) {
	source := Source{
		ID:      1,
		Name:    "Original Name",
		FeedURL: "https://example.com/original.xml",
		Active:  true,
	}

	// Verify original values
	assert.Equal(t, "Original Name", source.Name)
	assert.Equal(t, "https://example.com/original.xml", source.FeedURL)
	assert.True(t, source.Active)

	// Modify fields
	source.Name = "Updated Name"
	source.FeedURL = "https://example.com/updated.xml"
	source.Active = false
	now := time.Now()
	source.LastCrawledAt = &now

	// Verify updated values
	assert.Equal(t, "Updated Name", source.Name)
	assert.Equal(t, "https://example.com/updated.xml", source.FeedURL)
	assert.False(t, source.Active)
	assert.NotNil(t, source.LastCrawledAt)
}

func TestSource_WithAllFields(t *testing.T) {
	crawledAt := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	source := Source{
		ID:            123,
		Name:          "Complete Source",
		FeedURL:       "https://example.com/complete.xml",
		LastCrawledAt: &crawledAt,
		Active:        true,
	}

	// Verify all fields are set correctly
	assert.NotZero(t, source.ID)
	assert.NotEmpty(t, source.Name)
	assert.NotEmpty(t, source.FeedURL)
	assert.NotNil(t, source.LastCrawledAt)
	assert.True(t, source.Active)

	// Verify exact values
	assert.Equal(t, int64(123), source.ID)
	assert.Equal(t, "Complete Source", source.Name)
	assert.Equal(t, "https://example.com/complete.xml", source.FeedURL)
	assert.Equal(t, &crawledAt, source.LastCrawledAt)
	assert.True(t, source.Active)
}

func TestSource_PartialInitialization(t *testing.T) {
	source := Source{
		Name:    "Partial Source",
		FeedURL: "https://example.com/partial.xml",
	}

	assert.Equal(t, int64(0), source.ID)
	assert.Equal(t, "Partial Source", source.Name)
	assert.Equal(t, "https://example.com/partial.xml", source.FeedURL)
	assert.Nil(t, source.LastCrawledAt)
	assert.False(t, source.Active)
}

func TestSource_RSSFeedURLs(t *testing.T) {
	tests := []struct {
		name    string
		feedURL string
	}{
		{
			name:    "RSS feed",
			feedURL: "https://example.com/rss.xml",
		},
		{
			name:    "Atom feed",
			feedURL: "https://example.com/atom.xml",
		},
		{
			name:    "feed without extension",
			feedURL: "https://example.com/feed",
		},
		{
			name:    "feed with query params",
			feedURL: "https://example.com/feed?format=rss",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := Source{
				Name:    "Test Source",
				FeedURL: tt.feedURL,
			}

			assert.Equal(t, tt.feedURL, source.FeedURL)
		})
	}
}

func TestSource_StateTransitions(t *testing.T) {
	// Test transitioning from inactive to active
	source := Source{
		Name:    "Test Source",
		FeedURL: "https://example.com/feed.xml",
		Active:  false,
	}

	assert.False(t, source.Active)

	// Activate source
	source.Active = true
	assert.True(t, source.Active)

	// Deactivate source
	source.Active = false
	assert.False(t, source.Active)
}

func TestSource_LongNames(t *testing.T) {
	// Test with very long name
	longName := string(make([]byte, 1000))
	longURL := "https://example.com/" + string(make([]byte, 500))

	source := Source{
		Name:    longName,
		FeedURL: longURL,
	}

	assert.Len(t, source.Name, 1000)
	assert.Greater(t, len(source.FeedURL), 500)
}

func TestSource_Validate(t *testing.T) {
	tests := []struct {
		name      string
		source    Source
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid RSS source with empty SourceType (defaults to RSS)",
			source: Source{
				Name:       "RSS Feed",
				FeedURL:    "https://example.com/rss.xml",
				SourceType: "",
			},
			wantError: false,
		},
		{
			name: "valid RSS source with explicit type",
			source: Source{
				Name:       "RSS Feed",
				FeedURL:    "https://example.com/rss.xml",
				SourceType: "RSS",
			},
			wantError: false,
		},
		{
			name: "valid Webflow source with scraper config",
			source: Source{
				Name:       "Webflow Site",
				FeedURL:    "https://example.webflow.io",
				SourceType: "Webflow",
				ScraperConfig: &ScraperConfig{
					ItemSelector:  ".blog-item",
					TitleSelector: "h2",
					DateSelector:  ".date",
					URLSelector:   "a",
					DateFormat:    "2006-01-02",
				},
			},
			wantError: false,
		},
		{
			name: "valid NextJS source with scraper config",
			source: Source{
				Name:       "NextJS Site",
				FeedURL:    "https://example.com/_next/data/builds.json",
				SourceType: "NextJS",
				ScraperConfig: &ScraperConfig{
					DataKey:   "posts",
					URLPrefix: "https://example.com",
				},
			},
			wantError: false,
		},
		{
			name: "valid Remix source with scraper config",
			source: Source{
				Name:       "Remix Site",
				FeedURL:    "https://example.com/posts",
				SourceType: "Remix",
				ScraperConfig: &ScraperConfig{
					ContextKey: "posts",
					URLPrefix:  "https://example.com",
				},
			},
			wantError: false,
		},
		{
			name: "invalid source type",
			source: Source{
				Name:       "Invalid Source",
				FeedURL:    "https://example.com",
				SourceType: "WordPress",
			},
			wantError: true,
			errorMsg:  "invalid source_type: WordPress (must be RSS, Webflow, NextJS, or Remix)",
		},
		{
			name: "Webflow source without scraper config",
			source: Source{
				Name:          "Webflow Site",
				FeedURL:       "https://example.webflow.io",
				SourceType:    "Webflow",
				ScraperConfig: nil,
			},
			wantError: true,
			errorMsg:  "scraper_config is required for non-RSS sources",
		},
		{
			name: "NextJS source without scraper config",
			source: Source{
				Name:          "NextJS Site",
				FeedURL:       "https://example.com",
				SourceType:    "NextJS",
				ScraperConfig: nil,
			},
			wantError: true,
			errorMsg:  "scraper_config is required for non-RSS sources",
		},
		{
			name: "Remix source without scraper config",
			source: Source{
				Name:          "Remix Site",
				FeedURL:       "https://example.com",
				SourceType:    "Remix",
				ScraperConfig: nil,
			},
			wantError: true,
			errorMsg:  "scraper_config is required for non-RSS sources",
		},
		{
			name: "RSS source with scraper config (should be allowed)",
			source: Source{
				Name:       "RSS Feed",
				FeedURL:    "https://example.com/rss.xml",
				SourceType: "RSS",
				ScraperConfig: &ScraperConfig{
					DataKey: "something",
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.source.Validate()

			if tt.wantError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Equal(t, tt.errorMsg, err.Error())
				}
			} else {
				assert.NoError(t, err)
				// When SourceType is empty, it should default to RSS
				if tt.source.SourceType == "" {
					// Note: Validate modifies the source
				}
			}
		})
	}
}

func TestSource_Validate_SourceTypeDefault(t *testing.T) {
	// Test that empty SourceType defaults to RSS
	source := Source{
		Name:       "Test Source",
		FeedURL:    "https://example.com/feed.xml",
		SourceType: "",
	}

	err := source.Validate()
	assert.NoError(t, err)
	assert.Equal(t, "RSS", source.SourceType)
}

func TestScraperConfig_WebflowFields(t *testing.T) {
	config := &ScraperConfig{
		ItemSelector:  ".blog-item",
		TitleSelector: "h2.title",
		DateSelector:  "span.date",
		URLSelector:   "a.link",
		DateFormat:    "2006-01-02",
	}

	assert.Equal(t, ".blog-item", config.ItemSelector)
	assert.Equal(t, "h2.title", config.TitleSelector)
	assert.Equal(t, "span.date", config.DateSelector)
	assert.Equal(t, "a.link", config.URLSelector)
	assert.Equal(t, "2006-01-02", config.DateFormat)
}

func TestScraperConfig_NextJSFields(t *testing.T) {
	config := &ScraperConfig{
		DataKey:   "blogPosts",
		URLPrefix: "https://example.com/blog",
	}

	assert.Equal(t, "blogPosts", config.DataKey)
	assert.Equal(t, "https://example.com/blog", config.URLPrefix)
}

func TestScraperConfig_RemixFields(t *testing.T) {
	config := &ScraperConfig{
		ContextKey: "posts",
		URLPrefix:  "https://example.com",
	}

	assert.Equal(t, "posts", config.ContextKey)
	assert.Equal(t, "https://example.com", config.URLPrefix)
}
