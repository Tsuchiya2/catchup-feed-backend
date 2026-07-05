package entity

import "time"

// DefaultSourceLang is the default language for sources (§4: lang text NOT
// NULL DEFAULT 'en').
const DefaultSourceLang = "en"

// Source represents a feed source in the pulse schema (§4).
// Sources are RSS/Atom feeds crawled with gofeed; the category drives the
// radio script corner assignment (§4: 台本のコーナー分けに使用).
type Source struct {
	ID        int64
	Name      string
	FeedURL   string
	Category  string
	Lang      string
	Active    bool
	CreatedAt time.Time
}

// Validate validates the Source entity fields against the pulse schema.
// Name, FeedURL and Category are NOT NULL; Lang defaults to 'en'.
func (s *Source) Validate() error {
	if s.Name == "" {
		return &ValidationError{Field: "name", Message: "is required"}
	}
	if s.FeedURL == "" {
		return &ValidationError{Field: "feedURL", Message: "is required"}
	}
	if s.Category == "" {
		return &ValidationError{Field: "category", Message: "is required"}
	}
	if s.Lang == "" {
		s.Lang = DefaultSourceLang
	}
	return nil
}
