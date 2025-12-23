package csp

import (
	"fmt"
	"strings"
)

// CSPBuilder provides a fluent interface for constructing Content-Security-Policy headers.
//
// CSP (Content Security Policy) is a security standard that helps prevent cross-site scripting (XSS),
// clickjacking, and other code injection attacks by specifying which sources are trusted for
// loading content.
//
// Example Usage:
//
//	policy := NewCSPBuilder().
//	    DefaultSrc("'self'").
//	    ScriptSrc("'self'", "https://cdn.example.com").
//	    StyleSrc("'self'", "'unsafe-inline'").
//	    Build()
//	// Returns: "default-src 'self'; script-src 'self' https://cdn.example.com; style-src 'self' 'unsafe-inline'"
//
// Thread Safety: CSPBuilder is not thread-safe. Create separate instances for concurrent use.
type CSPBuilder struct {
	directives map[string][]string
	reportOnly bool
}

// NewCSPBuilder creates a new CSPBuilder with default empty directives.
//
// Returns:
//   - *CSPBuilder: A new builder instance ready for configuration
//
// Example:
//
//	builder := NewCSPBuilder()
//	builder.DefaultSrc("'self'")
func NewCSPBuilder() *CSPBuilder {
	return &CSPBuilder{
		directives: make(map[string][]string),
		reportOnly: false,
	}
}

// DefaultSrc sets the default-src directive.
//
// This directive serves as a fallback for other fetch directives.
// If a specific directive (like script-src) is not defined, the policy falls back to default-src.
//
// Parameters:
//   - sources: One or more source expressions (e.g., "'self'", "https://example.com", "'unsafe-inline'")
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Common Source Values:
//   - "'self'": Allow resources from the same origin
//   - "'none'": Block all resources
//   - "'unsafe-inline'": Allow inline scripts/styles (not recommended)
//   - "'unsafe-eval'": Allow eval() and similar functions (not recommended)
//   - "https://example.com": Allow resources from specific domain
//   - "data:": Allow data: URIs
//
// Example:
//
//	builder.DefaultSrc("'self'", "https://cdn.example.com")
func (b *CSPBuilder) DefaultSrc(sources ...string) *CSPBuilder {
	b.directives["default-src"] = sources
	return b
}

// ScriptSrc sets the script-src directive.
//
// Controls which sources are allowed for JavaScript execution.
// This is one of the most important directives for preventing XSS attacks.
//
// Parameters:
//   - sources: One or more source expressions
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Example:
//
//	builder.ScriptSrc("'self'", "https://cdn.jsdelivr.net")
func (b *CSPBuilder) ScriptSrc(sources ...string) *CSPBuilder {
	b.directives["script-src"] = sources
	return b
}

// StyleSrc sets the style-src directive.
//
// Controls which sources are allowed for stylesheets (CSS).
//
// Parameters:
//   - sources: One or more source expressions
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Example:
//
//	builder.StyleSrc("'self'", "'unsafe-inline'")
func (b *CSPBuilder) StyleSrc(sources ...string) *CSPBuilder {
	b.directives["style-src"] = sources
	return b
}

// ImgSrc sets the img-src directive.
//
// Controls which sources are allowed for images.
//
// Parameters:
//   - sources: One or more source expressions
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Example:
//
//	builder.ImgSrc("'self'", "data:", "https:")
func (b *CSPBuilder) ImgSrc(sources ...string) *CSPBuilder {
	b.directives["img-src"] = sources
	return b
}

// FontSrc sets the font-src directive.
//
// Controls which sources are allowed for fonts.
//
// Parameters:
//   - sources: One or more source expressions
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Example:
//
//	builder.FontSrc("'self'", "data:")
func (b *CSPBuilder) FontSrc(sources ...string) *CSPBuilder {
	b.directives["font-src"] = sources
	return b
}

