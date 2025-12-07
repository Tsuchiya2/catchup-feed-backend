// Package pagination provides a reusable pagination framework with support
// for offset-based pagination and extensibility for future strategies.
package pagination

import (
	"os"
	"strconv"
)

// Config holds pagination configuration settings.
// These values can be loaded from environment variables or config files.
type Config struct {
	DefaultPage  int // Default page number (typically 1)
	DefaultLimit int // Default items per page (typically 20)
	MaxLimit     int // Maximum allowed items per page (typically 100)
}

// DefaultConfig returns the default pagination configuration.
// Default values: page=1, limit=20, max=100
func DefaultConfig() Config {
	return Config{
		DefaultPage:  1,
		DefaultLimit: 20,
		MaxLimit:     100,
	}
}

// LoadFromEnv loads pagination config from environment variables.
// Supported environment variables:
//   - PAGINATION_DEFAULT_PAGE: Default page number
//   - PAGINATION_DEFAULT_LIMIT: Default items per page
//   - PAGINATION_MAX_LIMIT: Maximum items per page
//
// Falls back to DefaultConfig() if environment variables are not set.
func LoadFromEnv() Config {
	return Config{
		DefaultPage:  getEnvAsInt("PAGINATION_DEFAULT_PAGE", 1),
		DefaultLimit: getEnvAsInt("PAGINATION_DEFAULT_LIMIT", 20),
		MaxLimit:     getEnvAsInt("PAGINATION_MAX_LIMIT", 100),
	}
}

// getEnvAsInt retrieves an environment variable and parses it as an integer.
// Returns the default value if the variable is not set or cannot be parsed.
func getEnvAsInt(key string, defaultValue int) int {
	valStr := os.Getenv(key)
	if valStr == "" {
		return defaultValue
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return defaultValue
	}
	return val
}
