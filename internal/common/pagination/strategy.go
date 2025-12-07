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

// CursorStrategy implements cursor-based pagination (future enhancement).
// This is a placeholder for future implementation.
//
// TODO: Implement cursor-based pagination using opaque cursors (base64-encoded).
// Cursor format: base64(published_at + id) for stable pagination.
// Benefits: Better performance for large offsets, consistent results even with data changes.
type CursorStrategy struct{}

// CalculateQuery is not yet implemented for cursor-based pagination.
func (s CursorStrategy) CalculateQuery(params Params) QueryParams {
	// TODO: Parse cursor from params and calculate query parameters
	// Example: cursor = base64(published_at + id)
	return QueryParams{
		Offset: 0,
		Limit:  params.Limit,
		Cursor: nil, // Placeholder
		After:  nil,
	}
}

// BuildMetadata is not yet implemented for cursor-based pagination.
func (s CursorStrategy) BuildMetadata(params Params, total int64, hasMore bool) Metadata {
	// TODO: For cursor-based pagination, we don't return total count or total_pages
	// Instead, we return next cursor and hasMore flag
	return Metadata{
		Total:      -1, // Not applicable for cursor-based pagination
		Page:       -1, // Not applicable
		Limit:      params.Limit,
		TotalPages: -1, // Not applicable
		// NextCursor: calculateNextCursor(), // Future field
		// HasMore:    hasMore,
	}
}
