package entity

import (
	"errors"
	"fmt"
	"time"
)

// Source represents a news feed source in the system.
// It contains the feed URL, metadata, and crawling status information.
// For web scraping sources, it also includes the source type and configuration.
type Source struct {
	ID            int64
	Name          string
	FeedURL       string
	LastCrawledAt *time.Time
	Active        bool
	SourceType    string          `json:"source_type"`    // RSS, Webflow, NextJS, Remix
	ScraperConfig *ScraperConfig  `json:"scraper_config"` // Configuration for web scrapers
}

// ScraperConfig holds configuration for web scraping sources.
// Different fields are used depending on the source type:
// - Webflow: ItemSelector, TitleSelector, DateSelector, URLSelector, DateFormat
// - NextJS: DataKey, URLPrefix
// - Remix: ContextKey, URLPrefix
type ScraperConfig struct {
	// Webflow HTML selectors
	ItemSelector  string `json:"item_selector,omitempty"`
	TitleSelector string `json:"title_selector,omitempty"`
	DateSelector  string `json:"date_selector,omitempty"`
	URLSelector   string `json:"url_selector,omitempty"`
	DateFormat    string `json:"date_format,omitempty"`

	// Next.js JSON extraction
	DataKey string `json:"data_key,omitempty"`

	// Remix JSON extraction
	ContextKey string `json:"context_key,omitempty"`

	// Common
	URLPrefix string `json:"url_prefix,omitempty"` // Prepend to relative URLs
}

// Validate validates the Source entity fields.
// It checks that the source type is valid and that required configuration is present.
func (s *Source) Validate() error {
	// SourceTypeが空の場合はRSSとみなす（後方互換性）
	if s.SourceType == "" {
		s.SourceType = "RSS"
	}

	// SourceTypeの妥当性チェック
	validTypes := map[string]bool{
		"RSS":     true,
		"Webflow": true,
		"NextJS":  true,
		"Remix":   true,
	}
	if !validTypes[s.SourceType] {
		return fmt.Errorf("invalid source_type: %s (must be RSS, Webflow, NextJS, or Remix)", s.SourceType)
	}

	// 非RSSソースにはScraperConfigが必須
	if s.SourceType != "RSS" && s.ScraperConfig == nil {
		return errors.New("scraper_config is required for non-RSS sources")
	}

	return nil
}
