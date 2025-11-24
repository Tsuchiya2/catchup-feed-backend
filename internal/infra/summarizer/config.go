package summarizer

import "fmt"

// SummarizerConfig is a common interface for summarizer configuration.
// Both OpenAI and Claude implementations should implement this interface
// to ensure consistent validation and configuration behavior.
type SummarizerConfig interface {
	// GetCharacterLimit returns the maximum number of characters allowed in a summary.
	// The limit should be within the valid range (100-5000).
	GetCharacterLimit() int

	// Validate validates the configuration and returns an error if invalid.
	// This should check all configuration fields for validity.
	Validate() error
}

const (
	// minCharLimit is the minimum allowed character limit for summaries.
	minCharLimit = 100

	// maxCharLimit is the maximum allowed character limit for summaries.
	maxCharLimit = 5000
)

// ValidateCharacterLimit validates that the character limit is within the valid range (100-5000).
// Returns an error if the limit is out of range with a descriptive message.
//
// Parameters:
//   - limit: The character limit to validate
//
// Returns:
//   - nil if the limit is valid
//   - error if the limit is outside the valid range
//
// Example:
//
//	err := ValidateCharacterLimit(900)  // nil (valid)
//	err := ValidateCharacterLimit(50)   // error: "character limit 50 is below minimum 100"
//	err := ValidateCharacterLimit(6000) // error: "character limit 6000 exceeds maximum 5000"
func ValidateCharacterLimit(limit int) error {
	if limit < minCharLimit {
		return fmt.Errorf("character limit %d is below minimum %d", limit, minCharLimit)
	}
	if limit > maxCharLimit {
		return fmt.Errorf("character limit %d exceeds maximum %d", limit, maxCharLimit)
	}
	return nil
}
