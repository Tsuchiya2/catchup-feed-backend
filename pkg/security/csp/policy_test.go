package csp

import (
	"strings"
	"testing"
)

func TestNewCSPBuilder(t *testing.T) {
	builder := NewCSPBuilder()

	if builder == nil {
		t.Fatal("NewCSPBuilder returned nil")
	}

	if builder.directives == nil {
		t.Error("directives map is nil")
	}

	if builder.reportOnly {
		t.Error("reportOnly should be false by default")
	}
}

func TestCSPBuilder_DefaultSrc(t *testing.T) {
	policy := NewCSPBuilder().
		DefaultSrc("'self'").
		Build()

	expected := "default-src 'self'"
	if policy != expected {
		t.Errorf("Expected %q, got %q", expected, policy)
	}
}

func TestCSPBuilder_MultipleDirectives(t *testing.T) {
	policy := NewCSPBuilder().
		DefaultSrc("'self'").
		ScriptSrc("'self'", "https://cdn.example.com").
		StyleSrc("'self'", "'unsafe-inline'").
		Build()

	// Check that all directives are present
	if !strings.Contains(policy, "default-src 'self'") {
		t.Error("default-src directive missing")
	}
	if !strings.Contains(policy, "script-src 'self' https://cdn.example.com") {
		t.Error("script-src directive incorrect")
	}
	if !strings.Contains(policy, "style-src 'self' 'unsafe-inline'") {
		t.Error("style-src directive incorrect")
	}
}

func TestCSPBuilder_AllDirectives(t *testing.T) {
	policy := NewCSPBuilder().
		DefaultSrc("'self'").
		ScriptSrc("'self'", "'unsafe-inline'").
		StyleSrc("'self'", "'unsafe-inline'").
		ImgSrc("'self'", "data:").
		FontSrc("'self'", "data:").
		ConnectSrc("'self'").
		FrameAncestors("'none'").
		FormAction("'self'").
		BaseUri("'self'").
		ObjectSrc("'none'").
		ReportUri("/csp-report").
		Build()

	// Check all directives are present
	directives := []string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline'",
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"form-action 'self'",
		"base-uri 'self'",
		"object-src 'none'",
		"report-uri /csp-report",
	}

	for _, directive := range directives {
		if !strings.Contains(policy, directive) {
			t.Errorf("Directive %q not found in policy", directive)
		}
	}
}

func TestCSPBuilder_EmptyBuild(t *testing.T) {
	policy := NewCSPBuilder().Build()

	if policy != "" {
		t.Errorf("Expected empty string, got %q", policy)
	}
}

func TestCSPBuilder_HeaderName(t *testing.T) {
	tests := []struct {
		name       string
		reportOnly bool
		expected   string
	}{
		{
			name:       "enforcement mode",
			reportOnly: false,
			expected:   "Content-Security-Policy",
		},
		{
			name:       "report-only mode",
			reportOnly: true,
			expected:   "Content-Security-Policy-Report-Only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewCSPBuilder().ReportOnly(tt.reportOnly)
			headerName := builder.HeaderName()

			if headerName != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, headerName)
			}
		})
	}
}

func TestCSPBuilder_MethodChaining(t *testing.T) {
	// Verify all methods return the builder for chaining
	builder := NewCSPBuilder().
		DefaultSrc("'self'").
		ScriptSrc("'self'").
		StyleSrc("'self'").
		ImgSrc("'self'").
		FontSrc("'self'").
		ConnectSrc("'self'").
		FrameAncestors("'none'").
		FormAction("'self'").
		BaseUri("'self'").
		ObjectSrc("'none'").
		ReportUri("/report").
		ReportOnly(true)

	if builder == nil {
		t.Fatal("Method chaining returned nil")
	}

	policy := builder.Build()
	if policy == "" {
		t.Error("Policy should not be empty after method chaining")
	}
}

func TestSwaggerUIPolicy(t *testing.T) {
	policy := SwaggerUIPolicy().Build()

	// Check required directives for Swagger UI
	requiredDirectives := []string{
		"default-src 'self'",
		"script-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net",
		"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net",
		"img-src 'self' data: https:",
		"font-src 'self' data:",
		"connect-src 'self' blob:",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"object-src 'none'",
	}

	for _, directive := range requiredDirectives {
		if !strings.Contains(policy, directive) {
			t.Errorf("Swagger UI policy missing directive: %q", directive)
		}
	}
}

