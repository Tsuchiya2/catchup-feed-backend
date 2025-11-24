package auth

import "strings"

// PublicEndpoints defines endpoints that don't require authentication.
// These endpoints are accessible without a valid JWT token.
//
// Justification for each public endpoint:
// - /health, /ready, /live: Required for orchestration health checks (Kubernetes, Docker, monitoring)
// - /metrics: Required for Prometheus scraping (typically accessed by monitoring systems)
// - /swagger/: API documentation for developers
// - /auth/token: Token generation endpoint (can't require token to get token)
var PublicEndpoints = []string{
	"/health",
	"/ready",
	"/live",
	"/metrics",
	"/swagger/",
	"/auth/token",
}

// IsPublicEndpoint checks if a given path is a public endpoint.
// Public endpoints can be accessed without authentication.
//
// Matching logic:
// - Endpoints ending with '/' use prefix matching (e.g., /swagger/* matches /swagger/index.html)
// - Endpoints without '/' require exact match or query params only (e.g., /health matches /health?x=1 but not /health/detail)
//
// Example:
//
//	IsPublicEndpoint("/health")          // true
//	IsPublicEndpoint("/health?x=1")      // true (query params OK)
//	IsPublicEndpoint("/health/detail")   // false (subpath not allowed)
//	IsPublicEndpoint("/healthcheck")     // false (different endpoint)
//	IsPublicEndpoint("/swagger/index.html") // true (prefix match)
//	IsPublicEndpoint("/articles")        // false
//	IsPublicEndpoint("/sources")         // false
func IsPublicEndpoint(path string) bool {
	for _, endpoint := range PublicEndpoints {
		// Endpoints ending with '/' use prefix matching (for nested paths like /swagger/*)
		if strings.HasSuffix(endpoint, "/") {
			if strings.HasPrefix(path, endpoint) {
				return true
			}
			continue
		}

		// For endpoints without trailing '/', only allow exact match, trailing slash, or query params
		// This prevents /health from matching /health/detail or /healthcheck
		if path == endpoint {
			return true
		}
		// Allow trailing slash (e.g., /auth/token/ is same as /auth/token)
		if path == endpoint+"/" {
			return true
		}
		// Allow query parameters (e.g., /health?format=json)
		if strings.HasPrefix(path, endpoint+"?") {
			return true
		}
	}
	return false
}
