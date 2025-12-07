package pagination

// Response is a generic paginated response wrapper.
// T is the type of data items (e.g., ArticleDTO, UserDTO, SourceDTO).
//
// Example usage:
//
//	type ArticleDTO struct { ... }
//	response := pagination.NewResponse(articles, metadata)
//	// response is of type pagination.Response[ArticleDTO]
type Response[T any] struct {
	Data       []T      `json:"data"`       // Array of data items for the current page
	Pagination Metadata `json:"pagination"` // Pagination metadata (total, page, limit, etc.)
}

// NewResponse creates a new paginated response with data and metadata.
// This is a convenience constructor for creating Response[T] instances.
func NewResponse[T any](data []T, metadata Metadata) Response[T] {
	return Response[T]{
		Data:       data,
		Pagination: metadata,
	}
}
