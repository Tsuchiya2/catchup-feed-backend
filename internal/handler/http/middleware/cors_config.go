package middleware

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// EnvConfigSource implements the ConfigSource interface by loading CORS configuration
// from environment variables. This is the V1 default implementation.
//
// Environment Variables:
//   - CORS_ALLOWED_ORIGINS: Comma-separated list of allowed origins (required)
//   - CORS_ALLOWED_METHODS: Comma-separated list of allowed HTTP methods (optional)
//   - CORS_ALLOWED_HEADERS: Comma-separated list of allowed request headers (optional)
//   - CORS_MAX_AGE: Preflight cache duration in seconds (optional)
//
// Example:
//
//	CORS_ALLOWED_ORIGINS=http://localhost:3000,https://example.com
//	CORS_ALLOWED_METHODS=GET,POST,PUT,DELETE
//	CORS_ALLOWED_HEADERS=Content-Type,Authorization
//	CORS_MAX_AGE=86400
type EnvConfigSource struct{}

// LoadOrigins loads the allowed origins from the CORS_ALLOWED_ORIGINS environment variable.
//
// Returns:
//   - A slice of allowed origin strings
//   - An error if CORS_ALLOWED_ORIGINS is not set or contains invalid URLs
//
// Validation:
//   - CORS_ALLOWED_ORIGINS must be set (fail-closed)
//   - Each origin must be a valid URL with http:// or https:// scheme
//   - Origins must not include trailing slashes
//   - Origins must not include paths, query strings, or fragments
func (s *EnvConfigSource) LoadOrigins() ([]string, error) {
	originsStr := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if originsStr == "" {
		return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS environment variable is required")
	}

	// Split by comma
	originList := strings.Split(originsStr, ",")
	origins := make([]string, 0, len(originList))

	for _, originStr := range originList {
		originStr = strings.TrimSpace(originStr)
		if originStr == "" {
			continue
		}

		// Validate origin format using url.Parse
		u, err := url.Parse(originStr)
		if err != nil {
			return nil, fmt.Errorf("invalid origin URL '%s': %w", originStr, err)
		}

		// Validate scheme
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("origin must use http or https scheme: %s", originStr)
		}

		// Validate no path, query, or fragment
		if u.Path != "" && u.Path != "/" {
			return nil, fmt.Errorf("origin must not include path: %s", originStr)
		}
		if u.RawQuery != "" {
			return nil, fmt.Errorf("origin must not include query string: %s", originStr)
		}
		if u.Fragment != "" {
			return nil, fmt.Errorf("origin must not include fragment: %s", originStr)
		}

		// Validate no trailing slash
		if strings.HasSuffix(originStr, "/") {
			return nil, fmt.Errorf("origin must not have trailing slash: %s", originStr)
		}

		origins = append(origins, originStr)
	}

	// Ensure at least one origin is configured
	if len(origins) == 0 {
		return nil, fmt.Errorf("at least one valid origin must be configured in CORS_ALLOWED_ORIGINS")
	}

	return origins, nil
}

// LoadMethods loads the allowed HTTP methods from the CORS_ALLOWED_METHODS environment variable.
//
// Returns:
//   - A slice of allowed HTTP method strings
//   - Default: ["GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"] if not configured
//   - An error if any method is invalid
//
// Validation:
//   - Methods must be valid HTTP verbs: GET, POST, PUT, DELETE, PATCH, OPTIONS
//   - Invalid methods cause an error
func (s *EnvConfigSource) LoadMethods() ([]string, error) {
	methodsStr := strings.TrimSpace(os.Getenv("CORS_ALLOWED_METHODS"))
	if methodsStr == "" {
		// Return default methods
		return []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}, nil
	}

	// Split by comma
	methodList := strings.Split(methodsStr, ",")
	methods := make([]string, 0, len(methodList))

	// Valid HTTP methods for CORS
	validMethods := map[string]bool{
		"GET":     true,
		"POST":    true,
		"PUT":     true,
		"DELETE":  true,
		"PATCH":   true,
		"OPTIONS": true,
	}

	for _, method := range methodList {
		method = strings.ToUpper(strings.TrimSpace(method))
		if method == "" {
			continue
		}

		// Validate method
		if !validMethods[method] {
			return nil, fmt.Errorf("invalid HTTP method '%s': must be one of GET, POST, PUT, DELETE, PATCH, OPTIONS", method)
		}

		methods = append(methods, method)
	}

	// Ensure at least one method is configured
	if len(methods) == 0 {
		return nil, fmt.Errorf("at least one valid HTTP method must be configured in CORS_ALLOWED_METHODS")
	}

	return methods, nil
}