// ConnectSrc sets the connect-src directive.
//
// Controls which URLs can be loaded using script interfaces (fetch, XMLHttpRequest, WebSocket, EventSource).
//
// Parameters:
//   - sources: One or more source expressions
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Example:
//
//	builder.ConnectSrc("'self'", "https://api.example.com")
func (b *CSPBuilder) ConnectSrc(sources ...string) *CSPBuilder {
	b.directives["connect-src"] = sources
	return b
}

// FrameAncestors sets the frame-ancestors directive.
//
// Controls which sources can embed this page in <frame>, <iframe>, <object>, or <embed>.
// This helps prevent clickjacking attacks.
//
// Parameters:
//   - sources: One or more source expressions
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Common Values:
//   - "'none'": Prevent all framing (recommended for most applications)
//   - "'self'": Allow framing only from same origin
//
// Example:
//
//	builder.FrameAncestors("'none'")
func (b *CSPBuilder) FrameAncestors(sources ...string) *CSPBuilder {
	b.directives["frame-ancestors"] = sources
	return b
}

// FormAction sets the form-action directive.
//
// Controls which URLs can be used as the action of HTML form submissions.
//
// Parameters:
//   - sources: One or more source expressions
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Example:
//
//	builder.FormAction("'self'")
func (b *CSPBuilder) FormAction(sources ...string) *CSPBuilder {
	b.directives["form-action"] = sources
	return b
}

// BaseUri sets the base-uri directive.
//
// Controls which URLs can be used in a document's <base> element.
// This prevents attackers from changing the base URL of relative URLs.
//
// Parameters:
//   - sources: One or more source expressions
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Example:
//
//	builder.BaseUri("'self'")
func (b *CSPBuilder) BaseUri(sources ...string) *CSPBuilder {
	b.directives["base-uri"] = sources
	return b
}

// ObjectSrc sets the object-src directive.
//
// Controls which sources are allowed for <object>, <embed>, and <applet> elements.
// It's recommended to set this to 'none' for security.
//
// Parameters:
//   - sources: One or more source expressions
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Example:
//
//	builder.ObjectSrc("'none'")
func (b *CSPBuilder) ObjectSrc(sources ...string) *CSPBuilder {
	b.directives["object-src"] = sources
	return b
}

// ReportUri sets the report-uri directive.
//
// Specifies a URI where violation reports should be sent.
// Note: This is deprecated in CSP Level 3 in favor of report-to, but still widely supported.
//
// Parameters:
//   - uri: The URI to send violation reports to
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Example:
//
//	builder.ReportUri("/csp-violation-report")
func (b *CSPBuilder) ReportUri(uri string) *CSPBuilder {
	b.directives["report-uri"] = []string{uri}
	return b
}

// ReportOnly sets whether the policy should be in report-only mode.
//
// In report-only mode, violations are reported but not enforced.
// This is useful for testing CSP policies before enforcing them.
//
// Parameters:
//   - enabled: true for report-only mode, false for enforcement mode
//
// Returns:
//   - *CSPBuilder: The builder instance for method chaining
//
// Example:
//
//	builder.ReportOnly(true).Build()
func (b *CSPBuilder) ReportOnly(enabled bool) *CSPBuilder {
	b.reportOnly = enabled
	return b
}

// Build generates the CSP header value string.
//
// This method constructs the final CSP policy string from all configured directives.
// Directives are joined with semicolons, and sources within each directive are space-separated.
//
// Returns:
//   - string: The complete CSP policy string ready for use in HTTP headers
//
// Example:
//
//	policy := NewCSPBuilder().
//	    DefaultSrc("'self'").
//	    ScriptSrc("'self'", "https://cdn.example.com").
//	    Build()
//	// Returns: "default-src 'self'; script-src 'self' https://cdn.example.com"
func (b *CSPBuilder) Build() string {
	if len(b.directives) == 0 {
		return ""
	}

	// Order matters for readability, so we'll use a consistent order
	directiveOrder := []string{
		"default-src",
		"script-src",
		"style-src",
		"img-src",
		"font-src",
		"connect-src",
		"frame-ancestors",
		"form-action",
		"base-uri",
		"object-src",
		"report-uri",
	}

	var parts []string
	for _, directive := range directiveOrder {
		if sources, exists := b.directives[directive]; exists && len(sources) > 0 {
			// Join sources with spaces
			directiveString := fmt.Sprintf("%s %s", directive, strings.Join(sources, " "))
			parts = append(parts, directiveString)
		}
	}

	return strings.Join(parts, "; ")
}

