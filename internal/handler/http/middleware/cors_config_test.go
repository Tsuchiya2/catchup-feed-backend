package middleware

import (
	"os"
	"strings"
	"testing"
)

// TestEnvConfigSource_LoadOrigins_Valid tests loading
// valid origins from environment variable
func TestEnvConfigSource_LoadOrigins_Valid(t *testing.T) {
	testCases := []struct {
		name           string
		envValue       string
		expectedCount  int
		expectedFirst  string
	}{
		{
			name:          "single origin",
			envValue:      "http://localhost:3000",
			expectedCount: 1,
			expectedFirst: "http://localhost:3000",
		},
		{
			name:          "multiple origins",
			envValue:      "http://localhost:3000,https://example.com",
			expectedCount: 2,
			expectedFirst: "http://localhost:3000",
		},
		{
			name:          "origins with whitespace",
			envValue:      "  http://localhost:3000  ,  https://example.com  ",
			expectedCount: 2,
			expectedFirst: "http://localhost:3000",
		},
		{
			name:          "three origins",
			envValue:      "http://localhost:3000,http://localhost:3001,https://example.com",
			expectedCount: 3,
			expectedFirst: "http://localhost:3000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CORS_ALLOWED_ORIGINS", tc.envValue)

			source := &EnvConfigSource{}
			origins, err := source.LoadOrigins()

			if err != nil {
				t.Fatalf("LoadOrigins() returned unexpected error: %v", err)
			}

			if len(origins) != tc.expectedCount {
				t.Errorf("Expected %d origins, got %d", tc.expectedCount, len(origins))
			}

			if len(origins) > 0 && origins[0] != tc.expectedFirst {
				t.Errorf("First origin: expected %q, got %q", tc.expectedFirst, origins[0])
			}
		})
	}
}

// TestEnvConfigSource_LoadOrigins_EmptyReturnsError tests that
// missing CORS_ALLOWED_ORIGINS returns error
func TestEnvConfigSource_LoadOrigins_EmptyReturnsError(t *testing.T) {
	// Unset CORS_ALLOWED_ORIGINS
	_ = os.Unsetenv("CORS_ALLOWED_ORIGINS") //nolint:errcheck

	source := &EnvConfigSource{}
	origins, err := source.LoadOrigins()

	if err == nil {
		t.Error("Expected error for missing CORS_ALLOWED_ORIGINS, got nil")
	}

	if origins != nil {
		t.Errorf("Expected nil origins, got %v", origins)
	}

	if !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS") {
		t.Errorf("Error should mention CORS_ALLOWED_ORIGINS, got: %v", err)
	}
}

// TestEnvConfigSource_LoadOrigins_InvalidURLFormat tests that
// invalid URL format returns error
func TestEnvConfigSource_LoadOrigins_InvalidURLFormat(t *testing.T) {
	testCases := []struct {
		name     string
		envValue string
		errMsg   string
	}{
		{
			name:     "missing scheme",
			envValue: "localhost:3000",
			errMsg:   "scheme",
		},
		{
			name:     "invalid scheme",
			envValue: "ftp://localhost:3000",
			errMsg:   "scheme",
		},
		{
			name:     "with path",
			envValue: "http://localhost:3000/path",
			errMsg:   "path",
		},
		{
			name:     "with query string",
			envValue: "http://localhost:3000?query=value",
			errMsg:   "query",
		},
		{
			name:     "with fragment",
			envValue: "http://localhost:3000#fragment",
			errMsg:   "fragment",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CORS_ALLOWED_ORIGINS", tc.envValue)

			source := &EnvConfigSource{}
			origins, err := source.LoadOrigins()

			if err == nil {
				t.Errorf("Expected error for invalid origin %q, got nil", tc.envValue)
			}

			if origins != nil {
				t.Errorf("Expected nil origins for invalid config, got %v", origins)
			}

			if !strings.Contains(strings.ToLower(err.Error()), tc.errMsg) {
				t.Errorf("Error should mention %q, got: %v", tc.errMsg, err)
			}
		})
	}
}

