package entity

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidationError_Error(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		message  string
		expected string
	}{
		{
			name:     "simple validation error",
			field:    "email",
			message:  "invalid format",
			expected: "validation error on field 'email': invalid format",
		},
		{
			name:     "required field error",
			field:    "username",
			message:  "required",
			expected: "validation error on field 'username': required",
		},
		{
			name:     "length validation error",
			field:    "password",
			message:  "must be at least 8 characters",
			expected: "validation error on field 'password': must be at least 8 characters",
		},
		{
			name:     "empty field name",
			field:    "",
			message:  "test message",
			expected: "validation error on field '': test message",
		},
		{
			name:     "empty message",
			field:    "test",
			message:  "",
			expected: "validation error on field 'test': ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ValidationError{
				Field:   tt.field,
				Message: tt.message,
			}

			assert.Equal(t, tt.expected, err.Error())
		})
	}
}

func TestValidationError_AsError(t *testing.T) {
	err := &ValidationError{
		Field:   "email",
		Message: "invalid format",
	}

	// ValidationError should implement error interface
	var _ error = err

	// Should be usable as error
	assert.Error(t, err)
}

func TestValidationError_IsError(t *testing.T) {
	err1 := &ValidationError{
		Field:   "email",
		Message: "invalid format",
	}

	err2 := &ValidationError{
		Field:   "email",
		Message: "invalid format",
	}

	err3 := &ValidationError{
		Field:   "username",
		Message: "required",
	}

	// Same validation errors should have the same error message
	assert.Equal(t, err1.Error(), err2.Error())

	// Different validation errors should have different error messages
	assert.NotEqual(t, err1.Error(), err3.Error())
}

func TestValidationError_WithErrors(t *testing.T) {
	err := &ValidationError{
		Field:   "email",
		Message: "invalid format",
	}

	// Should work with errors.Is (though it's not a sentinel error)
	assert.False(t, errors.Is(err, ErrValidationFailed))

	// Should work with errors.As
	var validationErr *ValidationError
	assert.True(t, errors.As(err, &validationErr))
	assert.Equal(t, "email", validationErr.Field)
	assert.Equal(t, "invalid format", validationErr.Message)
}

func TestSentinelErrors(t *testing.T) {
	// Test that sentinel errors are defined
	assert.NotNil(t, ErrNotFound)
	assert.NotNil(t, ErrInvalidInput)
	assert.NotNil(t, ErrValidationFailed)
}

func TestSentinelErrors_ErrorMessages(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "ErrNotFound",
			err:      ErrNotFound,
			expected: "entity not found",
		},
		{
			name:     "ErrInvalidInput",
			err:      ErrInvalidInput,
			expected: "invalid input",
		},
		{
			name:     "ErrValidationFailed",
			err:      ErrValidationFailed,
			expected: "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestSentinelErrors_WithErrorsIs(t *testing.T) {
	// Test errors.Is with sentinel errors
	assert.True(t, errors.Is(ErrNotFound, ErrNotFound))
	assert.False(t, errors.Is(ErrNotFound, ErrInvalidInput))
	assert.False(t, errors.Is(ErrNotFound, ErrValidationFailed))

	assert.True(t, errors.Is(ErrInvalidInput, ErrInvalidInput))
	assert.False(t, errors.Is(ErrInvalidInput, ErrNotFound))

	assert.True(t, errors.Is(ErrValidationFailed, ErrValidationFailed))
	assert.False(t, errors.Is(ErrValidationFailed, ErrNotFound))
}

func TestSentinelErrors_Uniqueness(t *testing.T) {
	// All sentinel errors should be unique
	assert.NotEqual(t, ErrNotFound, ErrInvalidInput)
	assert.NotEqual(t, ErrNotFound, ErrValidationFailed)
	assert.NotEqual(t, ErrInvalidInput, ErrValidationFailed)
}

func TestValidationError_MultipleFields(t *testing.T) {
	// Create multiple validation errors for different fields
	errors := []*ValidationError{
		{Field: "email", Message: "invalid format"},
		{Field: "username", Message: "too short"},
		{Field: "password", Message: "too weak"},
	}

	// Each error should have the correct field and message
	assert.Equal(t, "email", errors[0].Field)
	assert.Equal(t, "invalid format", errors[0].Message)

	assert.Equal(t, "username", errors[1].Field)
	assert.Equal(t, "too short", errors[1].Message)

	assert.Equal(t, "password", errors[2].Field)
	assert.Equal(t, "too weak", errors[2].Message)
}

func TestValidationError_InErrorChain(t *testing.T) {
	// Test using ValidationError in error wrapping
	baseErr := &ValidationError{
		Field:   "email",
		Message: "invalid format",
	}

	wrappedErr := errors.Join(ErrValidationFailed, baseErr)

	// Should be able to unwrap to get ValidationError
	var validationErr *ValidationError
	assert.True(t, errors.As(wrappedErr, &validationErr))
	assert.Equal(t, "email", validationErr.Field)

	// Should also match ErrValidationFailed
	assert.True(t, errors.Is(wrappedErr, ErrValidationFailed))
}

func TestValidationError_ZeroValue(t *testing.T) {
	var err ValidationError

	assert.Equal(t, "", err.Field)
	assert.Equal(t, "", err.Message)
	assert.Equal(t, "validation error on field '': ", err.Error())
}
