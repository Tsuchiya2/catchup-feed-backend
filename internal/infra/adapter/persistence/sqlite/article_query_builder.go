// Package sqlite provides SQLite implementations of repository interfaces.
package sqlite

import (
	"strings"

	"catchup-feed/internal/repository"
)

// ArticleQueryBuilder builds WHERE clauses for article search.
// This builder is shared between COUNT and SELECT queries to eliminate duplication.
type ArticleQueryBuilder struct{}

// NewArticleQueryBuilder creates a new query builder instance.
func NewArticleQueryBuilder() *ArticleQueryBuilder {
	return &ArticleQueryBuilder{}
}

// BuildWhereClause builds WHERE clause and arguments for article search.
// It supports multi-keyword AND logic and optional filters (source_id, date range).
// Returns empty string if no conditions are provided.
func (qb *ArticleQueryBuilder) BuildWhereClause(keywords []string, filters repository.ArticleSearchFilters) (clause string, args []interface{}) {
	var conditions []string

	// Add keyword conditions (multi-keyword AND logic)
	// Each keyword searches in both title and summary
	for _, keyword := range keywords {
		likePattern := "%" + keyword + "%"
		conditions = append(conditions, "(title LIKE ? OR summary LIKE ?)")
		args = append(args, likePattern, likePattern)
	}

	// Add source ID filter
	if filters.SourceID != nil {
		conditions = append(conditions, "source_id = ?")
		args = append(args, *filters.SourceID)
	}

	// Add date range filters
	if filters.From != nil {
		conditions = append(conditions, "published_at >= ?")
		args = append(args, *filters.From)
	}
	if filters.To != nil {
		conditions = append(conditions, "published_at <= ?")
		args = append(args, *filters.To)
	}

	// Return empty if no conditions
	if len(conditions) == 0 {
		return "", args
	}

	// Join all conditions with AND
	return "WHERE " + strings.Join(conditions, " AND "), args
}
