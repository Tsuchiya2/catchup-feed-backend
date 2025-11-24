package pathutil

import (
	"errors"
	"strconv"
	"strings"
)

// ErrInvalidID is returned when the ID in the URL path is invalid.
var ErrInvalidID = errors.New("invalid id")

// ExtractID extracts and parses an integer ID from a URL path.
// It removes the specified prefix and attempts to parse the remaining string as an int64.
//
// Parameters:
//   - path: The full URL path (e.g., "/articles/123")
//   - prefix: The prefix to remove (e.g., "/articles/")
//
// Returns:
//   - int64: The parsed ID
//   - error: ErrInvalidID if the ID is invalid or <= 0
//
// Example:
//
//	id, err := ExtractID("/articles/123", "/articles/")
//	// Returns: 123, nil
func ExtractID(path, prefix string) (int64, error) {
	idStr := strings.TrimPrefix(path, prefix)
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrInvalidID
	}
	return id, nil
}
