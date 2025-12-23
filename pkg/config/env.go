package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// GetEnvString returns the value of an environment variable or the default value if not set.
//
// This function does not perform validation and does not log warnings.
// It is suitable for simple string configuration values.
//
// Parameters:
//   - key: Environment variable name
//   - defaultValue: Value to return if the environment variable is not set or empty
//
// Returns:
//   - string: The environment variable value or defaultValue
//
// Example:
//
//	apiURL := GetEnvString("API_URL", "http://localhost:8080")
func GetEnvString(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// GetEnvInt returns the value of an environment variable as an integer.
//
// If the environment variable is not set, empty, or cannot be parsed as an integer,
// this function returns the default value and logs a warning.
//
// Parameters:
//   - key: Environment variable name
//   - defaultValue: Value to return on error or if not set
//
// Returns:
//   - int: The parsed integer value or defaultValue
//
// Example:
//
//	port := GetEnvInt("PORT", 8080)
func GetEnvInt(key string, defaultValue int) int {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}

	var value int
	_, err := fmt.Sscanf(valueStr, "%d", &value)
	if err != nil {
		slog.Warn("invalid integer value for environment variable, using default",
			slog.String("key", key),
			slog.String("value", valueStr),
			slog.Int("default", defaultValue),
			slog.String("error", err.Error()))
		return defaultValue
	}

	return value
}

// GetEnvBool returns the value of an environment variable as a boolean.
//
// Accepted true values: "1", "t", "T", "true", "TRUE", "True"
// Accepted false values: "0", "f", "F", "false", "FALSE", "False"
//
// If the environment variable is not set, empty, or has an invalid value,
// this function returns the default value and logs a warning.
//
// Parameters:
//   - key: Environment variable name
//   - defaultValue: Value to return on error or if not set
//
// Returns:
//   - bool: The parsed boolean value or defaultValue
//
// Example:
//
//	enabled := GetEnvBool("RATELIMIT_ENABLED", true)
func GetEnvBool(key string, defaultValue bool) bool {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}

	switch valueStr {
	case "1", "t", "T", "true", "TRUE", "True":
		return true
	case "0", "f", "F", "false", "FALSE", "False":
		return false
	default:
		slog.Warn("invalid boolean value for environment variable, using default",
			slog.String("key", key),
			slog.String("value", valueStr),
			slog.Bool("default", defaultValue))
		return defaultValue
	}
}

// GetEnvDuration returns the value of an environment variable as a time.Duration.
//
// The value must be parseable by time.ParseDuration (e.g., "1m", "30s", "1h30m").
//
// If the environment variable is not set, empty, or cannot be parsed,
// this function returns the default value and logs a warning.
//
// Parameters:
//   - key: Environment variable name
//   - defaultValue: Value to return on error or if not set
//
// Returns:
//   - time.Duration: The parsed duration value or defaultValue
//
// Example:
//
//	timeout := GetEnvDuration("TIMEOUT", 30*time.Second)
func GetEnvDuration(key string, defaultValue time.Duration) time.Duration {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}

	value, err := time.ParseDuration(valueStr)
	if err != nil {
		slog.Warn("invalid duration value for environment variable, using default",
			slog.String("key", key),
			slog.String("value", valueStr),
			slog.String("default", defaultValue.String()),
			slog.String("error", err.Error()))
		return defaultValue
	}

	return value
}

// GetEnvStringList returns a comma-separated list of strings from an environment variable.
//
// The values are trimmed of whitespace. Empty values are filtered out.
//
// If the environment variable is not set or empty, this function returns the default value.
//
// Parameters:
//   - key: Environment variable name
//   - defaultValue: Value to return if the environment variable is not set
//
// Returns:
//   - []string: The parsed list of strings or defaultValue
//
// Example:
//
//	proxies := GetEnvStringList("TRUSTED_PROXIES", []string{"10.0.0.0/8"})
//	// TRUSTED_PROXIES="10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16"
//	// Result: ["10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"]
func GetEnvStringList(key string, defaultValue []string) []string {
	valueStr := os.Getenv(key)
	if valueStr == "" {
		return defaultValue
	}

	parts := strings.Split(valueStr, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	if len(result) == 0 {
		return defaultValue
	}

	return result
}
