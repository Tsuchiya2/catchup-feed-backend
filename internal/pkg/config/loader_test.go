package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ============================================================================
// Test Group 1: LoadEnvString
// ============================================================================

func TestLoadEnvString_WithValue(t *testing.T) {
	t.Setenv("TEST_STRING", "custom_value")

	result := LoadEnvString("TEST_STRING", "default_value")

	assert.Equal(t, "custom_value", result)
}

func TestLoadEnvString_WithoutValue(t *testing.T) {
	// Don't set TEST_STRING

	result := LoadEnvString("TEST_STRING", "default_value")

	assert.Equal(t, "default_value", result)
}

func TestLoadEnvString_EmptyString(t *testing.T) {
	t.Setenv("TEST_STRING", "")

	result := LoadEnvString("TEST_STRING", "default_value")

	// Empty string should use default
	assert.Equal(t, "default_value", result)
}

// ============================================================================
// Test Group 2: LoadEnvWithFallback - Basic Loading
// ============================================================================

func TestLoadEnvWithFallback_WithValidValue(t *testing.T) {
	t.Setenv("TEST_CRON", "0 6 * * *")

	result := LoadEnvWithFallback("TEST_CRON", "30 5 * * *", ValidateCronSchedule)

	assert.Equal(t, "0 6 * * *", result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvWithFallback_WithoutValue(t *testing.T) {
	// Don't set TEST_CRON

	result := LoadEnvWithFallback("TEST_CRON", "30 5 * * *", ValidateCronSchedule)

	assert.Equal(t, "30 5 * * *", result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvWithFallback_EmptyValue(t *testing.T) {
	t.Setenv("TEST_CRON", "")

	result := LoadEnvWithFallback("TEST_CRON", "30 5 * * *", ValidateCronSchedule)

	// Empty value should use default without warning
	assert.Equal(t, "30 5 * * *", result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvWithFallback_NoValidator(t *testing.T) {
	t.Setenv("TEST_STRING", "any_value")

	result := LoadEnvWithFallback("TEST_STRING", "default", nil)

	// Without validator, any value should be accepted
	assert.Equal(t, "any_value", result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

// ============================================================================
// Test Group 3: LoadEnvWithFallback - Validation Failure and Fallback
// ============================================================================

func TestLoadEnvWithFallback_InvalidCronSchedule(t *testing.T) {
	t.Setenv("TEST_CRON", "invalid format")

	result := LoadEnvWithFallback("TEST_CRON", "30 5 * * *", ValidateCronSchedule)

	// Should fallback to default
	assert.Equal(t, "30 5 * * *", result.Value)
	assert.NotEmpty(t, result.Warnings)
	assert.True(t, result.FallbackApplied)

	// Check warning message
	assert.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "Invalid TEST_CRON='invalid format'")
	assert.Contains(t, result.Warnings[0], "falling back to default '30 5 * * *'")
}

func TestLoadEnvWithFallback_InvalidTimezone(t *testing.T) {
	t.Setenv("TEST_TZ", "Invalid/Timezone")

	result := LoadEnvWithFallback("TEST_TZ", "Asia/Tokyo", ValidateTimezone)

	// Should fallback to default
	assert.Equal(t, "Asia/Tokyo", result.Value)
	assert.NotEmpty(t, result.Warnings)
	assert.True(t, result.FallbackApplied)

	// Check warning message
	assert.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "Invalid TEST_TZ='Invalid/Timezone'")
	assert.Contains(t, result.Warnings[0], "falling back to default 'Asia/Tokyo'")
}

// ============================================================================
// Test Group 4: LoadEnvDuration - Basic Loading
// ============================================================================

func TestLoadEnvDuration_WithValidValue(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "1h")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, ValidatePositiveDuration)

	assert.Equal(t, 1*time.Hour, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvDuration_WithoutValue(t *testing.T) {
	// Don't set TEST_TIMEOUT

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, ValidatePositiveDuration)

	assert.Equal(t, 30*time.Minute, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvDuration_EmptyValue(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, ValidatePositiveDuration)

	// Empty value should use default without warning
	assert.Equal(t, 30*time.Minute, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvDuration_NoValidator(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "5m")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, nil)

	// Without validator, any valid duration should be accepted
	assert.Equal(t, 5*time.Minute, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

// ============================================================================
// Test Group 5: LoadEnvDuration - Parse Error and Fallback
// ============================================================================

func TestLoadEnvDuration_InvalidFormat(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "not-a-duration")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, ValidatePositiveDuration)

	// Should fallback to default
	assert.Equal(t, 30*time.Minute, result.Value)
	assert.NotEmpty(t, result.Warnings)
	assert.True(t, result.FallbackApplied)

	// Check warning message
	assert.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "Invalid TEST_TIMEOUT='not-a-duration'")
	assert.Contains(t, result.Warnings[0], "falling back to default '30m0s'")
}

// ============================================================================
// Test Group 6: LoadEnvDuration - Validation Failure and Fallback
// ============================================================================

func TestLoadEnvDuration_NegativeDuration(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "-30m")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, ValidatePositiveDuration)

	// Should fallback to default
	assert.Equal(t, 30*time.Minute, result.Value)
	assert.NotEmpty(t, result.Warnings)
	assert.True(t, result.FallbackApplied)

	// Check warning message
	assert.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "Invalid TEST_TIMEOUT='-30m'")
	assert.Contains(t, result.Warnings[0], "falling back to default '30m0s'")
}

func TestLoadEnvDuration_ZeroDuration(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "0s")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, ValidatePositiveDuration)

	// Should fallback to default (zero is not positive)
	assert.Equal(t, 30*time.Minute, result.Value)
	assert.NotEmpty(t, result.Warnings)
	assert.True(t, result.FallbackApplied)
}

