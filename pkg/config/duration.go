package config

import (
	"fmt"
	"time"
)

// ValidatePositiveDuration validates that a duration is positive (greater than zero).
//
// This is commonly used for timeout, interval, and window validation
// where a non-zero, positive value is required.
//
// Parameters:
//   - d: Duration to validate
//
// Returns:
//   - error: nil if valid, error otherwise
//
// Example:
//
//	if err := ValidatePositiveDuration(timeout); err != nil {
//	    return fmt.Errorf("invalid timeout: %w", err)
//	}
func ValidatePositiveDuration(d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("duration must be positive, got %v", d)
	}
	return nil
}

// ValidateDurationRange validates that a duration is within a specified range.
//
// The duration must be >= min and <= max (inclusive).
//
// Parameters:
//   - d: Duration to validate
//   - min: Minimum allowed duration (inclusive)
//   - max: Maximum allowed duration (inclusive)
//
// Returns:
//   - error: nil if valid, error otherwise
//
// Example:
//
//	// Validate cleanup interval is between 1 minute and 1 hour
//	if err := ValidateDurationRange(interval, 1*time.Minute, 1*time.Hour); err != nil {
//	    return fmt.Errorf("invalid cleanup interval: %w", err)
//	}
func ValidateDurationRange(d, min, max time.Duration) error {
	if min > max {
		return fmt.Errorf("invalid range: min (%v) cannot be greater than max (%v)", min, max)
	}

	if d < min {
		return fmt.Errorf("duration %v is below minimum %v", d, min)
	}

	if d > max {
		return fmt.Errorf("duration %v exceeds maximum %v", d, max)
	}

	return nil
}

// ValidateNonNegativeDuration validates that a duration is non-negative (>= 0).
//
// This is useful for optional timeouts or delays where zero is acceptable
// but negative values are not.
//
// Parameters:
//   - d: Duration to validate
//
// Returns:
//   - error: nil if valid, error otherwise
//
// Example:
//
//	if err := ValidateNonNegativeDuration(delay); err != nil {
//	    return fmt.Errorf("invalid delay: %w", err)
//	}
func ValidateNonNegativeDuration(d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("duration must be non-negative, got %v", d)
	}
	return nil
}
