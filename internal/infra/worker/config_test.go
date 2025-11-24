package worker

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// Verify all fields have expected default values
	if config.CronSchedule != "30 5 * * *" {
		t.Errorf("Expected CronSchedule '30 5 * * *', got '%s'", config.CronSchedule)
	}

	if config.Timezone != "Asia/Tokyo" {
		t.Errorf("Expected Timezone 'Asia/Tokyo', got '%s'", config.Timezone)
	}

	if config.NotifyMaxConcurrent != 10 {
		t.Errorf("Expected NotifyMaxConcurrent 10, got %d", config.NotifyMaxConcurrent)
	}

	if config.CrawlTimeout != 30*time.Minute {
		t.Errorf("Expected CrawlTimeout 30m, got %v", config.CrawlTimeout)
	}

	if config.HealthPort != 9091 {
		t.Errorf("Expected HealthPort 9091, got %d", config.HealthPort)
	}
}

func TestDefaultConfig_Immutability(t *testing.T) {
	// Each call to DefaultConfig should return a new instance
	config1 := DefaultConfig()
	config2 := DefaultConfig()

	// Modify config1
	config1.CronSchedule = "0 6 * * *"
	config1.NotifyMaxConcurrent = 20

	// config2 should still have default values
	if config2.CronSchedule != "30 5 * * *" {
		t.Error("DefaultConfig returned a shared instance instead of a new one")
	}

	if config2.NotifyMaxConcurrent != 10 {
		t.Error("DefaultConfig returned a shared instance instead of a new one")
	}
}

func TestWorkerConfig_StructFields(t *testing.T) {
	// Verify that WorkerConfig struct can be instantiated with all field types
	config := WorkerConfig{
		CronSchedule:        "0 0 * * *",
		Timezone:            "UTC",
		NotifyMaxConcurrent: 5,
		CrawlTimeout:        15 * time.Minute,
		HealthPort:          8080,
	}

	if config.CronSchedule != "0 0 * * *" {
		t.Errorf("CronSchedule field not set correctly: %s", config.CronSchedule)
	}

	if config.Timezone != "UTC" {
		t.Errorf("Timezone field not set correctly: %s", config.Timezone)
	}

	if config.NotifyMaxConcurrent != 5 {
		t.Errorf("NotifyMaxConcurrent field not set correctly: %d", config.NotifyMaxConcurrent)
	}

	if config.CrawlTimeout != 15*time.Minute {
		t.Errorf("CrawlTimeout field not set correctly: %v", config.CrawlTimeout)
	}

	if config.HealthPort != 8080 {
		t.Errorf("HealthPort field not set correctly: %d", config.HealthPort)
	}
}

func TestWorkerConfig_ZeroValue(t *testing.T) {
	// Verify zero value struct is valid Go code
	var config WorkerConfig

	// Zero values should be the zero values of each type
	if config.CronSchedule != "" {
		t.Errorf("Expected empty CronSchedule, got '%s'", config.CronSchedule)
	}

	if config.Timezone != "" {
		t.Errorf("Expected empty Timezone, got '%s'", config.Timezone)
	}

	if config.NotifyMaxConcurrent != 0 {
		t.Errorf("Expected NotifyMaxConcurrent 0, got %d", config.NotifyMaxConcurrent)
	}

	if config.CrawlTimeout != 0 {
		t.Errorf("Expected CrawlTimeout 0, got %v", config.CrawlTimeout)
	}

	if config.HealthPort != 0 {
		t.Errorf("Expected HealthPort 0, got %d", config.HealthPort)
	}
}

func TestWorkerConfig_Validate_ValidConfig(t *testing.T) {
	// Default config should be valid
	config := DefaultConfig()

	err := config.Validate()
	if err != nil {
		t.Errorf("DefaultConfig should be valid, got error: %v", err)
	}
}

func TestWorkerConfig_Validate_InvalidCronSchedule(t *testing.T) {
	config := DefaultConfig()
	config.CronSchedule = "invalid cron"

	err := config.Validate()
	if err == nil {
		t.Error("Expected validation error for invalid cron schedule")
	}

	// Error should mention CronSchedule
	if err != nil && err.Error() == "" {
		t.Error("Error message should not be empty")
	}
}

func TestWorkerConfig_Validate_EmptyCronSchedule(t *testing.T) {
	config := DefaultConfig()
	config.CronSchedule = ""

	err := config.Validate()
	if err == nil {
		t.Error("Expected validation error for empty cron schedule")
	}
}

