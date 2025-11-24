// Package entity defines the core domain entities and validation logic for the application.
// It contains the fundamental business objects such as Article and Source, along with
// their validation rules and domain-specific errors.
package entity

import "time"

// Article represents a news article entity in the system.
// It contains the article's metadata, content summary, and relationships to sources.
type Article struct {
	ID          int64
	SourceID    int64
	Title       string
	URL         string
	Summary     string
	PublishedAt time.Time
	CreatedAt   time.Time
}