// HeaderName returns the appropriate CSP header name based on report-only mode.
//
// Returns:
//   - "Content-Security-Policy-Report-Only" if report-only mode is enabled
//   - "Content-Security-Policy" for enforcement mode
//
// Example:
//
//	builder := NewCSPBuilder().ReportOnly(true)
//	headerName := builder.HeaderName()  // Returns "Content-Security-Policy-Report-Only"
//	w.Header().Set(headerName, builder.Build())
func (b *CSPBuilder) HeaderName() string {
	if b.reportOnly {
		return "Content-Security-Policy-Report-Only"
	}
	return "Content-Security-Policy"
}

// SwaggerUIPolicy returns a CSP policy suitable for Swagger UI.
//
// Swagger UI requires specific permissions to function correctly:
//   - Inline scripts and styles ('unsafe-inline')
//   - Data URIs for images
//   - Blob URLs for API spec loading
//   - Self-hosted resources
//
// This policy balances security with Swagger UI functionality.
// For production, consider using a stricter policy and serving Swagger UI
// on a separate domain.
//
// Returns:
//   - *CSPBuilder: A pre-configured builder with Swagger UI-compatible directives
//
// Example:
//
//	policy := SwaggerUIPolicy().Build()
//	w.Header().Set("Content-Security-Policy", policy)
func SwaggerUIPolicy() *CSPBuilder {
	return NewCSPBuilder().
		DefaultSrc("'self'").
		ScriptSrc("'self'", "'unsafe-inline'", "https://cdn.jsdelivr.net").
		StyleSrc("'self'", "'unsafe-inline'", "https://cdn.jsdelivr.net").
		ImgSrc("'self'", "data:", "https:").
		FontSrc("'self'", "data:").
		ConnectSrc("'self'", "blob:").
		FrameAncestors("'none'").
		BaseUri("'self'").
		FormAction("'self'").
		ObjectSrc("'none'")
}

// StrictPolicy returns a strict CSP policy for API endpoints.
//
// This policy is highly restrictive and suitable for JSON API endpoints
// that don't serve HTML content. It blocks most content types and only
// allows same-origin connections.
//
// Use this for:
//   - REST API endpoints
//   - JSON-only responses
//   - Backend services without UI
//
// Returns:
//   - *CSPBuilder: A pre-configured builder with strict security directives
//
// Example:
//
//	policy := StrictPolicy().Build()
//	w.Header().Set("Content-Security-Policy", policy)
func StrictPolicy() *CSPBuilder {
	return NewCSPBuilder().
		DefaultSrc("'none'").
		ConnectSrc("'self'").
		FrameAncestors("'none'").
		BaseUri("'self'").
		FormAction("'self'")
}

// RelaxedPolicy returns a relaxed CSP policy for development.
//
// This policy is more permissive and suitable for development environments
// where external tools and resources may need access. It allows:
//   - Inline scripts and styles
//   - All HTTPS sources
//   - Data URIs
//
// WARNING: Do not use this policy in production. It provides minimal security.
//
// Returns:
//   - *CSPBuilder: A pre-configured builder with relaxed directives
//
// Example:
//
//	policy := RelaxedPolicy().Build()
//	w.Header().Set("Content-Security-Policy", policy)
func RelaxedPolicy() *CSPBuilder {
	return NewCSPBuilder().
		DefaultSrc("'self'").
		ScriptSrc("'self'", "'unsafe-inline'", "'unsafe-eval'", "https:").
		StyleSrc("'self'", "'unsafe-inline'", "https:").
		ImgSrc("'self'", "data:", "https:").
		FontSrc("'self'", "data:", "https:").
		ConnectSrc("'self'", "https:").
		FrameAncestors("'self'").
		BaseUri("'self'").
		FormAction("'self'")
}
