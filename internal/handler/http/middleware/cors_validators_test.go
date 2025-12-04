package middleware

import (
	"testing"
)

// TestWhitelistValidator_IsAllowed_ExactMatch tests that
// allowed origins return true for exact match
func TestWhitelistValidator_IsAllowed_ExactMatch(t *testing.T) {
	validator := NewWhitelistValidator([]string{
		"http://localhost:3000",
		"https://example.com",
	})

	testCases := []struct {
		name     string
		origin   string
		expected bool
	}{
		{"allowed localhost", "http://localhost:3000", true},
		{"allowed https", "https://example.com", true},
		{"disallowed origin", "http://malicious.com", false},
		{"disallowed subdomain", "http://subdomain.example.com", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := validator.IsAllowed(tc.origin)
			if result != tc.expected {
				t.Errorf("IsAllowed(%q) = %v, expected %v", tc.origin, result, tc.expected)
			}
		})
	}
}

// TestWhitelistValidator_IsAllowed_CaseInsensitive tests that
// origin comparison is case-insensitive
func TestWhitelistValidator_IsAllowed_CaseInsensitive(t *testing.T) {
	validator := NewWhitelistValidator([]string{
		"http://localhost:3000",
	})

	testCases := []struct {
		name     string
		origin   string
		expected bool
	}{
		{"lowercase", "http://localhost:3000", true},
		{"uppercase scheme", "HTTP://localhost:3000", true},
		{"uppercase host", "http://LOCALHOST:3000", true},
		{"mixed case", "HtTp://LoCaLhOsT:3000", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := validator.IsAllowed(tc.origin)
			if result != tc.expected {
				t.Errorf("IsAllowed(%q) = %v, expected %v", tc.origin, result, tc.expected)
			}
		})
	}
}

// TestWhitelistValidator_IsAllowed_TrailingSlashNormalization tests that
// trailing slashes are normalized during comparison
func TestWhitelistValidator_IsAllowed_TrailingSlashNormalization(t *testing.T) {
	validator := NewWhitelistValidator([]string{
		"http://localhost:3000",
	})

	testCases := []struct {
		name     string
		origin   string
		expected bool
	}{
		{"no trailing slash", "http://localhost:3000", true},
		{"with trailing slash", "http://localhost:3000/", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := validator.IsAllowed(tc.origin)
			if result != tc.expected {
				t.Errorf("IsAllowed(%q) = %v, expected %v", tc.origin, result, tc.expected)
			}
		})
	}
}

// TestWhitelistValidator_IsAllowed_EmptyOrigin tests that
// empty origin returns false
func TestWhitelistValidator_IsAllowed_EmptyOrigin(t *testing.T) {
	validator := NewWhitelistValidator([]string{
		"http://localhost:3000",
	})

	testCases := []struct {
		name   string
		origin string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := validator.IsAllowed(tc.origin)
			if result {
				t.Errorf("IsAllowed(%q) = true, expected false for empty/whitespace origin", tc.origin)
			}
		})
	}
}

// TestWhitelistValidator_IsAllowed_EmptyAllowedList tests that
// empty allowed list returns false for all origins
func TestWhitelistValidator_IsAllowed_EmptyAllowedList(t *testing.T) {
	validator := NewWhitelistValidator([]string{})

	testCases := []string{
		"http://localhost:3000",
		"https://example.com",
		"http://any-origin.com",
	}

	for _, origin := range testCases {
		t.Run(origin, func(t *testing.T) {
			result := validator.IsAllowed(origin)
			if result {
				t.Errorf("IsAllowed(%q) = true, expected false for empty whitelist", origin)
			}
		})
	}
}

// TestWhitelistValidator_GetAllowedOrigins_DefensiveCopy tests that
// GetAllowedOrigins returns a defensive copy
func TestWhitelistValidator_GetAllowedOrigins_DefensiveCopy(t *testing.T) {
	original := []string{
		"http://localhost:3000",
		"https://example.com",
	}

	validator := NewWhitelistValidator(original)

	// Get allowed origins
	copy := validator.GetAllowedOrigins()

	// Verify initial state
	if len(copy) != 2 {
		t.Errorf("Expected 2 allowed origins, got %d", len(copy))
	}

	// Modify the copy
	copy[0] = "http://modified.com"

	// Get allowed origins again
	newCopy := validator.GetAllowedOrigins()

	// Verify original is unchanged (defensive copy)
	if newCopy[0] == "http://modified.com" {
		t.Error("Modifying returned slice affected internal state (not a defensive copy)")
	}

	// Verify normalized form
	if newCopy[0] != "http://localhost:3000" {
		t.Errorf("Expected normalized origin 'http://localhost:3000', got %q", newCopy[0])
	}
}