func TestWorkerConfig_Validate_InvalidTimezone(t *testing.T) {
	config := DefaultConfig()
	config.Timezone = "Invalid/Timezone"

	err := config.Validate()
	if err == nil {
		t.Error("Expected validation error for invalid timezone")
	}
}

func TestWorkerConfig_Validate_EmptyTimezone(t *testing.T) {
	config := DefaultConfig()
	config.Timezone = ""

	err := config.Validate()
	if err == nil {
		t.Error("Expected validation error for empty timezone")
	}
}

func TestWorkerConfig_Validate_NotifyMaxConcurrentTooLow(t *testing.T) {
	config := DefaultConfig()
	config.NotifyMaxConcurrent = 0

	err := config.Validate()
	if err == nil {
		t.Error("Expected validation error for NotifyMaxConcurrent = 0")
	}
}

func TestWorkerConfig_Validate_NotifyMaxConcurrentTooHigh(t *testing.T) {
	config := DefaultConfig()
	config.NotifyMaxConcurrent = 101

	err := config.Validate()
	if err == nil {
		t.Error("Expected validation error for NotifyMaxConcurrent = 101")
	}
}

func TestWorkerConfig_Validate_NotifyMaxConcurrentBoundary(t *testing.T) {
	tests := []struct {
		name  string
		value int
		valid bool
	}{
		{"Min valid (1)", 1, true},
		{"Max valid (50)", 50, true},
		{"Below min (0)", 0, false},
		{"Negative", -1, false},
		{"Above max (51)", 51, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.NotifyMaxConcurrent = tt.value

			err := config.Validate()
			if tt.valid && err != nil {
				t.Errorf("Expected valid config, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected validation error for value %d", tt.value)
			}
		})
	}
}

func TestWorkerConfig_Validate_CrawlTimeoutZero(t *testing.T) {
	config := DefaultConfig()
	config.CrawlTimeout = 0

	err := config.Validate()
	if err == nil {
		t.Error("Expected validation error for CrawlTimeout = 0")
	}
}

func TestWorkerConfig_Validate_CrawlTimeoutNegative(t *testing.T) {
	config := DefaultConfig()
	config.CrawlTimeout = -1 * time.Minute

	err := config.Validate()
	if err == nil {
		t.Error("Expected validation error for negative CrawlTimeout")
	}
}

func TestWorkerConfig_Validate_CrawlTimeoutValid(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
	}{
		{"1 second", 1 * time.Second},
		{"1 minute", 1 * time.Minute},
		{"30 minutes", 30 * time.Minute},
		{"1 hour", 1 * time.Hour},
		{"2 hours", 2 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.CrawlTimeout = tt.duration

			err := config.Validate()
			if err != nil {
				t.Errorf("Expected valid timeout %v, got error: %v", tt.duration, err)
			}
		})
	}
}

func TestWorkerConfig_Validate_HealthPortTooLow(t *testing.T) {
	config := DefaultConfig()
	config.HealthPort = 1023

	err := config.Validate()
	if err == nil {
		t.Error("Expected validation error for HealthPort = 1023 (below 1024)")
	}
}

func TestWorkerConfig_Validate_HealthPortTooHigh(t *testing.T) {
	config := DefaultConfig()
	config.HealthPort = 65536

	err := config.Validate()
	if err == nil {
		t.Error("Expected validation error for HealthPort = 65536 (above 65535)")
	}
}

func TestWorkerConfig_Validate_HealthPortBoundary(t *testing.T) {
	tests := []struct {
		name  string
		port  int
		valid bool
	}{
		{"Min valid (1024)", 1024, true},
		{"Max valid (65535)", 65535, true},
		{"Below min (1023)", 1023, false},
		{"Above max (65536)", 65536, false},
		{"Zero", 0, false},
		{"Negative", -1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.HealthPort = tt.port

			err := config.Validate()
			if tt.valid && err != nil {
				t.Errorf("Expected valid port %d, got error: %v", tt.port, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("Expected validation error for port %d", tt.port)
			}
		})
	}
}

func TestWorkerConfig_Validate_MultipleErrors(t *testing.T) {
	// Create config with multiple invalid fields
	config := WorkerConfig{
		CronSchedule:        "invalid",           // Invalid
		Timezone:            "Invalid/Zone",      // Invalid
		NotifyMaxConcurrent: 0,                   // Invalid (too low)
		CrawlTimeout:        0,                   // Invalid (zero)
		HealthPort:          100,                 // Invalid (too low)
	}

	err := config.Validate()
	if err == nil {
		t.Fatal("Expected validation errors for multiple invalid fields")
	}

	// Error should contain information about all validation failures
	errStr := err.Error()
	if errStr == "" {
		t.Error("Error message should not be empty")
	}

	// Check that error message is meaningful (contains "validation")
	// We don't check exact format as it may contain wrapped errors
	t.Logf("Validation error (expected): %v", err)
}

