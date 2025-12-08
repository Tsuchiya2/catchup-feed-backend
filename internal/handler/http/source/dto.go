package source

import "time"

type DTO struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	FeedURL       string     `json:"feed_url"`
	URL           string     `json:"url"` // Mapped from FeedURL for frontend compatibility
	SourceType    string     `json:"source_type"`
	LastCrawledAt *time.Time `json:"last_crawled_at,omitempty"`
	Active        bool       `json:"active"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
