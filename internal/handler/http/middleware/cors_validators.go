package middleware

import (
	"strings"
)

// WhitelistValidator implements exact-match origin validation for CORS requests.
// It validates origins against a predefined whitelist using case-sensitive string comparison.
// This is the V1 implementation - V2 will add pattern matching via PatternValidator.
//
// Example usage:
//
//	validator := NewWhitelistValidator([]string{
//	    "http://localhost:3000",
//	    "https://example.com",
//	})
//	allowed := validator.IsAllowed("http://localhost:3000") // true
//	allowed = validator.IsAllowed("http://malicious.com")   // false
type WhitelistValidator struct {
	allowedOrigins []string
}

// NewWhitelistValidator creates a new WhitelistValidator with the given list of allowed origins.
//
// Parameters:
//   - origins: A slice of allowed origin strings (e.g., ["http://localhost:3000", "https://example.com"])
//
// Returns:
//   - A pointer to a new WhitelistValidator instance
//
// Implementation notes:
//   - Origins are normalized: converted to lowercase and trailing slashes removed
//   - Empty origins are filtered out
//   - Duplicate origins are preserved (no deduplication)
func NewWhitelistValidator(origins []string) *WhitelistValidator {
	// Normalize origins: lowercase and remove trailing slashes
	normalized := make([]string, 0, len(origins))
	for _, origin := range origins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		// Convert to lowercase for case-insensitive comparison
		origin = strings.ToLower(origin)
		// Remove trailing slash if present
		origin = strings.TrimSuffix(origin, "/")
		normalized = append(normalized, origin)
	}

	return &WhitelistValidator{
		allowedOrigins: normalized,
	}
}

// IsAllowed checks if the given origin is in the whitelist.
//
// Parameters:
//   - origin: The origin from the HTTP Origin header (e.g., "http://localhost:3000")
//
// Returns:
//   - true if the origin is in the whitelist
//   - false if the origin is not in the whitelist or is empty
//
// Implementation notes:
//   - Comparison is case-insensitive (origins are normalized to lowercase)
//   - Trailing slashes are removed before comparison
//   - Empty origins return false
//   - Time complexity: O(n) where n is the number of allowed origins
func (v *WhitelistValidator) IsAllowed(origin string) bool {
	if origin == "" {
		return false
	}

	// Normalize the origin for comparison
	origin = strings.ToLower(strings.TrimSpace(origin))
	origin = strings.TrimSuffix(origin, "/")

	// Check if origin is in the whitelist
	for _, allowed := range v.allowedOrigins {
		if origin == allowed {
			return true
		}
	}

	return false
}

// GetAllowedOrigins returns a defensive copy of the allowed origins list.
//
// Returns:
//   - A slice of allowed origin strings
//
// Implementation notes:
//   - Returns a copy to prevent external modification of internal state
//   - Origins are returned in normalized form (lowercase, no trailing slash)
func (v *WhitelistValidator) GetAllowedOrigins() []string {
	// Return a defensive copy
	copy := make([]string, len(v.allowedOrigins))
	for i, origin := range v.allowedOrigins {
		copy[i] = origin
	}
	return copy
}
