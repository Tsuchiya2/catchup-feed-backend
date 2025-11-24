package source

import "time"

type DTO struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	FeedURL       string     `json:"feed_url"`
	LastCrawledAt *time.Time `json:"last_crawled_at,omitempty"`
	Active        bool       `json:"active"`
}