// LoadHeaders loads the allowed request headers from the CORS_ALLOWED_HEADERS environment variable.
//
// Returns:
//   - A slice of allowed header names
//   - Default: ["Content-Type", "Authorization", "X-Request-ID"] if not configured
//   - An error if the configuration is invalid
func (s *EnvConfigSource) LoadHeaders() ([]string, error) {
	headersStr := strings.TrimSpace(os.Getenv("CORS_ALLOWED_HEADERS"))
	if headersStr == "" {
		// Return default headers
		return []string{"Content-Type", "Authorization", "X-Request-ID", "X-Trace-ID"}, nil
	}

	// Split by comma
	headerList := strings.Split(headersStr, ",")
	headers := make([]string, 0, len(headerList))

	for _, header := range headerList {
		header = strings.TrimSpace(header)
		if header == "" {
			continue
		}

		headers = append(headers, header)
	}

	// Ensure at least one header is configured
	if len(headers) == 0 {
		return nil, fmt.Errorf("at least one valid header must be configured in CORS_ALLOWED_HEADERS")
	}

	return headers, nil
}

// LoadMaxAge loads the preflight cache duration from the CORS_MAX_AGE environment variable.
//
// Returns:
//   - An integer representing the number of seconds browsers can cache preflight results
//   - Default: 86400 (24 hours) if not configured
//   - An error if the value is not a valid non-negative integer
func (s *EnvConfigSource) LoadMaxAge() (int, error) {
	maxAgeStr := strings.TrimSpace(os.Getenv("CORS_MAX_AGE"))
	if maxAgeStr == "" {
		// Return default: 24 hours
		return 86400, nil
	}

	// Parse as integer
	maxAge, err := strconv.Atoi(maxAgeStr)
	if err != nil {
		return 0, fmt.Errorf("invalid CORS_MAX_AGE '%s': must be a valid integer", maxAgeStr)
	}

	// Validate non-negative
	if maxAge < 0 {
		return 0, fmt.Errorf("CORS_MAX_AGE must be non-negative, got: %d", maxAge)
	}

	return maxAge, nil
}

// LoadCORSConfig loads CORS configuration from environment variables using EnvConfigSource.
// This is the V1 backward-compatible method.
//
// Returns:
//   - *CORSConfig: Loaded configuration with WhitelistValidator
//   - error: Non-nil if configuration is invalid or missing
//
// Usage:
//
//	config, err := middleware.LoadCORSConfig()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	config.Logger = &middleware.SlogAdapter{Logger: logger}
//	handler = middleware.CORS(*config)(handler)
//
// Note: Caller must inject Logger after loading (Logger is not set by this function)
func LoadCORSConfig() (*CORSConfig, error) {
	source := &EnvConfigSource{}
	return LoadCORSConfigFromSource(source, nil)
}

// LoadCORSConfigFromSource loads CORS configuration from a custom ConfigSource.
// This is the V2 extensible method that allows loading from different sources
// (environment variables, files, databases, remote services).
//
// Parameters:
//   - source: Implementation of ConfigSource interface
//   - logger: Implementation of CORSLogger interface (can be nil, caller can inject later)
//
// Returns:
//   - *CORSConfig: Loaded configuration with WhitelistValidator
//   - error: Non-nil if configuration is invalid or missing
//
// Usage:
//
//	// V1: Environment variables
//	source := &middleware.EnvConfigSource{}
//	config, err := middleware.LoadCORSConfigFromSource(source, logger)
//
//	// V2: File config (future)
//	source := &middleware.FileConfigSource{Path: "/etc/cors-config.yaml"}
//	config, err := middleware.LoadCORSConfigFromSource(source, logger)
func LoadCORSConfigFromSource(source ConfigSource, logger CORSLogger) (*CORSConfig, error) {
	// Load origins from source
	origins, err := source.LoadOrigins()
	if err != nil {
		return nil, fmt.Errorf("failed to load allowed origins: %w", err)
	}

	// Load methods from source
	methods, err := source.LoadMethods()
	if err != nil {
		return nil, fmt.Errorf("failed to load allowed methods: %w", err)
	}

	// Load headers from source
	headers, err := source.LoadHeaders()
	if err != nil {
		return nil, fmt.Errorf("failed to load allowed headers: %w", err)
	}

	// Load max age from source
	maxAge, err := source.LoadMaxAge()
	if err != nil {
		return nil, fmt.Errorf("failed to load max age: %w", err)
	}

	// Create WhitelistValidator with loaded origins
	validator := NewWhitelistValidator(origins)

	// Build CORSConfig
	config := &CORSConfig{
		AllowedOrigins:   origins, // Kept for backward compatibility
		AllowedMethods:   methods,
		AllowedHeaders:   headers,
		AllowCredentials: true, // Always true for JWT authentication
		MaxAge:           maxAge,
		Validator:        validator,
		Logger:           logger, // Can be nil, caller can inject later
	}

	return config, nil
}
