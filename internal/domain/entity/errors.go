package entity

import (
	"errors"
	"fmt"
)

// Sentinel errors for domain layer operations.
var (
	// ErrNotFound indicates that a requested entity was not found
	ErrNotFound = errors.New("entity not found")

	// ErrInvalidInput indicates that the provided input is invalid
	ErrInvalidInput = errors.New("invalid input")

	// ErrValidationFailed indicates that validation checks have failed
	ErrValidationFailed = errors.New("validation failed")
)

// ValidationError represents a validation error with detailed field information.
// It implements the error interface and provides context about which field failed validation.
type ValidationError struct {
	Field   string
	Message string
}

// Error returns a formatted error message for the validation error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
}