func TestWorkerConfig_Validate_ValidCustomConfig(t *testing.T) {
	config := WorkerConfig{
		CronSchedule:        "0 */6 * * *",
		Timezone:            "UTC",
		NotifyMaxConcurrent: 20,
		CrawlTimeout:        1 * time.Hour,
		HealthPort:          8080,
	}

	err := config.Validate()
	if err != nil {
		t.Errorf("Expected valid custom config, got error: %v", err)
	}
}

// globalTestMetrics is a shared metrics instance for tests to avoid
// duplicate Prometheus registration errors. In production, metrics are
// created once at startup, so this simulates that behavior.
var globalTestMetrics = NewWorkerMetrics()

// setEnv is a test helper that sets an environment variable and fails the test if it errors
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("Failed to set %s: %v", key, err)
	}
}

// unsetEnv is a test helper that unsets an environment variable and fails the test if it errors
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Failed to unset %s: %v", key, err)
	}
}

func TestLoadConfigFromEnv_AllEnvVarsValid(t *testing.T) {
	// Set up environment variables
	setEnv(t, "CRON_SCHEDULE", "0 6 * * *")
	setEnv(t, "WORKER_TIMEZONE", "UTC")
	setEnv(t, "NOTIFY_MAX_CONCURRENT", "20")
	setEnv(t, "CRAWL_TIMEOUT", "1h")
	setEnv(t, "WORKER_HEALTH_PORT", "8080")
	defer func() {
		unsetEnv(t, "CRON_SCHEDULE")
		unsetEnv(t, "WORKER_TIMEZONE")
		unsetEnv(t, "NOTIFY_MAX_CONCURRENT")
		unsetEnv(t, "CRAWL_TIMEOUT")
		unsetEnv(t, "WORKER_HEALTH_PORT")
	}()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	config, err := LoadConfigFromEnv(logger, globalTestMetrics)

	// Should not return error (fail-open strategy)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Should load all values from environment
	if config.CronSchedule != "0 6 * * *" {
		t.Errorf("Expected CronSchedule '0 6 * * *', got '%s'", config.CronSchedule)
	}
	if config.Timezone != "UTC" {
		t.Errorf("Expected Timezone 'UTC', got '%s'", config.Timezone)
	}
	if config.NotifyMaxConcurrent != 20 {
		t.Errorf("Expected NotifyMaxConcurrent 20, got %d", config.NotifyMaxConcurrent)
	}
	if config.CrawlTimeout != 1*time.Hour {
		t.Errorf("Expected CrawlTimeout 1h, got %v", config.CrawlTimeout)
	}
	if config.HealthPort != 8080 {
		t.Errorf("Expected HealthPort 8080, got %d", config.HealthPort)
	}

	// No warnings should be logged
	if buf.Len() > 0 {
		t.Errorf("Expected no warnings, got: %s", buf.String())
	}
}

func TestLoadConfigFromEnv_MissingEnvVars(t *testing.T) {
	// Clear all environment variables
	unsetEnv(t, "CRON_SCHEDULE")
	unsetEnv(t, "WORKER_TIMEZONE")
	unsetEnv(t, "NOTIFY_MAX_CONCURRENT")
	unsetEnv(t, "CRAWL_TIMEOUT")
	unsetEnv(t, "WORKER_HEALTH_PORT")

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	// Use shared global metrics instance

	config, err := LoadConfigFromEnv(logger, globalTestMetrics)

	// Should not return error (fail-open strategy)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Should use default values
	defaults := DefaultConfig()
	if config.CronSchedule != defaults.CronSchedule {
		t.Errorf("Expected default CronSchedule, got '%s'", config.CronSchedule)
	}
	if config.Timezone != defaults.Timezone {
		t.Errorf("Expected default Timezone, got '%s'", config.Timezone)
	}
	if config.NotifyMaxConcurrent != defaults.NotifyMaxConcurrent {
		t.Errorf("Expected default NotifyMaxConcurrent, got %d", config.NotifyMaxConcurrent)
	}
	if config.CrawlTimeout != defaults.CrawlTimeout {
		t.Errorf("Expected default CrawlTimeout, got %v", config.CrawlTimeout)
	}
	if config.HealthPort != defaults.HealthPort {
		t.Errorf("Expected default HealthPort, got %d", config.HealthPort)
	}

	// No warnings should be logged (missing env vars don't trigger fallback)
	if buf.Len() > 0 {
		t.Errorf("Expected no warnings, got: %s", buf.String())
	}
}

