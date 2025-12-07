package pagination

import (
	"fmt"
	"net/http"
	"strconv"
)

// Params represents pagination query parameters from an HTTP request.
type Params struct {
	Page  int // 1-based page number
	Limit int // Items per page
}

// ParseQueryParams parses pagination parameters from HTTP request query string.
// Returns Params with defaults if parameters are missing or invalid.
//
// Query parameters:
//   - page: Page number (must be positive integer)
//   - limit: Items per page (must be between 1 and config.MaxLimit)
//
// Returns an error if parameters are invalid.
func ParseQueryParams(r *http.Request, config Config) (Params, error) {
	params := Params{
		Page:  config.DefaultPage,
		Limit: config.DefaultLimit,
	}

	// Parse page parameter
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		page, err := strconv.Atoi(pageStr)
		if err != nil || page < 1 {
			return params, fmt.Errorf("invalid query parameter: page must be a positive integer")
		}
		params.Page = page
	}

	// Parse limit parameter
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 || limit > config.MaxLimit {
			return params, fmt.Errorf("invalid query parameter: limit must be between 1 and %d", config.MaxLimit)
		}
		params.Limit = limit
	}

	return params, nil
}
