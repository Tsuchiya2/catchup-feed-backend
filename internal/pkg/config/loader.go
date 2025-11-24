package config

import (
	"fmt"
	"os"
	"time"
)

// ConfigLoadResult represents the result of loading a configuration value.
// It contains the loaded value, any warnings generated during loading,
// and a flag indicating whether a fallback value was used.
//
// This type is used by all ConfigLoader functions to provide consistent
// error handling and fallback behavior across different configuration types.
//
// Fields:
//   - Value: The loaded configuration value (may be fallback if validation failed)
//   - Warnings: List of warning messages (one per fallback applied)
//   - FallbackApplied: True if the default value was used due to validation failure
//
// Example:
//
//	result := LoadEnvDuration("TIMEOUT", 30*time.Minute, ValidatePositiveDuration)
//	if result.FallbackApplied {
//	    for _, warning := range result.Warnings {
//	        log.Warn("Configuration warning: %s", warning)
//	    }
//	}
//	timeout := result.Value.(time.Duration)
type ConfigLoadResult struct {
	Value           interface{}
	Warnings        []string
	FallbackApplied bool
}

// LoadEnvString loads a string value from an environment variable.
// If the environment variable is not set, the default value is returned.
// No validation is performed.
//
// This is a simple string loader without fallback logic, suitable for
// cases where any string value is acceptable (including empty strings
// if the default is empty).
//
// Parameters:
//   - envKey: Environment variable name to read
//   - defaultValue: Value to use if environment variable is not set
//
// Returns:
//   - string: The environment variable value, or default if not set
//
// Example:
//
//	schedule := LoadEnvString("CRON_SCHEDULE", "30 5 * * *")
//	// If CRON_SCHEDULE is not set, returns "30 5 * * *"
//	// If CRON_SCHEDULE="0 6 * * *", returns "0 6 * * *"
//
// Note: This function does NOT apply validation or fallback logic.
// Use LoadEnvWithFallback if validation is needed.
func LoadEnvString(envKey, defaultValue string) string {
	value := os.Getenv(envKey)
	if value == "" {
		return defaultValue
	}
	return value
}

// LoadEnvWithFallback loads a string value from an environment variable
// with validation and automatic fallback to default on validation failure.
//
// Loading behavior:
//  1. Read environment variable
//  2. If not set or empty: Use default value (no warning)
//  3. If set: Validate using provided validator
//  4. If validation fails: Use default value and generate warning
//
// This function never returns an error. It always returns a valid
// configuration value, either from the environment or the default.
// Validation failures result in warnings, not errors.
//
// Parameters:
//   - envKey: Environment variable name to read
//   - defaultValue: Value to use if variable not set or validation fails
//   - validator: Validation function (can be nil to skip validation)
//
// Returns:
//   - ConfigLoadResult: Contains the loaded value, warnings, and fallback flag
//
// Example:
//
//	result := LoadEnvWithFallback(
//	    "CRON_SCHEDULE",
//	    "30 5 * * *",
//	    ValidateCronSchedule,
//	)
//	if result.FallbackApplied {
//	    for _, warning := range result.Warnings {
//	        log.Warn("Configuration fallback: %s", warning)
//	    }
//	}
//	schedule := result.Value.(string)
//
// Warning format:
//
//	"Invalid {envKey}='{value}': {error}, falling back to default '{default}'"
//
// Use cases:
//   - Cron schedule loading with validation
//   - Timezone loading with validation
//   - Any string configuration requiring validation
func LoadEnvWithFallback(envKey, defaultValue string, validator func(string) error) ConfigLoadResult {
	value := os.Getenv(envKey)

	// If environment variable is not set or empty, use default (no warning)
	if value == "" {
		return ConfigLoadResult{
			Value:           defaultValue,
			Warnings:        nil,
			FallbackApplied: false,
		}
	}

	// If validator is provided, validate the value
	if validator != nil {
		if err := validator(value); err != nil {
			// Validation failed - use default and generate warning
			warning := fmt.Sprintf(
				"Invalid %s='%s': %v, falling back to default '%s'",
				envKey,
				value,
				err,
				defaultValue,
			)
			return ConfigLoadResult{
				Value:           defaultValue,
				Warnings:        []string{warning},
				FallbackApplied: true,
			}
		}
	}

	// Validation passed (or no validator) - use the environment value
	return ConfigLoadResult{
		Value:           value,
		Warnings:        nil,
		FallbackApplied: false,
	}
}

