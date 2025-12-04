package middleware

// OriginValidator is an interface for validating allowed origins in CORS requests.
// It provides an abstraction layer for different origin validation strategies,
// allowing the application to choose between exact-match whitelisting (V1),
// pattern matching (V2 - future), IP-based validation (V2 - future), or composite validators.
//
// Example usage:
//
//	// V1: Exact match whitelist
//	validator := NewWhitelistValidator([]string{"http://localhost:3000", "https://example.com"})
//	allowed := validator.IsAllowed("http://localhost:3000") // true
//
//	// V2: Pattern matching (future)
//	validator := NewPatternValidator([]string{"https://*.vercel.app"})
//	allowed := validator.IsAllowed("https://my-app.vercel.app") // true
type OriginValidator interface {
	// IsAllowed checks if the given origin is permitted for CORS requests.
	//
	// Parameters:
	//   - origin: The origin from the HTTP Origin header (e.g., "http://localhost:3000")
	//
	// Returns:
	//   - true if the origin is allowed
	//   - false if the origin is not allowed or invalid
	//
	// Implementation notes:
	//   - The origin should be compared in a case-sensitive manner
	//   - The origin should not include trailing slashes
	//   - Empty origins should return false
	IsAllowed(origin string) bool

	// GetAllowedOrigins returns the list of allowed origins for logging and debugging.
	//
	// Returns:
	//   - A slice of allowed origin strings
	//   - For pattern-based validators, this may return patterns instead of exact origins
	//   - For security, this should return a defensive copy, not a reference to internal state
	//
	// Example:
	//   - WhitelistValidator: ["http://localhost:3000", "https://example.com"]
	//   - PatternValidator: ["https://*.vercel.app", "http://localhost:*"]
	GetAllowedOrigins() []string
}

// ConfigSource is an interface for loading CORS configuration from various sources.
// It provides an abstraction layer for configuration storage, allowing the application
// to load from environment variables (V1), files (V2), databases (V2), or remote services (V2).
//
// Example usage:
//
//	// V1: Load from environment variables
//	source := &EnvConfigSource{}
//	config, err := LoadCORSConfigFromSource(source, logger)
//
//	// V2: Load from YAML file (future)
//	source := &FileConfigSource{Path: "/etc/cors-config.yaml"}
//	config, err := LoadCORSConfigFromSource(source, logger)
type ConfigSource interface {
	// LoadOrigins loads the list of allowed origins from the configuration source.
	//
	// Returns:
	//   - A slice of allowed origin strings (e.g., ["http://localhost:3000", "https://example.com"])
	//   - An error if the configuration is invalid or missing
	//
	// Validation requirements:
	//   - At least one origin must be configured (fail-closed)
	//   - Each origin must be a valid URL with http:// or https:// scheme
	//   - Origins must not include trailing slashes
	//   - Invalid origins should cause the loader to return an error
	LoadOrigins() ([]string, error)

	// LoadMethods loads the list of allowed HTTP methods from the configuration source.
	//
	// Returns:
	//   - A slice of allowed HTTP method strings (e.g., ["GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"])
	//   - If not configured, returns default: ["GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"]
	//   - An error if the configuration is invalid
	//
	// Validation requirements:
	//   - Methods must be valid HTTP verbs (GET, POST, PUT, DELETE, PATCH, OPTIONS)
	//   - Invalid methods should cause the loader to return an error
	LoadMethods() ([]string, error)

	// LoadHeaders loads the list of allowed request headers from the configuration source.
	//
	// Returns:
	//   - A slice of allowed header names (e.g., ["Content-Type", "Authorization", "X-Request-ID"])
	//   - If not configured, returns default: ["Content-Type", "Authorization", "X-Request-ID"]
	//   - An error if the configuration is invalid
	//
	// Implementation notes:
	//   - Header names should be case-insensitive
	//   - Browsers will normalize header names, so "content-type" and "Content-Type" are equivalent
	LoadHeaders() ([]string, error)

	// LoadMaxAge loads the preflight cache duration (in seconds) from the configuration source.
	//
	// Returns:
	//   - An integer representing the number of seconds browsers can cache preflight results
	//   - If not configured, returns default: 86400 (24 hours)
	//   - An error if the configuration is invalid (negative value, non-numeric)
	//
	// Implementation notes:
	//   - MaxAge must be non-negative
	//   - Typical values: 3600 (1 hour), 86400 (24 hours)
	//   - Setting to 0 disables preflight caching
	LoadMaxAge() (int, error)
}

// CORSLogger is an interface for logging CORS-related events.
// It provides an abstraction layer for logging implementations, allowing the application
// to inject custom loggers for testing or use different logging backends.
//
// Example usage:
//
//	// Production: Use slog adapter
//	logger := &SlogAdapter{Logger: slog.Default()}
//	logger.Warn("CORS: origin not allowed", map[string]interface{}{
//	    "origin": "http://malicious.com",
//	    "path": "/api/sources",
//	})
//
//	// Testing: Use NoOpLogger
//	logger := &NoOpLogger{}
//	logger.Warn("test message", nil) // no-op
type CORSLogger interface {
	// Info logs informational messages about CORS operations.
	//
	// Parameters:
	//   - msg: The log message
	//   - fields: A map of structured log fields (e.g., origin, path, method)
	//
	// Usage:
	//   - Startup configuration logging
	//   - Normal CORS request processing (at DEBUG level, not INFO)
	Info(msg string, fields map[string]interface{})

	// Warn logs warning messages about CORS policy violations.
	//
	// Parameters:
	//   - msg: The log message
	//   - fields: A map of structured log fields (e.g., origin, path, method, request_id)
	//
	// Usage:
	//   - Origin validation failures
	//   - Untrusted proxy attempts to set CORS headers
	//   - Malformed Origin headers
	Warn(msg string, fields map[string]interface{})

	// Debug logs debug messages about CORS request processing.
	//
	// Parameters:
	//   - msg: The log message
	//   - fields: A map of structured log fields (e.g., origin, requested_method, requested_headers)
	//
	// Usage:
	//   - Preflight request processing
	//   - Successful CORS request handling
	//   - Detailed CORS header information
	Debug(msg string, fields map[string]interface{})
}