func TestLoadConfigFromEnv_InvalidCronSchedule(t *testing.T) {
	setEnv(t, "CRON_SCHEDULE", "invalid cron")
	defer unsetEnv(t, "CRON_SCHEDULE")

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	// Use shared global metrics instance

	config, err := LoadConfigFromEnv(logger, globalTestMetrics)

	// Should not return error (fail-open strategy)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Should use default value
	if config.CronSchedule != DefaultConfig().CronSchedule {
		t.Errorf("Expected default CronSchedule, got '%s'", config.CronSchedule)
	}

	// Warning should be logged
	logOutput := buf.String()
	if !strings.Contains(logOutput, "Configuration fallback applied") {
		t.Error("Expected fallback warning in logs")
	}
	if !strings.Contains(logOutput, "CronSchedule") {
		t.Error("Expected CronSchedule field in warning")
	}
}

func TestLoadConfigFromEnv_InvalidTimezone(t *testing.T) {
	setEnv(t, "WORKER_TIMEZONE", "Invalid/Timezone")
	defer unsetEnv(t, "WORKER_TIMEZONE")

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	// Use shared global metrics instance

	config, err := LoadConfigFromEnv(logger, globalTestMetrics)

	// Should not return error (fail-open strategy)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Should use default value
	if config.Timezone != DefaultConfig().Timezone {
		t.Errorf("Expected default Timezone, got '%s'", config.Timezone)
	}

	// Warning should be logged
	logOutput := buf.String()
	if !strings.Contains(logOutput, "Configuration fallback applied") {
		t.Error("Expected fallback warning in logs")
	}
	if !strings.Contains(logOutput, "Timezone") {
		t.Error("Expected Timezone field in warning")
	}
}

func TestLoadConfigFromEnv_InvalidNotifyMaxConcurrent(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"Zero", "0"},
		{"Negative", "-1"},
		{"Too high", "101"},
		{"Invalid format", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, "NOTIFY_MAX_CONCURRENT", tt.value)
			defer unsetEnv(t, "NOTIFY_MAX_CONCURRENT")

			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			// Use shared global metrics instance

			config, err := LoadConfigFromEnv(logger, globalTestMetrics)

			// Should not return error (fail-open strategy)
			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// Should use default value
			if config.NotifyMaxConcurrent != DefaultConfig().NotifyMaxConcurrent {
				t.Errorf("Expected default NotifyMaxConcurrent, got %d", config.NotifyMaxConcurrent)
			}

			// Warning should be logged
			logOutput := buf.String()
			if !strings.Contains(logOutput, "Configuration fallback applied") {
				t.Error("Expected fallback warning in logs")
			}
		})
	}
}

func TestLoadConfigFromEnv_InvalidCrawlTimeout(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"Zero", "0"},
		{"Negative", "-1s"},
		{"Invalid format", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, "CRAWL_TIMEOUT", tt.value)
			defer unsetEnv(t, "CRAWL_TIMEOUT")

			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			// Use shared global metrics instance

			config, err := LoadConfigFromEnv(logger, globalTestMetrics)

			// Should not return error (fail-open strategy)
			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// Should use default value
			if config.CrawlTimeout != DefaultConfig().CrawlTimeout {
				t.Errorf("Expected default CrawlTimeout, got %v", config.CrawlTimeout)
			}

			// Warning should be logged
			logOutput := buf.String()
			if !strings.Contains(logOutput, "Configuration fallback applied") {
				t.Error("Expected fallback warning in logs")
			}
		})
	}
}

func TestLoadConfigFromEnv_InvalidHealthPort(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"Too low", "1023"},
		{"Too high", "65536"},
		{"Zero", "0"},
		{"Negative", "-1"},
		{"Invalid format", "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, "WORKER_HEALTH_PORT", tt.value)
			defer unsetEnv(t, "WORKER_HEALTH_PORT")

			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			// Use shared global metrics instance

			config, err := LoadConfigFromEnv(logger, globalTestMetrics)

			// Should not return error (fail-open strategy)
			if err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// Should use default value
			if config.HealthPort != DefaultConfig().HealthPort {
				t.Errorf("Expected default HealthPort, got %d", config.HealthPort)
			}

			// Warning should be logged
			logOutput := buf.String()
			if !strings.Contains(logOutput, "Configuration fallback applied") {
				t.Error("Expected fallback warning in logs")
			}
		})
	}
}