// LoadEnvDuration loads a duration value from an environment variable
// with parsing, validation, and automatic fallback to default on failure.
//
// Loading behavior:
//  1. Read environment variable
//  2. If not set or empty: Use default value (no warning)
//  3. If set: Parse using time.ParseDuration
//  4. If parsing fails: Use default value and generate warning
//  5. If parsing succeeds: Validate using provided validator
//  6. If validation fails: Use default value and generate warning
//
// This function never returns an error. It always returns a valid
// duration value, either from the environment or the default.
// Parsing and validation failures result in warnings, not errors.
//
// Parameters:
//   - envKey: Environment variable name to read
//   - defaultValue: Duration to use if variable not set or parsing/validation fails
//   - validator: Validation function (can be nil to skip validation)
//
// Returns:
//   - ConfigLoadResult: Contains the loaded duration, warnings, and fallback flag
//
// Example:
//
//	result := LoadEnvDuration(
//	    "CRAWL_TIMEOUT",
//	    30*time.Minute,
//	    ValidatePositiveDuration,
//	)
//	if result.FallbackApplied {
//	    for _, warning := range result.Warnings {
//	        log.Warn("Configuration fallback: %s", warning)
//	    }
//	}
//	timeout := result.Value.(time.Duration)
//
// Environment variable format:
//   - Go duration string: "30s", "5m", "1h30m", "2h", etc.
//   - Must be parseable by time.ParseDuration
//
// Warning formats:
//   - Parse error: "Invalid {envKey}='{value}': time: invalid duration, falling back to default '{default}'"
//   - Validation error: "Invalid {envKey}='{value}': {error}, falling back to default '{default}'"
//
// Use cases:
//   - Timeout configuration (with positive duration validation)
//   - Retry delay configuration (with range validation)
//   - Interval configuration (with range validation)
//   - Cache TTL configuration (with positive duration validation)
func LoadEnvDuration(envKey string, defaultValue time.Duration, validator func(time.Duration) error) ConfigLoadResult {
	valueStr := os.Getenv(envKey)

	// If environment variable is not set or empty, use default (no warning)
	if valueStr == "" {
		return ConfigLoadResult{
			Value:           defaultValue,
			Warnings:        nil,
			FallbackApplied: false,
		}
	}

	// Try to parse the duration
	parsedDuration, err := time.ParseDuration(valueStr)
	if err != nil {
		// Parsing failed - use default and generate warning
		warning := fmt.Sprintf(
			"Invalid %s='%s': %v, falling back to default '%v'",
			envKey,
			valueStr,
			err,
			defaultValue,
		)
		return ConfigLoadResult{
			Value:           defaultValue,
			Warnings:        []string{warning},
			FallbackApplied: true,
		}
	}

	// If validator is provided, validate the parsed duration
	if validator != nil {
		if err := validator(parsedDuration); err != nil {
			// Validation failed - use default and generate warning
			warning := fmt.Sprintf(
				"Invalid %s='%s': %v, falling back to default '%v'",
				envKey,
				valueStr,
				err,
				defaultValue,
			)
			return ConfigLoadResult{
				Value:           defaultValue,
				Warnings:        []string{warning},
				FallbackApplied: true,
			}
		}
	}

	// Parsing and validation passed - use the parsed duration
	return ConfigLoadResult{
		Value:           parsedDuration,
		Warnings:        nil,
		FallbackApplied: false,
	}
}