func TestStrictPolicy(t *testing.T) {
	policy := StrictPolicy().Build()

	// Check strict policy directives
	requiredDirectives := []string{
		"default-src 'none'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'self'",
		"form-action 'self'",
	}

	for _, directive := range requiredDirectives {
		if !strings.Contains(policy, directive) {
			t.Errorf("Strict policy missing directive: %q", directive)
		}
	}

	// Strict policy should not allow unsafe-inline
	if strings.Contains(policy, "unsafe-inline") {
		t.Error("Strict policy should not contain 'unsafe-inline'")
	}
}

func TestRelaxedPolicy(t *testing.T) {
	policy := RelaxedPolicy().Build()

	// Check relaxed policy allows unsafe directives (for development)
	if !strings.Contains(policy, "unsafe-inline") {
		t.Error("Relaxed policy should contain 'unsafe-inline'")
	}
	if !strings.Contains(policy, "unsafe-eval") {
		t.Error("Relaxed policy should contain 'unsafe-eval'")
	}
}

func TestCSPBuilder_DirectiveOrder(t *testing.T) {
	// Build policy with directives in reverse order
	policy := NewCSPBuilder().
		ObjectSrc("'none'").
		BaseUri("'self'").
		FormAction("'self'").
		FrameAncestors("'none'").
		ConnectSrc("'self'").
		FontSrc("'self'").
		ImgSrc("'self'").
		StyleSrc("'self'").
		ScriptSrc("'self'").
		DefaultSrc("'self'").
		Build()

	// Directives should still appear in consistent order
	// default-src should come before script-src
	defaultIndex := strings.Index(policy, "default-src")
	scriptIndex := strings.Index(policy, "script-src")

	if defaultIndex < 0 || scriptIndex < 0 {
		t.Fatal("Missing directives in policy")
	}

	if defaultIndex > scriptIndex {
		t.Error("Directives are not in expected order (default-src should come before script-src)")
	}
}

func TestCSPBuilder_MultipleSources(t *testing.T) {
	policy := NewCSPBuilder().
		ScriptSrc("'self'", "https://cdn1.example.com", "https://cdn2.example.com", "'unsafe-inline'").
		Build()

	expected := "script-src 'self' https://cdn1.example.com https://cdn2.example.com 'unsafe-inline'"
	if policy != expected {
		t.Errorf("Expected %q, got %q", expected, policy)
	}
}

func TestCSPBuilder_OverwriteDirective(t *testing.T) {
	// Test that calling the same directive twice overwrites the previous value
	policy := NewCSPBuilder().
		DefaultSrc("'self'").
		DefaultSrc("'none'").  // This should overwrite the previous value
		Build()

	expected := "default-src 'none'"
	if policy != expected {
		t.Errorf("Expected %q, got %q", expected, policy)
	}
}

func TestSwaggerUIPolicy_ReportOnly(t *testing.T) {
	// Test that we can use report-only mode with preset policies
	builder := SwaggerUIPolicy().ReportOnly(true)

	headerName := builder.HeaderName()
	if headerName != "Content-Security-Policy-Report-Only" {
		t.Errorf("Expected report-only header name, got %q", headerName)
	}

	policy := builder.Build()
	if policy == "" {
		t.Error("Policy should not be empty")
	}
}

func TestCSPBuilder_EmptySources(t *testing.T) {
	// Test that directives with empty sources are not included
	policy := NewCSPBuilder().
		DefaultSrc().  // Empty sources
		ScriptSrc("'self'").
		Build()

	// default-src with empty sources should not be included
	if strings.Contains(policy, "default-src") {
		t.Error("default-src with empty sources should not be included")
	}

	// script-src should still be present
	if !strings.Contains(policy, "script-src 'self'") {
		t.Error("script-src should be present")
	}
}

// Benchmark tests
func BenchmarkCSPBuilder_Build(b *testing.B) {
	builder := NewCSPBuilder().
		DefaultSrc("'self'").
		ScriptSrc("'self'", "https://cdn.example.com").
		StyleSrc("'self'", "'unsafe-inline'").
		ImgSrc("'self'", "data:").
		FontSrc("'self'").
		ConnectSrc("'self'").
		FrameAncestors("'none'").
		FormAction("'self'").
		BaseUri("'self'").
		ObjectSrc("'none'")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = builder.Build()
	}
}

func BenchmarkSwaggerUIPolicy(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = SwaggerUIPolicy().Build()
	}
}