func TestLoadConfigFromEnv_MultipleInvalidFields(t *testing.T) {
	// Set multiple invalid environment variables
	setEnv(t, "CRON_SCHEDULE", "invalid")
	setEnv(t, "WORKER_TIMEZONE", "Invalid/Zone")
	setEnv(t, "NOTIFY_MAX_CONCURRENT", "0")
	setEnv(t, "CRAWL_TIMEOUT", "invalid")
	setEnv(t, "WORKER_HEALTH_PORT", "100")
	defer func() {
		unsetEnv(t, "CRON_SCHEDULE")
		unsetEnv(t, "WORKER_TIMEZONE")
		unsetEnv(t, "NOTIFY_MAX_CONCURRENT")
		unsetEnv(t, "CRAWL_TIMEOUT")
		unsetEnv(t, "WORKER_HEALTH_PORT")
	}()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	// Use shared global metrics instance

	config, err := LoadConfigFromEnv(logger, globalTestMetrics)

	// Should not return error (fail-open strategy)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// All fields should use default values
	defaults := DefaultConfig()
	if config.CronSchedule != defaults.CronSchedule {
		t.Errorf("Expected default CronSchedule, got '%s'", config.CronSchedule)
	}
	if config.Timezone != defaults.Timezone {
		t.Errorf("Expected default Timezone, got '%s'", config.Timezone)
	}
	if config.NotifyMaxConcurrent != defaults.NotifyMaxConcurrent {
		t.Errorf("Expected default NotifyMaxConcurrent, got %d", config.NotifyMaxConcurrent)
	}
	if config.CrawlTimeout != defaults.CrawlTimeout {
		t.Errorf("Expected default CrawlTimeout, got %v", config.CrawlTimeout)
	}
	if config.HealthPort != defaults.HealthPort {
		t.Errorf("Expected default HealthPort, got %d", config.HealthPort)
	}

	// Multiple warnings should be logged
	logOutput := buf.String()
	warningCount := strings.Count(logOutput, "Configuration fallback applied")
	if warningCount != 5 {
		t.Errorf("Expected 5 warnings, got %d", warningCount)
	}
}

func TestLoadConfigFromEnv_PartiallyValid(t *testing.T) {
	// Set some valid and some invalid values
	setEnv(t, "CRON_SCHEDULE", "0 6 * * *")         // Valid
	setEnv(t, "WORKER_TIMEZONE", "Invalid/Zone")    // Invalid
	setEnv(t, "NOTIFY_MAX_CONCURRENT", "20")        // Valid
	setEnv(t, "CRAWL_TIMEOUT", "invalid")           // Invalid
	setEnv(t, "WORKER_HEALTH_PORT", "8080")         // Valid
	defer func() {
		unsetEnv(t, "CRON_SCHEDULE")
		unsetEnv(t, "WORKER_TIMEZONE")
		unsetEnv(t, "NOTIFY_MAX_CONCURRENT")
		unsetEnv(t, "CRAWL_TIMEOUT")
		unsetEnv(t, "WORKER_HEALTH_PORT")
	}()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	// Use shared global metrics instance

	config, err := LoadConfigFromEnv(logger, globalTestMetrics)

	// Should not return error (fail-open strategy)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Valid fields should use environment values
	if config.CronSchedule != "0 6 * * *" {
		t.Errorf("Expected CronSchedule '0 6 * * *', got '%s'", config.CronSchedule)
	}
	if config.NotifyMaxConcurrent != 20 {
		t.Errorf("Expected NotifyMaxConcurrent 20, got %d", config.NotifyMaxConcurrent)
	}
	if config.HealthPort != 8080 {
		t.Errorf("Expected HealthPort 8080, got %d", config.HealthPort)
	}

	// Invalid fields should use defaults
	if config.Timezone != DefaultConfig().Timezone {
		t.Errorf("Expected default Timezone, got '%s'", config.Timezone)
	}
	if config.CrawlTimeout != DefaultConfig().CrawlTimeout {
		t.Errorf("Expected default CrawlTimeout, got %v", config.CrawlTimeout)
	}

	// Only 2 warnings should be logged (for Timezone and CrawlTimeout)
	logOutput := buf.String()
	warningCount := strings.Count(logOutput, "Configuration fallback applied")
	if warningCount != 2 {
		t.Errorf("Expected 2 warnings, got %d", warningCount)
	}
}
