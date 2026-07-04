// Package config provides configuration loading helpers for the application.
package config

// CSPConfig contains the configuration for Content Security Policy.
//
// This struct holds settings for CSP headers, which help prevent
// cross-site scripting (XSS) and other code injection attacks.
type CSPConfig struct {
	// Enabled controls whether CSP headers are applied
	Enabled bool

	// ReportOnly sets the header to Content-Security-Policy-Report-Only
	// instead of Content-Security-Policy, which logs violations but does not enforce
	ReportOnly bool

	// TrustedScriptSources lists additional trusted script sources (e.g., CDN URLs)
	TrustedScriptSources []string

	// TrustedStyleSources lists additional trusted style sources (e.g., CDN URLs)
	TrustedStyleSources []string
}

// LoadCSPConfig loads Content Security Policy configuration from environment variables.
//
// Environment variables:
//   - CSP_ENABLED: Enable/disable CSP headers (default: true)
//   - CSP_REPORT_ONLY: Use report-only mode (default: false)
//
// Returns:
//   - *CSPConfig: CSP configuration
//   - error: Always nil
func LoadCSPConfig() (*CSPConfig, error) {
	config := &CSPConfig{
		Enabled:    GetEnvBool("CSP_ENABLED", true),
		ReportOnly: GetEnvBool("CSP_REPORT_ONLY", false),
	}

	return config, nil
}
