package pagination

// PaginationStrategy defines an interface for different pagination strategies.
// This enables future support for cursor-based, keyset-based, or other pagination methods
// without changing handler or service layer code.
type PaginationStrategy interface {
	// CalculateQuery returns the query parameters (offset, limit, cursor, etc.)
	// based on the pagination parameters.
	CalculateQuery(params Params) QueryParams

	// BuildMetadata constructs pagination metadata from query results.
	// The hasMore parameter is used for cursor-based pagination to indicate
	// if there are more results available.
	BuildMetadata(params Params, total int64, hasMore bool) Metadata
}

// QueryParams represents the calculated query parameters for database queries.
type QueryParams struct {
	Offset int     // For offset-based pagination
	Limit  int     // For all strategies
	Cursor *string // For cursor-based pagination (optional)
	After  *string // For keyset pagination (optional)
}

// OffsetStrategy implements traditional offset-based pagination.
// This is the current implementation used by the application.
type OffsetStrategy struct{}

// CalculateQuery calculates offset and limit for offset-based pagination.
func (s OffsetStrategy) CalculateQuery(params Params) QueryParams {
	return QueryParams{
		Offset: CalculateOffset(params.Page, params.Limit),
		Limit:  params.Limit,
		Cursor: nil,
		After:  nil,
	}
}

// BuildMetadata constructs standard pagination metadata for offset-based pagination.
func (s OffsetStrategy) BuildMetadata(params Params, total int64, hasMore bool) Metadata {
	return Metadata{
		Total:      total,
		Page:       params.Page,
		Limit:      params.Limit,
		TotalPages: CalculateTotalPages(total, params.Limit),
	}
}

// CursorStrategy is planned for future implementation.
// Design considerations documented in:
// - docs/designs/security-and-quality-improvements.md (Section 14)
//
// Future implementation will use:
// - Opaque cursors: base64(published_at + article_id)
// - No total count (performance optimization)
// - hasMore flag instead of total_pages