// TestEnvConfigSource_LoadOrigins_TrailingSlashReturnsError tests that
// origins with trailing slash return error
func TestEnvConfigSource_LoadOrigins_TrailingSlashReturnsError(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000/")

	source := &EnvConfigSource{}
	origins, err := source.LoadOrigins()

	if err == nil {
		t.Error("Expected error for origin with trailing slash, got nil")
	}

	if origins != nil {
		t.Errorf("Expected nil origins, got %v", origins)
	}

	if !strings.Contains(err.Error(), "trailing slash") {
		t.Errorf("Error should mention trailing slash, got: %v", err)
	}
}

// TestEnvConfigSource_LoadMethods_Default tests that
// default methods are returned when not configured
func TestEnvConfigSource_LoadMethods_Default(t *testing.T) {
	// Unset CORS_ALLOWED_METHODS
	_ = os.Unsetenv("CORS_ALLOWED_METHODS") //nolint:errcheck

	source := &EnvConfigSource{}
	methods, err := source.LoadMethods()

	if err != nil {
		t.Fatalf("LoadMethods() returned unexpected error: %v", err)
	}

	expectedMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
	if len(methods) != len(expectedMethods) {
		t.Errorf("Expected %d default methods, got %d", len(expectedMethods), len(methods))
	}

	// Verify all expected methods are present
	methodMap := make(map[string]bool)
	for _, method := range methods {
		methodMap[method] = true
	}

	for _, expected := range expectedMethods {
		if !methodMap[expected] {
			t.Errorf("Expected default method %q not found", expected)
		}
	}
}

// TestEnvConfigSource_LoadMethods_Custom tests loading
// custom methods from environment variable
func TestEnvConfigSource_LoadMethods_Custom(t *testing.T) {
	testCases := []struct {
		name           string
		envValue       string
		expectedCount  int
		expectedFirst  string
	}{
		{
			name:          "GET and POST only",
			envValue:      "GET,POST",
			expectedCount: 2,
			expectedFirst: "GET",
		},
		{
			name:          "all standard methods",
			envValue:      "GET,POST,PUT,DELETE,PATCH,OPTIONS",
			expectedCount: 6,
			expectedFirst: "GET",
		},
		{
			name:          "lowercase converted to uppercase",
			envValue:      "get,post",
			expectedCount: 2,
			expectedFirst: "GET",
		},
		{
			name:          "with whitespace",
			envValue:      "  GET  ,  POST  ",
			expectedCount: 2,
			expectedFirst: "GET",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CORS_ALLOWED_METHODS", tc.envValue)

			source := &EnvConfigSource{}
			methods, err := source.LoadMethods()

			if err != nil {
				t.Fatalf("LoadMethods() returned unexpected error: %v", err)
			}

			if len(methods) != tc.expectedCount {
				t.Errorf("Expected %d methods, got %d", tc.expectedCount, len(methods))
			}

			if len(methods) > 0 && methods[0] != tc.expectedFirst {
				t.Errorf("First method: expected %q, got %q", tc.expectedFirst, methods[0])
			}
		})
	}
}

// TestEnvConfigSource_LoadMethods_InvalidMethod tests that
// invalid HTTP methods return error
func TestEnvConfigSource_LoadMethods_InvalidMethod(t *testing.T) {
	testCases := []struct {
		name     string
		envValue string
	}{
		{"invalid method", "GET,INVALID,POST"},
		{"TRACE not allowed", "GET,TRACE"},
		{"CONNECT not allowed", "GET,CONNECT"},
		{"random text", "GET,FOOBAR"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CORS_ALLOWED_METHODS", tc.envValue)

			source := &EnvConfigSource{}
			methods, err := source.LoadMethods()

			if err == nil {
				t.Errorf("Expected error for invalid method in %q, got nil", tc.envValue)
			}

			if methods != nil {
				t.Errorf("Expected nil methods for invalid config, got %v", methods)
			}
		})
	}
}

