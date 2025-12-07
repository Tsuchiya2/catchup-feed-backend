package pagination

// Metadata contains pagination metadata included in API responses.
type Metadata struct {
	Total      int64 `json:"total"`       // Total number of items across all pages
	Page       int   `json:"page"`        // Current page number (1-based)
	Limit      int   `json:"limit"`       // Items per page
	TotalPages int   `json:"total_pages"` // Calculated total number of pages
}
