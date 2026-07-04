// Package entity defines the core domain entities and validation logic for the application.
// It contains the fundamental business objects such as Article and Source, along with
// their validation rules and domain-specific errors.
package entity

import "time"

// Article represents a crawled article in the pulse schema (§4).
//
// Content holds the go-readability extracted full text (articles.content,
// nullable in the schema; empty string when absent).
//
// Summary is NOT a column of the articles table: it is populated on reads
// from summaries.body via LEFT JOIN (empty string when no summary exists
// yet) and is ignored on writes. Persist summaries through
// repository.SummaryRepository instead.
type Article struct {
	ID          int64
	SourceID    int64
	Title       string
	URL         string
	Content     string
	Summary     string // read-only: joined from summaries.body
	PublishedAt time.Time
	CrawledAt   time.Time
}