// TestEnvConfigSource_LoadHeaders_Default tests that
// default headers are returned when not configured
func TestEnvConfigSource_LoadHeaders_Default(t *testing.T) {
	// Unset CORS_ALLOWED_HEADERS
	_ = os.Unsetenv("CORS_ALLOWED_HEADERS") //nolint:errcheck

	source := &EnvConfigSource{}
	headers, err := source.LoadHeaders()

	if err != nil {
		t.Fatalf("LoadHeaders() returned unexpected error: %v", err)
	}

	expectedHeaders := []string{"Content-Type", "Authorization", "X-Request-ID"}
	if len(headers) != len(expectedHeaders) {
		t.Errorf("Expected %d default headers, got %d", len(expectedHeaders), len(headers))
	}

	for i, expected := range expectedHeaders {
		if headers[i] != expected {
			t.Errorf("Header %d: expected %q, got %q", i, expected, headers[i])
		}
	}
}

// TestEnvConfigSource_LoadHeaders_Custom tests loading
// custom headers from environment variable
func TestEnvConfigSource_LoadHeaders_Custom(t *testing.T) {
	testCases := []struct {
		name           string
		envValue       string
		expectedCount  int
		expectedFirst  string
	}{
		{
			name:          "content-type only",
			envValue:      "Content-Type",
			expectedCount: 1,
			expectedFirst: "Content-Type",
		},
		{
			name:          "multiple headers",
			envValue:      "Content-Type,Authorization,X-Custom-Header",
			expectedCount: 3,
			expectedFirst: "Content-Type",
		},
		{
			name:          "with whitespace",
			envValue:      "  Content-Type  ,  Authorization  ",
			expectedCount: 2,
			expectedFirst: "Content-Type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CORS_ALLOWED_HEADERS", tc.envValue)

			source := &EnvConfigSource{}
			headers, err := source.LoadHeaders()

			if err != nil {
				t.Fatalf("LoadHeaders() returned unexpected error: %v", err)
			}

			if len(headers) != tc.expectedCount {
				t.Errorf("Expected %d headers, got %d", tc.expectedCount, len(headers))
			}

			if len(headers) > 0 && headers[0] != tc.expectedFirst {
				t.Errorf("First header: expected %q, got %q", tc.expectedFirst, headers[0])
			}
		})
	}
}

// TestEnvConfigSource_LoadMaxAge_Default tests that
// default max age is returned when not configured
func TestEnvConfigSource_LoadMaxAge_Default(t *testing.T) {
	// Unset CORS_MAX_AGE
	_ = os.Unsetenv("CORS_MAX_AGE") //nolint:errcheck

	source := &EnvConfigSource{}
	maxAge, err := source.LoadMaxAge()

	if err != nil {
		t.Fatalf("LoadMaxAge() returned unexpected error: %v", err)
	}

	expectedMaxAge := 86400 // 24 hours
	if maxAge != expectedMaxAge {
		t.Errorf("Expected default max age %d, got %d", expectedMaxAge, maxAge)
	}
}

// TestEnvConfigSource_LoadMaxAge_Valid tests loading
// valid max age values
func TestEnvConfigSource_LoadMaxAge_Valid(t *testing.T) {
	testCases := []struct {
		name     string
		envValue string
		expected int
	}{
		{"1 hour", "3600", 3600},
		{"24 hours", "86400", 86400},
		{"1 week", "604800", 604800},
		{"zero (no cache)", "0", 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CORS_MAX_AGE", tc.envValue)

			source := &EnvConfigSource{}
			maxAge, err := source.LoadMaxAge()

			if err != nil {
				t.Fatalf("LoadMaxAge() returned unexpected error: %v", err)
			}

			if maxAge != tc.expected {
				t.Errorf("Expected max age %d, got %d", tc.expected, maxAge)
			}
		})
	}
}

// TestEnvConfigSource_LoadMaxAge_InvalidFormat tests that
// invalid format returns error
func TestEnvConfigSource_LoadMaxAge_InvalidFormat(t *testing.T) {
	testCases := []struct {
		name     string
		envValue string
	}{
		{"not a number", "invalid"},
		{"float value", "3600.5"},
		{"with units", "3600s"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CORS_MAX_AGE", tc.envValue)

			source := &EnvConfigSource{}
			maxAge, err := source.LoadMaxAge()

			if err == nil {
				t.Errorf("Expected error for invalid max age %q, got nil", tc.envValue)
			}

			if maxAge != 0 {
				t.Errorf("Expected 0 for invalid config, got %d", maxAge)
			}

			if !strings.Contains(err.Error(), "CORS_MAX_AGE") {
				t.Errorf("Error should mention CORS_MAX_AGE, got: %v", err)
			}
		})
	}
}