// TestWhitelistValidator_Normalization tests that
// origins are normalized during construction
func TestWhitelistValidator_Normalization(t *testing.T) {
	validator := NewWhitelistValidator([]string{
		"HTTP://LOCALHOST:3000/",  // uppercase + trailing slash
		"https://Example.COM",      // mixed case
		"  http://test.com  ",      // whitespace
		"",                         // empty (should be filtered)
		"   ",                      // whitespace only (should be filtered)
	})

	allowedOrigins := validator.GetAllowedOrigins()

	// Should have 3 valid origins (empty and whitespace-only filtered out)
	if len(allowedOrigins) != 3 {
		t.Errorf("Expected 3 allowed origins, got %d", len(allowedOrigins))
	}

	// Verify normalization
	expected := []string{
		"http://localhost:3000",
		"https://example.com",
		"http://test.com",
	}

	for i, expectedOrigin := range expected {
		if allowedOrigins[i] != expectedOrigin {
			t.Errorf("Origin %d: expected %q, got %q", i, expectedOrigin, allowedOrigins[i])
		}
	}
}

// TestWhitelistValidator_MultipleOrigins tests validation
// with multiple allowed origins
func TestWhitelistValidator_MultipleOrigins(t *testing.T) {
	validator := NewWhitelistValidator([]string{
		"http://localhost:3000",
		"http://localhost:3001",
		"https://example.com",
		"https://api.example.com",
	})

	testCases := []struct {
		origin   string
		expected bool
	}{
		{"http://localhost:3000", true},
		{"http://localhost:3001", true},
		{"http://localhost:3002", false},
		{"https://example.com", true},
		{"https://api.example.com", true},
		{"https://www.example.com", false},
		{"http://example.com", false}, // Different scheme
	}

	for _, tc := range testCases {
		t.Run(tc.origin, func(t *testing.T) {
			result := validator.IsAllowed(tc.origin)
			if result != tc.expected {
				t.Errorf("IsAllowed(%q) = %v, expected %v", tc.origin, result, tc.expected)
			}
		})
	}
}

// TestWhitelistValidator_PortSensitivity tests that
// port numbers are considered in validation
func TestWhitelistValidator_PortSensitivity(t *testing.T) {
	validator := NewWhitelistValidator([]string{
		"http://localhost:3000",
	})

	testCases := []struct {
		origin   string
		expected bool
	}{
		{"http://localhost:3000", true},
		{"http://localhost:3001", false},
		{"http://localhost:8080", false},
		{"http://localhost", false}, // No port
	}

	for _, tc := range testCases {
		t.Run(tc.origin, func(t *testing.T) {
			result := validator.IsAllowed(tc.origin)
			if result != tc.expected {
				t.Errorf("IsAllowed(%q) = %v, expected %v", tc.origin, result, tc.expected)
			}
		})
	}
}

// TestWhitelistValidator_SchemeSensitivity tests that
// scheme (http vs https) is considered in validation
func TestWhitelistValidator_SchemeSensitivity(t *testing.T) {
	validator := NewWhitelistValidator([]string{
		"http://example.com",
	})

	testCases := []struct {
		origin   string
		expected bool
	}{
		{"http://example.com", true},
		{"https://example.com", false}, // Different scheme
	}

	for _, tc := range testCases {
		t.Run(tc.origin, func(t *testing.T) {
			result := validator.IsAllowed(tc.origin)
			if result != tc.expected {
				t.Errorf("IsAllowed(%q) = %v, expected %v", tc.origin, result, tc.expected)
			}
		})
	}
}

// TestWhitelistValidator_IPv6Origins tests validation
// with IPv6 addresses
func TestWhitelistValidator_IPv6Origins(t *testing.T) {
	validator := NewWhitelistValidator([]string{
		"http://[::1]:8080",
		"https://[2001:db8::1]:443",
	})

	testCases := []struct {
		origin   string
		expected bool
	}{
		{"http://[::1]:8080", true},
		{"https://[2001:db8::1]:443", true},
		{"http://[::1]:9000", false}, // Different port
		{"http://[2001:db8::2]:443", false}, // Different address
	}

	for _, tc := range testCases {
		t.Run(tc.origin, func(t *testing.T) {
			result := validator.IsAllowed(tc.origin)
			if result != tc.expected {
				t.Errorf("IsAllowed(%q) = %v, expected %v", tc.origin, result, tc.expected)
			}
		})
	}
}

// TestWhitelistValidator_PerformanceWithManyOrigins tests
// performance with a large whitelist
func TestWhitelistValidator_PerformanceWithManyOrigins(t *testing.T) {
	// Create validator with 1000 origins
	origins := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		origins[i] = "https://example" + string(rune(i)) + ".com"
	}
	validator := NewWhitelistValidator(origins)

	// Test that validation still works efficiently
	// Worst case: origin not in list (O(n) scan)
	result := validator.IsAllowed("https://notinlist.com")
	if result {
		t.Error("Expected false for origin not in whitelist")
	}

	// Best case: origin at beginning
	result = validator.IsAllowed(origins[0])
	if !result {
		t.Error("Expected true for first origin in whitelist")
	}

	// Middle case: origin in middle
	result = validator.IsAllowed(origins[500])
	if !result {
		t.Error("Expected true for middle origin in whitelist")
	}
}