func TestLoadEnvDuration_WithRangeValidator(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "10h")

	validator := func(d time.Duration) error {
		return ValidateDuration(d, 1*time.Minute, 2*time.Hour)
	}

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, validator)

	// Should fallback to default (10h exceeds max 2h)
	assert.Equal(t, 30*time.Minute, result.Value)
	assert.NotEmpty(t, result.Warnings)
	assert.True(t, result.FallbackApplied)

	// Check warning message
	assert.Contains(t, result.Warnings[0], "exceeds maximum")
}

// ============================================================================
// Test Group 7: LoadEnvInt - Basic Loading
// ============================================================================

func TestLoadEnvInt_WithValidValue(t *testing.T) {
	t.Setenv("TEST_PORT", "8080")

	result := LoadEnvInt("TEST_PORT", 9090, func(v int) error {
		return ValidateIntRange(v, 1024, 65535)
	})

	assert.Equal(t, 8080, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvInt_WithoutValue(t *testing.T) {
	// Don't set TEST_PORT

	result := LoadEnvInt("TEST_PORT", 9090, func(v int) error {
		return ValidateIntRange(v, 1024, 65535)
	})

	assert.Equal(t, 9090, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvInt_EmptyValue(t *testing.T) {
	t.Setenv("TEST_PORT", "")

	result := LoadEnvInt("TEST_PORT", 9090, func(v int) error {
		return ValidateIntRange(v, 1024, 65535)
	})

	// Empty value should use default without warning
	assert.Equal(t, 9090, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvInt_NoValidator(t *testing.T) {
	t.Setenv("TEST_COUNT", "42")

	result := LoadEnvInt("TEST_COUNT", 10, nil)

	// Without validator, any valid integer should be accepted
	assert.Equal(t, 42, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvInt_NegativeValue(t *testing.T) {
	t.Setenv("TEST_RETRIES", "-5")

	result := LoadEnvInt("TEST_RETRIES", 3, nil)

	// Negative integers are valid integers (parsing succeeds)
	assert.Equal(t, -5, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvInt_ZeroValue(t *testing.T) {
	t.Setenv("TEST_COUNT", "0")

	result := LoadEnvInt("TEST_COUNT", 10, nil)

	// Zero is a valid integer
	assert.Equal(t, 0, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

// ============================================================================
// Test Group 8: LoadEnvInt - Parse Error and Fallback
// ============================================================================

func TestLoadEnvInt_InvalidFormat(t *testing.T) {
	t.Setenv("TEST_PORT", "not-a-number")

	result := LoadEnvInt("TEST_PORT", 9090, func(v int) error {
		return ValidateIntRange(v, 1024, 65535)
	})

	// Should fallback to default
	assert.Equal(t, 9090, result.Value)
	assert.NotEmpty(t, result.Warnings)
	assert.True(t, result.FallbackApplied)

	// Check warning message
	assert.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "Invalid TEST_PORT='not-a-number'")
	assert.Contains(t, result.Warnings[0], "invalid integer format")
	assert.Contains(t, result.Warnings[0], "falling back to default '9090'")
}

func TestLoadEnvInt_DecimalFormat(t *testing.T) {
	t.Setenv("TEST_COUNT", "10.5")

	result := LoadEnvInt("TEST_COUNT", 100, nil)

	// fmt.Sscanf will parse "10" from "10.5" (stops at decimal point)
	// This is expected behavior of fmt.Sscanf
	assert.Equal(t, 10, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvInt_WithSpaces(t *testing.T) {
	t.Setenv("TEST_COUNT", " 42 ")

	result := LoadEnvInt("TEST_COUNT", 10, nil)

	// fmt.Sscanf will skip leading/trailing whitespace and parse successfully
	// This is expected behavior of fmt.Sscanf
	assert.Equal(t, 42, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

// ============================================================================
// Test Group 9: LoadEnvInt - Validation Failure and Fallback
// ============================================================================

func TestLoadEnvInt_BelowMinimum(t *testing.T) {
	t.Setenv("TEST_PORT", "100")

	result := LoadEnvInt("TEST_PORT", 9090, func(v int) error {
		return ValidateIntRange(v, 1024, 65535)
	})

	// Should fallback to default (100 < 1024)
	assert.Equal(t, 9090, result.Value)
	assert.NotEmpty(t, result.Warnings)
	assert.True(t, result.FallbackApplied)

	// Check warning message
	assert.Contains(t, result.Warnings[0], "below minimum")
}

func TestLoadEnvInt_AboveMaximum(t *testing.T) {
	t.Setenv("TEST_PORT", "70000")

	result := LoadEnvInt("TEST_PORT", 9090, func(v int) error {
		return ValidateIntRange(v, 1024, 65535)
	})

	// Should fallback to default (70000 > 65535)
	assert.Equal(t, 9090, result.Value)
	assert.NotEmpty(t, result.Warnings)
	assert.True(t, result.FallbackApplied)

	// Check warning message
	assert.Contains(t, result.Warnings[0], "exceeds maximum")
}

// ============================================================================
// Test Group 10: LoadEnvBool - Basic Loading
// ============================================================================

func TestLoadEnvBool_TrueValues(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{"1", "1"},
		{"t", "t"},
		{"T", "T"},
		{"true", "true"},
		{"TRUE", "TRUE"},
		{"True", "True"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_BOOL", tc.value)

			result := LoadEnvBool("TEST_BOOL", false)

			assert.Equal(t, true, result.Value)
			assert.Empty(t, result.Warnings)
			assert.False(t, result.FallbackApplied)
		})
	}
}

func TestLoadEnvBool_FalseValues(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{"0", "0"},
		{"f", "f"},
		{"F", "F"},
		{"false", "false"},
		{"FALSE", "FALSE"},
		{"False", "False"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_BOOL", tc.value)

			result := LoadEnvBool("TEST_BOOL", true)

			assert.Equal(t, false, result.Value)
			assert.Empty(t, result.Warnings)
			assert.False(t, result.FallbackApplied)
		})
	}
}

func TestLoadEnvBool_WithoutValue(t *testing.T) {
	// Don't set TEST_BOOL

	result := LoadEnvBool("TEST_BOOL", true)

	assert.Equal(t, true, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvBool_EmptyValue(t *testing.T) {
	t.Setenv("TEST_BOOL", "")

	result := LoadEnvBool("TEST_BOOL", true)

	// Empty value should use default without warning
	assert.Equal(t, true, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

// ============================================================================
// Test Group 11: LoadEnvBool - Invalid Format and Fallback
// ============================================================================

func TestLoadEnvBool_InvalidFormat(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{"yes", "yes"},
		{"no", "no"},
		{"on", "on"},
		{"off", "off"},
		{"2", "2"},
		{"invalid", "invalid"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_BOOL", tc.value)

			result := LoadEnvBool("TEST_BOOL", true)

			// Should fallback to default
			assert.Equal(t, true, result.Value)
			assert.NotEmpty(t, result.Warnings)
			assert.True(t, result.FallbackApplied)

			// Check warning message
			assert.Len(t, result.Warnings, 1)
			assert.Contains(t, result.Warnings[0], "Invalid TEST_BOOL='"+tc.value+"'")
			assert.Contains(t, result.Warnings[0], "invalid boolean format")
			assert.Contains(t, result.Warnings[0], "falling back to default 'true'")
		})
	}
}

// ============================================================================
// Test Group 12: Edge Cases and Complex Scenarios
// ============================================================================

func TestLoadEnvDuration_VeryLongDuration(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "24h")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, ValidatePositiveDuration)

	// 24h is valid and positive
	assert.Equal(t, 24*time.Hour, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvDuration_VeryShortDuration(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "1s")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, ValidatePositiveDuration)

	// 1s is valid and positive
	assert.Equal(t, 1*time.Second, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvDuration_Nanoseconds(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "500ns")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, ValidatePositiveDuration)

	// 500ns is valid and positive
	assert.Equal(t, 500*time.Nanosecond, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvDuration_CompoundDuration(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "1h30m45s")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, nil)

	// Compound duration should parse correctly
	expected := 1*time.Hour + 30*time.Minute + 45*time.Second
	assert.Equal(t, expected, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvInt_VeryLargeNumber(t *testing.T) {
	t.Setenv("TEST_COUNT", "2147483647") // Max int32

	result := LoadEnvInt("TEST_COUNT", 100, nil)

	assert.Equal(t, 2147483647, result.Value)
	assert.Empty(t, result.Warnings)
	assert.False(t, result.FallbackApplied)
}

func TestLoadEnvWithFallback_ComplexCronExpression(t *testing.T) {
	testCases := []struct {
		name     string
		schedule string
	}{
		{"yearly", "0 0 1 1 *"},
		{"monthly", "0 0 1 * *"},
		{"weekly", "0 0 * * 0"},
		{"daily", "0 0 * * *"},
		{"hourly", "0 * * * *"},
		{"every 5 minutes", "*/5 * * * *"},
		{"weekdays at 9am", "0 9 * * 1-5"},
		{"weekend at noon", "0 12 * * 6,0"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_CRON", tc.schedule)

			result := LoadEnvWithFallback("TEST_CRON", "30 5 * * *", ValidateCronSchedule)

			assert.Equal(t, tc.schedule, result.Value)
			assert.Empty(t, result.Warnings)
			assert.False(t, result.FallbackApplied)
		})
	}
}

func TestLoadEnvWithFallback_VariousTimezones(t *testing.T) {
	testCases := []struct {
		name     string
		timezone string
	}{
		{"UTC", "UTC"},
		{"New York", "America/New_York"},
		{"London", "Europe/London"},
		{"Tokyo", "Asia/Tokyo"},
		{"Sydney", "Australia/Sydney"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TEST_TZ", tc.timezone)

			result := LoadEnvWithFallback("TEST_TZ", "UTC", ValidateTimezone)

			assert.Equal(t, tc.timezone, result.Value)
			assert.Empty(t, result.Warnings)
			assert.False(t, result.FallbackApplied)
		})
	}
}

// ============================================================================
// Test Group 13: Multiple Fallbacks Scenario
// ============================================================================

func TestMultipleFallbacks_Simulation(t *testing.T) {
	// Simulate loading multiple configuration values with some failures
	t.Setenv("CRON_SCHEDULE", "invalid")
	t.Setenv("TZ", "Invalid/Zone")
	t.Setenv("CRAWL_TIMEOUT", "-5m")

	var allWarnings []string
	fallbackCount := 0

	// Load cron schedule
	cronResult := LoadEnvWithFallback("CRON_SCHEDULE", "30 5 * * *", ValidateCronSchedule)
	if cronResult.FallbackApplied {
		fallbackCount++
		allWarnings = append(allWarnings, cronResult.Warnings...)
	}

	// Load timezone
	tzResult := LoadEnvWithFallback("TZ", "Asia/Tokyo", ValidateTimezone)
	if tzResult.FallbackApplied {
		fallbackCount++
		allWarnings = append(allWarnings, tzResult.Warnings...)
	}

	// Load timeout
	timeoutResult := LoadEnvDuration("CRAWL_TIMEOUT", 30*time.Minute, ValidatePositiveDuration)
	if timeoutResult.FallbackApplied {
		fallbackCount++
		allWarnings = append(allWarnings, timeoutResult.Warnings...)
	}

	// Verify all three fallbacks were applied
	assert.Equal(t, 3, fallbackCount)
	assert.Len(t, allWarnings, 3)

	// Verify default values were used
	assert.Equal(t, "30 5 * * *", cronResult.Value)
	assert.Equal(t, "Asia/Tokyo", tzResult.Value)
	assert.Equal(t, 30*time.Minute, timeoutResult.Value)
}

// ============================================================================
// Test Group 14: Type Assertion Verification
// ============================================================================

func TestConfigLoadResult_TypeAssertion_String(t *testing.T) {
	t.Setenv("TEST_STRING", "test_value")

	result := LoadEnvWithFallback("TEST_STRING", "default", nil)

	// Type assertion should work
	value, ok := result.Value.(string)
	assert.True(t, ok)
	assert.Equal(t, "test_value", value)
}

func TestConfigLoadResult_TypeAssertion_Duration(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "1h")

	result := LoadEnvDuration("TEST_TIMEOUT", 30*time.Minute, nil)

	// Type assertion should work
	value, ok := result.Value.(time.Duration)
	assert.True(t, ok)
	assert.Equal(t, 1*time.Hour, value)
}

func TestConfigLoadResult_TypeAssertion_Int(t *testing.T) {
	t.Setenv("TEST_PORT", "8080")

	result := LoadEnvInt("TEST_PORT", 9090, nil)

	// Type assertion should work
	value, ok := result.Value.(int)
	assert.True(t, ok)
	assert.Equal(t, 8080, value)
}

func TestConfigLoadResult_TypeAssertion_Bool(t *testing.T) {
	t.Setenv("TEST_BOOL", "true")

	result := LoadEnvBool("TEST_BOOL", false)

	// Type assertion should work
	value, ok := result.Value.(bool)
	assert.True(t, ok)
	assert.Equal(t, true, value)
}
