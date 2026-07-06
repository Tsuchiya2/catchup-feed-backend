package entity

import "time"

// DefaultSourceLang is the default language for sources (§4: lang text NOT
// NULL DEFAULT 'en').
const DefaultSourceLang = "en"

// Source kinds (Phase 2 §4: sources.kind — 'rss' | 'youtube' | 'podcast').
// YouTube channels and podcasts both expose RSS, so new-item detection
// shares the gofeed crawler; only content acquisition differs (transcribe
// on the Mac instead of go-readability on the Pi).
const (
	SourceKindRSS     = "rss"
	SourceKindYouTube = "youtube"
	SourceKindPodcast = "podcast"
)

// DefaultSourceKind is the default source kind (Phase 2 §4: kind text NOT
// NULL DEFAULT 'rss' — Phase 1 rows and requests stay fully compatible).
const DefaultSourceKind = SourceKindRSS

// ValidSourceKind reports whether kind is one of the three allowed values.
func ValidSourceKind(kind string) bool {
	switch kind {
	case SourceKindRSS, SourceKindYouTube, SourceKindPodcast:
		return true
	}
	return false
}

// Source represents a feed source in the pulse schema (§4).
// Sources are RSS/Atom feeds crawled with gofeed; the category drives the
// radio script corner assignment (§4: 台本のコーナー分けに使用).
// Kind selects the content pipeline (Phase 2 §5): 'rss' extracts content
// with go-readability, 'youtube'/'podcast' enqueue a transcribe job.
type Source struct {
	ID        int64
	Name      string
	FeedURL   string
	Category  string
	Lang      string
	Kind      string
	Active    bool
	CreatedAt time.Time
}

// Validate validates the Source entity fields against the pulse schema.
// Name, FeedURL and Category are NOT NULL; Lang defaults to 'en'; Kind
// defaults to 'rss' and must be one of rss|youtube|podcast (CHECK 制約).
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
	if s.Kind == "" {
		s.Kind = DefaultSourceKind
	}
	if !ValidSourceKind(s.Kind) {
		return &ValidationError{Field: "kind", Message: "must be one of rss, youtube, podcast"}
	}
	return nil
}