// TestEnvConfigSource_LoadMaxAge_NegativeValue tests that
// negative values return error
func TestEnvConfigSource_LoadMaxAge_NegativeValue(t *testing.T) {
	t.Setenv("CORS_MAX_AGE", "-1")

	source := &EnvConfigSource{}
	maxAge, err := source.LoadMaxAge()

	if err == nil {
		t.Error("Expected error for negative max age, got nil")
	}

	if maxAge != 0 {
		t.Errorf("Expected 0 for invalid config, got %d", maxAge)
	}

	if !strings.Contains(err.Error(), "non-negative") {
		t.Errorf("Error should mention non-negative, got: %v", err)
	}
}

// TestLoadCORSConfig_Success tests successful configuration loading
func TestLoadCORSConfig_Success(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000,https://example.com")
	t.Setenv("CORS_ALLOWED_METHODS", "GET,POST")
	t.Setenv("CORS_ALLOWED_HEADERS", "Content-Type,Authorization")
	t.Setenv("CORS_MAX_AGE", "3600")

	config, err := LoadCORSConfig()

	if err != nil {
		t.Fatalf("LoadCORSConfig() returned unexpected error: %v", err)
	}

	if config == nil {
		t.Fatal("Expected non-nil config, got nil")
	}

	// Verify validator is set
	if config.Validator == nil {
		t.Error("Expected non-nil Validator, got nil")
	}

	// Verify allowed origins
	if len(config.AllowedOrigins) != 2 {
		t.Errorf("Expected 2 allowed origins, got %d", len(config.AllowedOrigins))
	}

	// Verify allowed methods
	if len(config.AllowedMethods) != 2 {
		t.Errorf("Expected 2 allowed methods, got %d", len(config.AllowedMethods))
	}

	// Verify allowed headers
	if len(config.AllowedHeaders) != 2 {
		t.Errorf("Expected 2 allowed headers, got %d", len(config.AllowedHeaders))
	}

	// Verify max age
	if config.MaxAge != 3600 {
		t.Errorf("Expected max age 3600, got %d", config.MaxAge)
	}

	// Verify AllowCredentials is true
	if !config.AllowCredentials {
		t.Error("Expected AllowCredentials to be true")
	}

	// Verify logger is nil (caller must inject)
	if config.Logger != nil {
		t.Error("Expected Logger to be nil (caller must inject)")
	}
}

// TestLoadCORSConfig_MissingOrigins tests that
// missing CORS_ALLOWED_ORIGINS returns error
func TestLoadCORSConfig_MissingOrigins(t *testing.T) {
	// Unset all CORS environment variables
	_ = os.Unsetenv("CORS_ALLOWED_ORIGINS") //nolint:errcheck
	_ = os.Unsetenv("CORS_ALLOWED_METHODS") //nolint:errcheck
	_ = os.Unsetenv("CORS_ALLOWED_HEADERS") //nolint:errcheck
	_ = os.Unsetenv("CORS_MAX_AGE") //nolint:errcheck

	config, err := LoadCORSConfig()

	if err == nil {
		t.Error("Expected error for missing CORS_ALLOWED_ORIGINS, got nil")
	}

	if config != nil {
		t.Errorf("Expected nil config for invalid configuration, got %v", config)
	}
}

