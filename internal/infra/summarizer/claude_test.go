package summarizer_test

import (
	"os"
	"testing"

	"catchup-feed/internal/infra/summarizer"
)

/* ───────── TASK-007: Configuration Loading Tests ───────── */

// TestLoadClaudeConfig_DefaultValue tests that default value (900) is used when env var is not set
func TestLoadClaudeConfig_DefaultValue(t *testing.T) {
	// Arrange: Clear environment variable
	_ = os.Unsetenv("SUMMARIZER_CHAR_LIMIT")

	// Act
	config := summarizer.LoadClaudeConfig()

	// Assert
	if config.CharacterLimit != 900 {
		t.Errorf("Expected default CharacterLimit=900, got %d", config.CharacterLimit)
	}
	if config.Language != "japanese" {
		t.Errorf("Expected Language=japanese, got %s", config.Language)
	}
}

// TestLoadClaudeConfig_CustomValue tests that custom value is loaded from environment variable
func TestLoadClaudeConfig_CustomValue(t *testing.T) {
	// Arrange: Set custom character limit
	_ = os.Setenv("SUMMARIZER_CHAR_LIMIT", "1200")
	defer func() { _ = os.Unsetenv("SUMMARIZER_CHAR_LIMIT") }()

	// Act
	config := summarizer.LoadClaudeConfig()

	// Assert
	if config.CharacterLimit != 1200 {
		t.Errorf("Expected CharacterLimit=1200, got %d", config.CharacterLimit)
	}
}

// TestLoadClaudeConfig_InvalidValue tests that invalid format falls back to default
func TestLoadClaudeConfig_InvalidValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"non-numeric", "invalid"},
		{"with letters", "900abc"},
		{"special chars", "!@#$"},
		{"empty string", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			if tt.value == "" {
				_ = os.Unsetenv("SUMMARIZER_CHAR_LIMIT")
			} else {
				_ = os.Setenv("SUMMARIZER_CHAR_LIMIT", tt.value)
			}
			defer func() { _ = os.Unsetenv("SUMMARIZER_CHAR_LIMIT") }()

			// Act
			config := summarizer.LoadClaudeConfig()

			// Assert
			if config.CharacterLimit != 900 {
				t.Errorf("Expected fallback to default (900), got %d", config.CharacterLimit)
			}
		})
	}
}

// TestLoadClaudeConfig_BelowMinimum tests that values below minimum (100) fall back to default
func TestLoadClaudeConfig_BelowMinimum(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"zero", "0"},
		{"negative", "-100"},
		{"below minimum", "50"},
		{"just below", "99"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			_ = os.Setenv("SUMMARIZER_CHAR_LIMIT", tt.value)
			defer func() { _ = os.Unsetenv("SUMMARIZER_CHAR_LIMIT") }()

			// Act
			config := summarizer.LoadClaudeConfig()

			// Assert
			if config.CharacterLimit != 900 {
				t.Errorf("Value %s should fall back to default (900), got %d", tt.value, config.CharacterLimit)
			}
		})
	}
}

// TestLoadClaudeConfig_AboveMaximum tests that values above maximum (5000) fall back to default
func TestLoadClaudeConfig_AboveMaximum(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"just above", "5001"},
		{"very large", "10000"},
		{"extremely large", "999999"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			_ = os.Setenv("SUMMARIZER_CHAR_LIMIT", tt.value)
			defer func() { _ = os.Unsetenv("SUMMARIZER_CHAR_LIMIT") }()

			// Act
			config := summarizer.LoadClaudeConfig()

			// Assert
			if config.CharacterLimit != 900 {
				t.Errorf("Value %s should fall back to default (900), got %d", tt.value, config.CharacterLimit)
			}
		})
	}
}

// TestLoadClaudeConfig_EdgeCases tests edge case values (0, negative, boundary values)
func TestLoadClaudeConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		expectedLimit int
	}{
		{"zero", "0", 900},
		{"negative small", "-1", 900},
		{"negative large", "-999", 900},
		{"minimum valid", "100", 100},
		{"maximum valid", "5000", 5000},
		{"just below min", "99", 900},
		{"just above max", "5001", 900},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			_ = os.Setenv("SUMMARIZER_CHAR_LIMIT", tt.value)
			defer func() { _ = os.Unsetenv("SUMMARIZER_CHAR_LIMIT") }()

			// Act
			config := summarizer.LoadClaudeConfig()

			// Assert
			if config.CharacterLimit != tt.expectedLimit {
				t.Errorf("For value %s: expected CharacterLimit=%d, got %d",
					tt.value, tt.expectedLimit, config.CharacterLimit)
			}
		})
	}
}

// TestLoadClaudeConfig_AllFields tests that all config fields are populated correctly
func TestLoadClaudeConfig_AllFields(t *testing.T) {
	// Arrange
	_ = os.Setenv("SUMMARIZER_CHAR_LIMIT", "1500")
	defer func() { _ = os.Unsetenv("SUMMARIZER_CHAR_LIMIT") }()

	// Act
	config := summarizer.LoadClaudeConfig()

	// Assert all fields
	if config.CharacterLimit != 1500 {
		t.Errorf("Expected CharacterLimit=1500, got %d", config.CharacterLimit)
	}
	if config.Language != "japanese" {
		t.Errorf("Expected Language=japanese, got %s", config.Language)
	}
	if config.Model == "" {
		t.Error("Model should not be empty")
	}
	if config.MaxTokens != 1024 {
		t.Errorf("Expected MaxTokens=1024, got %d", config.MaxTokens)
	}
	if config.Timeout.Seconds() != 60 {
		t.Errorf("Expected Timeout=60s, got %v", config.Timeout)
	}
}

// TestLoadClaudeConfig_ValidRangeBoundaries tests values at the exact boundaries of valid range
func TestLoadClaudeConfig_ValidRangeBoundaries(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		expectedLimit int
	}{
		{"minimum boundary", "100", 100},
		{"maximum boundary", "5000", 5000},
		{"midpoint", "2550", 2550},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			_ = os.Setenv("SUMMARIZER_CHAR_LIMIT", tt.value)
			defer func() { _ = os.Unsetenv("SUMMARIZER_CHAR_LIMIT") }()

			// Act
			config := summarizer.LoadClaudeConfig()

			// Assert
			if config.CharacterLimit != tt.expectedLimit {
				t.Errorf("Expected CharacterLimit=%d, got %d", tt.expectedLimit, config.CharacterLimit)
			}
		})
	}
}
