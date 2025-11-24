package config

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// ValidateCronSchedule validates a cron expression using the robfig/cron/v3 parser.
// This function uses the standard cron parser to ensure the schedule is valid
// and can be properly parsed by the cron scheduler.
//
// The cron expression must follow the standard cron format:
//   - "minute hour day month weekday"
//   - Example: "30 5 * * *" (every day at 5:30)
//   - Example: "0 */6 * * *" (every 6 hours)
//   - Example: "30 9 * * 1-5" (weekdays at 9:30)
//
// Parameters:
//   - schedule: Cron expression to validate
//
// Returns:
//   - error: nil if valid, descriptive error otherwise
//
// Error messages include details about what went wrong, making them
// actionable for operators fixing configuration issues.
//
// Example:
//
//	err := ValidateCronSchedule("30 5 * * *")
//	if err != nil {
//	    log.Error("Invalid cron schedule: %v", err)
//	}
//
// Validation tool: https://crontab.guru/
func ValidateCronSchedule(schedule string) error {
	if schedule == "" {
		return fmt.Errorf("invalid cron schedule: cannot be empty")
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(schedule)
	if err != nil {
		return fmt.Errorf("invalid cron schedule '%s': %w", schedule, err)
	}

	return nil
}

// ValidateTimezone validates a timezone string by attempting to load it
// using the standard library time.LoadLocation function.
//
// The timezone must be a valid IANA timezone name:
//   - Example: "UTC"
//   - Example: "America/New_York"
//   - Example: "Europe/London"
//   - Example: "Asia/Tokyo"
//
// This validation checks if the timezone can be successfully loaded,
// which depends on the availability of timezone data in the system.
// If timezone data is not available (e.g., missing tzdata package),
// this validation may fail even for valid timezone names.
//
// Parameters:
//   - timezone: IANA timezone name to validate
//
// Returns:
//   - error: nil if valid and loadable, descriptive error otherwise
//
// Error messages include the timezone name and reason for failure,
// helping operators diagnose timezone data availability issues.
//
// Example:
//
//	err := ValidateTimezone("Asia/Tokyo")
//	if err != nil {
//	    log.Error("Invalid timezone: %v", err)
//	}
//
// Common issues:
//   - Missing tzdata package in Docker image
//   - Typo in timezone name
//   - Using UTC offset instead of IANA name (e.g., "+09:00" instead of "Asia/Tokyo")
//
// Timezone database: https://www.iana.org/time-zones
func ValidateTimezone(timezone string) error {
	if timezone == "" {
		return fmt.Errorf("invalid timezone: cannot be empty")
	}

	_, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone '%s': %w", timezone, err)
	}

	return nil
}

// ValidateDuration validates that a duration is within a specified range.
// This function checks both minimum and maximum bounds, ensuring the
// duration is not too short or too long.
//
// Validation rules:
//   - duration must be >= min (inclusive)
//   - duration must be <= max (inclusive)
//   - min must be <= max (checked internally)
//
// Parameters:
//   - duration: Duration value to validate
//   - min: Minimum allowed duration (inclusive)
//   - max: Maximum allowed duration (inclusive)
//
// Returns:
//   - error: nil if valid, descriptive error otherwise
//
// Error messages include the actual value and the valid range,
// helping operators understand the limits.
//
// Example:
//
//	// Validate timeout is between 1s and 1h
//	err := ValidateDuration(30*time.Minute, 1*time.Second, 1*time.Hour)
//	if err != nil {
//	    log.Error("Invalid duration: %v", err)
//	}
//
// Use cases:
//   - Timeout validation (must be between reasonable bounds)
//   - Retry delay validation (not too short, not too long)
//   - Interval validation (within acceptable range)
func ValidateDuration(duration, min, max time.Duration) error {
	if min > max {
		return fmt.Errorf("invalid range: min (%v) cannot be greater than max (%v)", min, max)
	}

	if duration < min {
		return fmt.Errorf("duration %v is below minimum %v", duration, min)
	}

	if duration > max {
		return fmt.Errorf("duration %v exceeds maximum %v", duration, max)
	}

	return nil
}

// ValidateIntRange validates that an integer value is within a specified range.
// This function checks both minimum and maximum bounds, ensuring the
// value is not too small or too large.
//
// Validation rules:
//   - value must be >= min (inclusive)
//   - value must be <= max (inclusive)
//   - min must be <= max (checked internally)
//
// Parameters:
//   - value: Integer value to validate
//   - min: Minimum allowed value (inclusive)
//   - max: Maximum allowed value (inclusive)
//
// Returns:
//   - error: nil if valid, descriptive error otherwise
//
// Error messages include the actual value and the valid range,
// helping operators understand the limits.
//
// Example:
//
//	// Validate parallelism is between 1 and 50
//	err := ValidateIntRange(10, 1, 50)
//	if err != nil {
//	    log.Error("Invalid parallelism: %v", err)
//	}
//
// Use cases:
//   - Parallelism validation (e.g., 1-50 concurrent operations)
//   - Port number validation (e.g., 1024-65535)
//   - Count validation (e.g., 0-1000 items)
//   - Retry attempt validation (e.g., 0-10 retries)
func ValidateIntRange(value, min, max int) error {
	if min > max {
		return fmt.Errorf("invalid range: min (%d) cannot be greater than max (%d)", min, max)
	}

	if value < min {
		return fmt.Errorf("value %d is below minimum %d", value, min)
	}

	if value > max {
		return fmt.Errorf("value %d exceeds maximum %d", value, max)
	}

	return nil
}

// ValidatePositiveDuration validates that a duration is positive (greater than zero).
// This is a common validation for timeouts, delays, and intervals that must
// have a non-zero, non-negative value.
//
// Validation rule:
//   - duration must be > 0 (strictly positive)
//
// Parameters:
//   - duration: Duration value to validate
//
// Returns:
//   - error: nil if positive, descriptive error otherwise
//
// Error messages include the actual value, making it clear what went wrong.
//
// Example:
//
//	err := ValidatePositiveDuration(30 * time.Minute)
//	if err != nil {
//	    log.Error("Invalid timeout: %v", err)
//	}
//
// Use cases:
//   - Timeout validation (must be positive)
//   - Retry delay validation (must be positive)
//   - Interval validation (must be positive)
//   - Cache TTL validation (must be positive)
//
// Common mistakes:
//   - Using zero duration (indicates infinite or disabled behavior)
//   - Using negative duration (invalid)
//
// This is equivalent to ValidateDuration(d, 1*time.Nanosecond, time.Duration(max int64))
// but with a clearer error message for the common case.
func ValidatePositiveDuration(duration time.Duration) error {
	if duration <= 0 {
		return fmt.Errorf("duration must be positive, got %v", duration)
	}

	return nil
}