// LoadEnvInt loads an integer value from an environment variable
// with parsing, validation, and automatic fallback to default on failure.
//
// Loading behavior:
//  1. Read environment variable
//  2. If not set or empty: Use default value (no warning)
//  3. If set: Parse as integer using fmt.Sscanf
//  4. If parsing fails: Use default value and generate warning
//  5. If parsing succeeds: Validate using provided validator
//  6. If validation fails: Use default value and generate warning
//
// This function never returns an error. It always returns a valid
// integer value, either from the environment or the default.
// Parsing and validation failures result in warnings, not errors.
//
// Parameters:
//   - envKey: Environment variable name to read
//   - defaultValue: Integer to use if variable not set or parsing/validation fails
//   - validator: Validation function (can be nil to skip validation)
//
// Returns:
//   - ConfigLoadResult: Contains the loaded integer, warnings, and fallback flag
//
// Example:
//
//	result := LoadEnvInt(
//	    "MAX_RETRIES",
//	    3,
//	    func(v int) error { return ValidateIntRange(v, 0, 10) },
//	)
//	if result.FallbackApplied {
//	    for _, warning := range result.Warnings {
//	        log.Warn("Configuration fallback: %s", warning)
//	    }
//	}
//	maxRetries := result.Value.(int)
//
// Environment variable format:
//   - Integer string: "0", "10", "100", etc.
//   - Must not contain spaces, decimals, or other characters
//
// Warning formats:
//   - Parse error: "Invalid {envKey}='{value}': invalid integer format, falling back to default '{default}'"
//   - Validation error: "Invalid {envKey}='{value}': {error}, falling back to default '{default}'"
//
// Use cases:
//   - Port number configuration (with range validation)
//   - Parallelism configuration (with range validation)
//   - Retry attempt configuration (with range validation)
//   - Count/limit configuration (with range validation)
func LoadEnvInt(envKey string, defaultValue int, validator func(int) error) ConfigLoadResult {
	valueStr := os.Getenv(envKey)

	// If environment variable is not set or empty, use default (no warning)
	if valueStr == "" {
		return ConfigLoadResult{
			Value:           defaultValue,
			Warnings:        nil,
			FallbackApplied: false,
		}
	}

	// Try to parse the integer
	var parsedInt int
	_, err := fmt.Sscanf(valueStr, "%d", &parsedInt)
	if err != nil {
		// Parsing failed - use default and generate warning
		warning := fmt.Sprintf(
			"Invalid %s='%s': invalid integer format, falling back to default '%d'",
			envKey,
			valueStr,
			defaultValue,
		)
		return ConfigLoadResult{
			Value:           defaultValue,
			Warnings:        []string{warning},
			FallbackApplied: true,
		}
	}

	// If validator is provided, validate the parsed integer
	if validator != nil {
		if err := validator(parsedInt); err != nil {
			// Validation failed - use default and generate warning
			warning := fmt.Sprintf(
				"Invalid %s='%s': %v, falling back to default '%d'",
				envKey,
				valueStr,
				err,
				defaultValue,
			)
			return ConfigLoadResult{
				Value:           defaultValue,
				Warnings:        []string{warning},
				FallbackApplied: true,
			}
		}
	}

	// Parsing and validation passed - use the parsed integer
	return ConfigLoadResult{
		Value:           parsedInt,
		Warnings:        nil,
		FallbackApplied: false,
	}
}

// LoadEnvBool loads a boolean value from an environment variable
// with parsing and automatic fallback to default on failure.
//
// Loading behavior:
//  1. Read environment variable
//  2. If not set or empty: Use default value (no warning)
//  3. If set: Parse as boolean
//     - True values: "1", "t", "T", "true", "TRUE", "True"
//     - False values: "0", "f", "F", "false", "FALSE", "False"
//  4. If parsing fails: Use default value and generate warning
//
// This function never returns an error. It always returns a valid
// boolean value, either from the environment or the default.
// Parsing failures result in warnings, not errors.
//
// Parameters:
//   - envKey: Environment variable name to read
//   - defaultValue: Boolean to use if variable not set or parsing fails
//
// Returns:
//   - ConfigLoadResult: Contains the loaded boolean, warnings, and fallback flag
//
// Example:
//
//	result := LoadEnvBool("ENABLE_METRICS", true)
//	if result.FallbackApplied {
//	    for _, warning := range result.Warnings {
//	        log.Warn("Configuration fallback: %s", warning)
//	    }
//	}
//	enableMetrics := result.Value.(bool)
//
// Environment variable format:
//   - True: "1", "t", "T", "true", "TRUE", "True"
//   - False: "0", "f", "F", "false", "FALSE", "False"
//   - Other values will trigger fallback with warning
//
// Warning format:
//   - Parse error: "Invalid {envKey}='{value}': invalid boolean format, expected 'true' or 'false', falling back to default '{default}'"
//
// Use cases:
//   - Feature flags (enable/disable functionality)
//   - Debug mode configuration
//   - Dry-run mode configuration
//   - Toggle configuration
func LoadEnvBool(envKey string, defaultValue bool) ConfigLoadResult {
	valueStr := os.Getenv(envKey)

	// If environment variable is not set or empty, use default (no warning)
	if valueStr == "" {
		return ConfigLoadResult{
			Value:           defaultValue,
			Warnings:        nil,
			FallbackApplied: false,
		}
	}

	// Parse boolean value
	var parsedBool bool
	switch valueStr {
	case "1", "t", "T", "true", "TRUE", "True":
		parsedBool = true
	case "0", "f", "F", "false", "FALSE", "False":
		parsedBool = false
	default:
		// Parsing failed - use default and generate warning
		warning := fmt.Sprintf(
			"Invalid %s='%s': invalid boolean format, expected 'true' or 'false', falling back to default '%t'",
			envKey,
			valueStr,
			defaultValue,
		)
		return ConfigLoadResult{
			Value:           defaultValue,
			Warnings:        []string{warning},
			FallbackApplied: true,
		}
	}

	// Parsing passed - use the parsed boolean
	return ConfigLoadResult{
		Value:           parsedBool,
		Warnings:        nil,
		FallbackApplied: false,
	}
}
