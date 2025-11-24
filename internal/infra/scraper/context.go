package scraper

import "catchup-feed/internal/domain/entity"

// ContextKey is the type for context keys used in scrapers.
// Exported for use in tests.
type ContextKey string

// ScraperConfigKey is the context key for ScraperConfig.
const ScraperConfigKey ContextKey = "scraper_config"

// GetScraperConfig extracts ScraperConfig from context.
// Returns nil if not found or invalid type.
func GetScraperConfig(ctx interface{}) *entity.ScraperConfig {
	if ctx == nil {
		return nil
	}

	// Try to extract using the context.Context interface
	type valueGetter interface {
		Value(key interface{}) interface{}
	}

	vg, ok := ctx.(valueGetter)
	if !ok {
		return nil
	}

	config, ok := vg.Value(ScraperConfigKey).(*entity.ScraperConfig)
	if !ok {
		return nil
	}

	return config
}
