// Package article provides HTTP handlers for article-related endpoints.
// It includes handlers for creating, listing, searching, updating, and deleting articles.
package article

import "time"

// DTO represents the JSON structure for article data transfer.
type DTO struct {
	ID          int64     `json:"id" example:"1"`
	SourceID    int64     `json:"source_id" example:"1"`
	Title       string    `json:"title" example:"Go 1.23 リリース"`
	URL         string    `json:"url" example:"https://example.com/article/1"`
	Summary     string    `json:"summary" example:"Go 1.23 がリリースされました。新機能には..."`
	PublishedAt time.Time `json:"published_at" example:"2025-10-26T10:00:00Z"`
	CreatedAt   time.Time `json:"created_at" example:"2025-10-26T12:00:00Z"`
}
