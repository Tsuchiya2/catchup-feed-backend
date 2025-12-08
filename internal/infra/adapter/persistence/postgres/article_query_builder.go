// Package postgres provides PostgreSQL implementations of repository interfaces.
package postgres

import (
	"fmt"
	"strings"

	"catchup-feed/internal/pkg/search"
	"catchup-feed/internal/repository"
)

// ArticleQueryBuilder builds WHERE clauses for article search in PostgreSQL.
// This builder is shared between COUNT and SELECT queries to eliminate duplication.
// It uses PostgreSQL-specific features like ILIKE and numbered placeholders ($1, $2, etc.).
type ArticleQueryBuilder struct{}

// NewArticleQueryBuilder creates a new query builder instance.
func NewArticleQueryBuilder() *ArticleQueryBuilder {
	return &ArticleQueryBuilder{}
}

// BuildWhereClause builds WHERE clause and arguments for article search.
// It supports multi-keyword AND logic and optional filters (source_id, date range).
// Returns empty string if no conditions are provided.
// PostgreSQL-specific: Uses ILIKE for case-insensitive search and $N placeholders.
func (qb *ArticleQueryBuilder) BuildWhereClause(keywords []string, filters repository.ArticleSearchFilters, tableAlias string) (clause string, args []interface{}) {
	var conditions []string
	paramIndex := 1

	// Add keyword conditions (multi-keyword AND logic)
	// Each keyword searches in both title and summary using ILIKE (case-insensitive)
	for _, keyword := range keywords {
		// Escape special characters for ILIKE
		escapedKeyword := search.EscapeILIKE(keyword)

		// Build condition with table alias if provided
		var titleCol, summaryCol string
		if tableAlias != "" {
			titleCol = tableAlias + ".title"
			summaryCol = tableAlias + ".summary"
		} else {
			titleCol = "title"
			summaryCol = "summary"
		}

		conditions = append(conditions, fmt.Sprintf("(%s ILIKE $%d OR %s ILIKE $%d)", titleCol, paramIndex, summaryCol, paramIndex))
		args = append(args, escapedKeyword)
		paramIndex++
	}

	// Add source ID filter
	if filters.SourceID != nil {
		var col string
		if tableAlias != "" {
			col = tableAlias + ".source_id"
		} else {
			col = "source_id"
		}
		conditions = append(conditions, fmt.Sprintf("%s = $%d", col, paramIndex))
		args = append(args, *filters.SourceID)
		paramIndex++
	}

	// Add date range filters
	if filters.From != nil {
		var col string
		if tableAlias != "" {
			col = tableAlias + ".published_at"
		} else {
			col = "published_at"
		}
		conditions = append(conditions, fmt.Sprintf("%s >= $%d", col, paramIndex))
		args = append(args, *filters.From)
		paramIndex++
	}
	if filters.To != nil {
		var col string
		if tableAlias != "" {
			col = tableAlias + ".published_at"
		} else {
			col = "published_at"
		}
		conditions = append(conditions, fmt.Sprintf("%s <= $%d", col, paramIndex))
		args = append(args, *filters.To)
	}

	// Return empty if no conditions
	if len(conditions) == 0 {
		return "", args
	}

	// Join all conditions with AND
	return "WHERE " + strings.Join(conditions, " AND "), args
}