// TestLoadCORSConfig_DefaultValues tests that
// default values are used for optional parameters
func TestLoadCORSConfig_DefaultValues(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")
	// Unset optional parameters
	_ = os.Unsetenv("CORS_ALLOWED_METHODS") //nolint:errcheck
	_ = os.Unsetenv("CORS_ALLOWED_HEADERS") //nolint:errcheck
	_ = os.Unsetenv("CORS_MAX_AGE") //nolint:errcheck

	config, err := LoadCORSConfig()

	if err != nil {
		t.Fatalf("LoadCORSConfig() returned unexpected error: %v", err)
	}

	// Verify default methods
	expectedMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
	if len(config.AllowedMethods) != len(expectedMethods) {
		t.Errorf("Expected %d default methods, got %d", len(expectedMethods), len(config.AllowedMethods))
	}

	// Verify default headers
	expectedHeaders := []string{"Content-Type", "Authorization", "X-Request-ID"}
	if len(config.AllowedHeaders) != len(expectedHeaders) {
		t.Errorf("Expected %d default headers, got %d", len(expectedHeaders), len(config.AllowedHeaders))
	}

	// Verify default max age
	if config.MaxAge != 86400 {
		t.Errorf("Expected default max age 86400, got %d", config.MaxAge)
	}
}

// TestLoadCORSConfigFromSource_WithLogger tests loading
// configuration with custom logger
func TestLoadCORSConfigFromSource_WithLogger(t *testing.T) {
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")

	// Create mock logger
	logger := &NoOpLogger{}

	source := &EnvConfigSource{}
	config, err := LoadCORSConfigFromSource(source, logger)

	if err != nil {
		t.Fatalf("LoadCORSConfigFromSource() returned unexpected error: %v", err)
	}

	// Verify logger is set
	if config.Logger == nil {
		t.Error("Expected non-nil Logger, got nil")
	}

	if config.Logger != logger {
		t.Error("Logger was not set to the provided logger")
	}
}

// TestLoadCORSConfigFromSource_InvalidConfiguration tests that
// invalid configuration from source returns error
func TestLoadCORSConfigFromSource_InvalidConfiguration(t *testing.T) {
	testCases := []struct {
		name          string
		setupEnv      func(*testing.T)
		expectedError string
	}{
		{
			name: "invalid origins",
			setupEnv: func(t *testing.T) {
				t.Setenv("CORS_ALLOWED_ORIGINS", "invalid-url")
			},
			expectedError: "failed to load allowed origins",
		},
		{
			name: "invalid methods",
			setupEnv: func(t *testing.T) {
				t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")
				t.Setenv("CORS_ALLOWED_METHODS", "INVALID")
			},
			expectedError: "failed to load allowed methods",
		},
		{
			name: "invalid max age",
			setupEnv: func(t *testing.T) {
				t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000")
				t.Setenv("CORS_MAX_AGE", "invalid")
			},
			expectedError: "failed to load max age",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setupEnv(t)

			source := &EnvConfigSource{}
			config, err := LoadCORSConfigFromSource(source, nil)

			if err == nil {
				t.Error("Expected error for invalid configuration, got nil")
			}

			if config != nil {
				t.Errorf("Expected nil config for invalid configuration, got %v", config)
			}

			if !strings.Contains(err.Error(), tc.expectedError) {
				t.Errorf("Error should contain %q, got: %v", tc.expectedError, err)
			}
		})
	}
}

// TestEnvConfigSource_EmptyHeadersAfterTrim tests that
// empty headers after trimming return error
func TestEnvConfigSource_EmptyHeadersAfterTrim(t *testing.T) {
	t.Setenv("CORS_ALLOWED_HEADERS", "  ,  ,  ")

	source := &EnvConfigSource{}
	headers, err := source.LoadHeaders()

	if err == nil {
		t.Error("Expected error for all-empty headers, got nil")
	}

	if headers != nil {
		t.Errorf("Expected nil headers, got %v", headers)
	}
}

// TestEnvConfigSource_EmptyMethodsAfterTrim tests that
// empty methods after trimming return error
func TestEnvConfigSource_EmptyMethodsAfterTrim(t *testing.T) {
	t.Setenv("CORS_ALLOWED_METHODS", "  ,  ,  ")

	source := &EnvConfigSource{}
	methods, err := source.LoadMethods()

	if err == nil {
		t.Error("Expected error for all-empty methods, got nil")
	}

	if methods != nil {
		t.Errorf("Expected nil methods, got %v", methods)
	}
}
