package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"catchup-feed/pkg/ratelimit"
	"catchup-feed/pkg/security/csp"
)

// CSPMiddlewareConfig holds configuration for CSP middleware.
// It supports path-based policy selection and report-only mode for testing.
type CSPMiddlewareConfig struct {
	// Enabled controls whether CSP headers are applied.
	// Can be toggled via environment variable for gradual rollout.
	// Default: true
	Enabled bool

	// DefaultPolicy is the default CSP policy to apply.
	// Used when no path-specific policy matches.
	DefaultPolicy *csp.CSPBuilder

	// PathPolicies maps path prefixes to specific CSP policies.
	// Example: map[string]*csp.CSPBuilder{
	//     "/swagger/": csp.SwaggerUIPolicy(),
	//     "/api/":     csp.StrictPolicy(),
	// }
	PathPolicies map[string]*csp.CSPBuilder

	// ReportOnly enables Content-Security-Policy-Report-Only mode.
	// In this mode, violations are reported but not enforced.
	// Useful for testing CSP policies before enforcement.
	// Default: false
	ReportOnly bool
}

// CSPMiddleware applies Content-Security-Policy headers to HTTP responses.
// It supports path-based policy selection and report-only mode.
type CSPMiddleware struct {
	config  CSPMiddlewareConfig
	metrics ratelimit.RateLimitMetrics // Reuse for CSP violation counting (if report endpoint configured)
}

// NewCSPMiddleware creates a new CSP middleware with the provided configuration.
//
// Parameters:
//   - config: CSP middleware configuration
//
// Returns:
//   - *CSPMiddleware: Configured CSP middleware instance
//
// Example:
//
//	cspMiddleware := NewCSPMiddleware(CSPMiddlewareConfig{
//	    Enabled: true,
//	    DefaultPolicy: csp.StrictPolicy(),
//	    PathPolicies: map[string]*csp.CSPBuilder{
//	        "/swagger/": csp.SwaggerUIPolicy(),
//	        "/docs/":    csp.RelaxedPolicy(),
//	    },
//	    ReportOnly: false,
//	})
//	handler = cspMiddleware.Middleware()(handler)
func NewCSPMiddleware(config CSPMiddlewareConfig) *CSPMiddleware {
	return &CSPMiddleware{
		config:  config,
		metrics: nil, // Optional: can be injected for violation metrics
	}
}

// Middleware returns an HTTP middleware handler that applies CSP headers.
//
// Behavior:
//   - If CSP is disabled, skip CSP processing
//   - Select appropriate policy based on request path:
//   - Check PathPolicies for matching path prefix
//   - Fall back to DefaultPolicy if no match
//   - Apply CSP header to response (Content-Security-Policy or Content-Security-Policy-Report-Only)
//   - Pass request to next handler
//
// Path Matching:
//   - Uses prefix matching (e.g., "/swagger/" matches "/swagger/index.html")
//   - Longest matching prefix wins if multiple matches exist
//   - Case-sensitive matching
//
// Example:
//
//	handler = cspMiddleware.Middleware()(handler)
func (m *CSPMiddleware) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if CSP is disabled
			if !m.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			// Select policy based on path
			policy := m.selectPolicy(r.URL.Path)

			// Skip if no policy is configured
			if policy == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Set report-only mode if configured
			if m.config.ReportOnly {
				policy = policy.ReportOnly(true)
			}

			// Build CSP header value
			cspValue := policy.Build()
			if cspValue == "" {
				// Empty policy, skip
				next.ServeHTTP(w, r)
				return
			}

			// Set appropriate CSP header
			headerName := policy.HeaderName()
			w.Header().Set(headerName, cspValue)

			// Log CSP header application (debug level)
			slog.Debug("CSP header applied",
				slog.String("path", r.URL.Path),
				slog.String("header", headerName),
				slog.String("policy", cspValue),
			)

			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// selectPolicy selects the appropriate CSP policy for the given path.
//
// Algorithm:
//  1. Check PathPolicies for matching path prefix
//  2. If multiple prefixes match, select the longest one (most specific)
//  3. If no prefix matches, return DefaultPolicy
//
// Parameters:
//   - path: Request URL path
//
// Returns:
//   - *csp.CSPBuilder: Selected policy, or nil if no policy configured
//
// Example:
//
//	PathPolicies: {
//	    "/api/":     csp.StrictPolicy(),
//	    "/swagger/": csp.SwaggerUIPolicy(),
//	}
//	"/swagger/index.html" → SwaggerUIPolicy
//	"/api/articles"       → StrictPolicy
//	"/health"             → DefaultPolicy
func (m *CSPMiddleware) selectPolicy(path string) *csp.CSPBuilder {
	// Find longest matching prefix
	longestPrefix := ""
	var matchedPolicy *csp.CSPBuilder

	for prefix, policy := range m.config.PathPolicies {
		if strings.HasPrefix(path, prefix) && len(prefix) > len(longestPrefix) {
			longestPrefix = prefix
			matchedPolicy = policy
		}
	}

	// Return matched policy or default
	if matchedPolicy != nil {
		return matchedPolicy
	}

	return m.config.DefaultPolicy
}

// WithMetrics sets the metrics recorder for CSP violation tracking.
// This is optional and only needed if CSP violation reporting is configured.
//
// Parameters:
//   - metrics: Metrics recorder interface
//
// Returns:
//   - *CSPMiddleware: The middleware instance for method chaining
//
// Example:
//
//	cspMiddleware.WithMetrics(prometheusMetrics)
func (m *CSPMiddleware) WithMetrics(metrics ratelimit.RateLimitMetrics) *CSPMiddleware {
	m.metrics = metrics
	return m
}

// ShouldApplyCSP checks if CSP should be applied to the given path.
// This is a utility function for testing purposes.
//
// Parameters:
//   - path: Request URL path
//   - applyToPaths: List of path patterns to apply CSP to
//
// Returns:
//   - bool: true if CSP should be applied, false otherwise
//
// Path Matching:
//   - Supports exact match and wildcard suffix matching
//   - Examples:
//   - "/swagger/" matches "/swagger/" exactly
//   - "/swagger/*" matches "/swagger/index.html", "/swagger/api/docs", etc.
//   - "/swagger" matches "/swagger" exactly (no trailing slash)
//
// Note: This function is for backward compatibility and testing.
// The main Middleware() method uses PathPolicies map for policy selection.
func ShouldApplyCSP(path string, applyToPaths []string) bool {
	for _, pattern := range applyToPaths {
		// Exact match
		if pattern == path {
			return true
		}

		// Wildcard match (pattern ends with /*)
		if strings.HasSuffix(pattern, "/*") {
			prefix := strings.TrimSuffix(pattern, "*")
			if strings.HasPrefix(path, prefix) {
				return true
			}
		}

		// Prefix match (pattern ends with /)
		if strings.HasSuffix(pattern, "/") {
			if strings.HasPrefix(path, pattern) {
				return true
			}
		}
	}

	return false
}
