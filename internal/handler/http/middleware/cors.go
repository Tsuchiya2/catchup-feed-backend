package middleware

import (
	"net/http"
	"strconv"
	"strings"
)

// CORSConfig holds the configuration for CORS middleware.
// It supports both V1 (environment variables) and V2 (extensible interfaces) configurations.
type CORSConfig struct {
	// AllowedOrigins is a whitelist of permitted origins.
	// DEPRECATED: Use Validator interface instead (kept for backward compatibility)
	// Example: ["http://localhost:3000", "https://example.com"]
	AllowedOrigins []string

	// AllowedMethods specifies which HTTP methods are allowed in CORS requests.
	// Configurable via CORS_ALLOWED_METHODS environment variable.
	// Default: ["GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"]
	AllowedMethods []string

	// AllowedHeaders specifies which request headers are allowed in CORS requests.
	// Configurable via CORS_ALLOWED_HEADERS environment variable.
	// Default: ["Content-Type", "Authorization", "X-Request-ID"]
	AllowedHeaders []string

	// AllowCredentials indicates whether credentials (cookies, authorization headers) are supported.
	// Must be true for JWT Bearer token authentication.
	AllowCredentials bool

	// MaxAge specifies how long preflight results can be cached (in seconds).
	// Configurable via CORS_MAX_AGE environment variable.
	// Default: 86400 (24 hours)
	MaxAge int

	// Validator is the origin validation strategy (V2 - extensibility).
	// Allows swapping between whitelist, pattern matching, IP validation, etc.
	Validator OriginValidator

	// Logger is the logging interface (V2 - dependency injection).
	// Allows testing with NoOpLogger and custom logging implementations.
	Logger CORSLogger
}

// CORS returns an HTTP middleware that handles CORS for cross-origin requests.
// It validates origins using the configured OriginValidator and sets appropriate
// CORS headers for allowed origins.
//
// Parameters:
//   - config: CORS policy configuration (origins, methods, headers, etc.)
//
// Returns:
//   - A middleware function that wraps http.Handler
//
// Example usage:
//
//	// V1: Load from environment variables
//	config, _ := middleware.LoadCORSConfig()
//	config.Logger = &middleware.SlogAdapter{Logger: logger}
//	handler = middleware.CORS(*config)(handler)
//
// Behavior:
//   - If Origin header is empty, skip CORS processing (same-origin request)
//   - If Origin is not allowed, log warning and continue without CORS headers
//   - If Origin is allowed and request is OPTIONS (preflight):
//   - Set Access-Control-Allow-Origin, Allow-Methods, Allow-Headers, Allow-Credentials, Max-Age
//   - Return 204 No Content (do not call next handler)
//   - If Origin is allowed and request is not OPTIONS (actual request):
//   - Set Access-Control-Allow-Origin, Allow-Credentials
//   - Pass request to next handler
func CORS(config CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract Origin header from request
			origin := r.Header.Get("Origin")

			// If Origin is empty, this is a same-origin request - skip CORS processing
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Validate Origin using the configured validator
			if !config.Validator.IsAllowed(origin) {
				// Log CORS policy violation
				if config.Logger != nil {
					config.Logger.Warn("CORS: origin not allowed", map[string]interface{}{
						"origin":      origin,
						"path":        r.URL.Path,
						"method":      r.Method,
						"remote_addr": r.RemoteAddr,
					})
				}

				// Do not set CORS headers for disallowed origins
				// Browser will block the response
				next.ServeHTTP(w, r)
				return
			}

			// Origin is allowed - set CORS headers
			// Echo back the request origin (required for credentials)
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			// Handle preflight OPTIONS request
			if r.Method == http.MethodOptions {
				// Set preflight-specific headers
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(config.AllowedMethods, ", "))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(config.AllowedHeaders, ", "))
				w.Header().Set("Access-Control-Max-Age", strconv.Itoa(config.MaxAge))

				// Log preflight request at DEBUG level
				if config.Logger != nil {
					config.Logger.Debug("CORS: preflight request", map[string]interface{}{
						"origin":            origin,
						"requested_method":  r.Header.Get("Access-Control-Request-Method"),
						"requested_headers": r.Header.Get("Access-Control-Request-Headers"),
					})
				}

				// Return 204 No Content for preflight (do not call next handler)
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Actual request - pass to next handler with CORS headers set
			next.ServeHTTP(w, r)
		})
	}
}
