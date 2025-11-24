package worker

import (
	"catchup-feed/internal/pkg/config"
	"fmt"
	"log/slog"
	"time"
)

// WorkerConfig holds the configuration for the worker component.
// This configuration controls the cron schedule, timezone, notification settings,
// and other operational parameters for the worker service.
//
// Configuration sources:
//   - Environment variables (loaded via LoadConfigFromEnv)
//   - Default values (provided by DefaultConfig)
//
// All fields have sensible defaults and validation rules to ensure
// the worker can operate safely even with invalid or missing configuration.
//
// Example usage:
//
//	// Use defaults
//	config := DefaultConfig()
//
//	// Load from environment with fallback
//	config, err := LoadConfigFromEnv(logger, metrics)
//	if err != nil {
//	    // This should never happen with fail-open strategy
//	    log.Fatal("Unexpected configuration error: %v", err)
//	}
//
//	// Validate before use (optional, LoadConfigFromEnv already validates)
//	if err := config.Validate(); err != nil {
//	    log.Fatal("Invalid configuration: %v", err)
//	}
type WorkerConfig struct {
	// CronSchedule is the cron expression for job scheduling.
	// Format: "minute hour day month weekday"
	// Example: "30 5 * * *" (every day at 5:30)
	// Validation: Must be a valid cron expression (5 fields)
	// Default: "30 5 * * *"
	CronSchedule string

	// Timezone is the IANA timezone name for cron scheduling.
	// Example: "Asia/Tokyo", "UTC", "America/New_York"
	// Validation: Must be a valid IANA timezone name
	// Default: "Asia/Tokyo"
	Timezone string

	// NotifyMaxConcurrent is the maximum number of concurrent notification operations.
	// This controls how many notification channels can be called simultaneously.
	// Range: 1-100
	// Default: 10
	NotifyMaxConcurrent int

	// CrawlTimeout is the maximum duration for a single crawl job.
	// After this timeout, the crawl operation will be cancelled.
	// Must be positive (> 0)
	// Default: 30 minutes
	CrawlTimeout time.Duration

	// HealthPort is the port number for the health check HTTP server.
	// Range: 1024-65535 (avoid privileged ports)
	// Default: 9091
	HealthPort int
}

// DefaultConfig returns a WorkerConfig with sensible default values.
// These defaults are optimized for:
//   - Typical usage: Daily crawl at 5:30 AM JST
//   - Safety: 30-minute timeout prevents stuck jobs
//   - Performance: 10 concurrent notifications balances throughput and resources
//   - Standard ports: 9091 for health checks (common Prometheus exporter port)
//
// Returns:
//   - WorkerConfig with production-ready default values
//
// Example:
//
//	config := DefaultConfig()
//	config.CronSchedule = "0 */6 * * *"  // Customize to run every 6 hours
func DefaultConfig() WorkerConfig {
	return WorkerConfig{
		CronSchedule:        "30 5 * * *",      // Every day at 5:30 AM
		Timezone:            "Asia/Tokyo",      // JST
		NotifyMaxConcurrent: 10,                // 10 concurrent notifications
		CrawlTimeout:        30 * time.Minute,  // 30 minutes
		HealthPort:          9091,              // Standard Prometheus exporter port
	}
}

// Validate checks if the configuration values are valid.
// This method validates each field using the reusable validators from internal/pkg/config.
// If multiple fields are invalid, all errors are collected and returned together.
//
// Validation rules:
//   - CronSchedule: Must be a valid cron expression (validated by robfig/cron parser)
//   - Timezone: Must be a valid IANA timezone name (validated by time.LoadLocation)
//   - NotifyMaxConcurrent: Must be between 1 and 100 (inclusive)
//   - CrawlTimeout: Must be positive (> 0)
//   - HealthPort: Must be between 1024 and 65535 (avoid privileged ports)
//
// Returns:
//   - error: nil if configuration is valid, aggregated error if any validation fails
//
// Example:
//
//	config := DefaultConfig()
//	if err := config.Validate(); err != nil {
//	    log.Fatal("Invalid configuration: %v", err)
//	}
//
//	// Invalid configuration
//	config.CronSchedule = "invalid"
//	config.NotifyMaxConcurrent = 0
//	err := config.Validate()
//	// err contains: "validation errors: [invalid cron schedule, NotifyMaxConcurrent out of range]"
func (c *WorkerConfig) Validate() error {
	var errors []error

	// Validate CronSchedule
	if err := config.ValidateCronSchedule(c.CronSchedule); err != nil {
		errors = append(errors, fmt.Errorf("cron schedule: %w", err))
	}

	// Validate Timezone
	if err := config.ValidateTimezone(c.Timezone); err != nil {
		errors = append(errors, fmt.Errorf("timezone: %w", err))
	}

	// Validate NotifyMaxConcurrent (range: 1-50, reduced for safety)
	if err := config.ValidateIntRange(c.NotifyMaxConcurrent, 1, 50); err != nil {
		errors = append(errors, fmt.Errorf("notify max concurrent: %w", err))
	}

	// Validate CrawlTimeout (must be positive)
	if err := config.ValidatePositiveDuration(c.CrawlTimeout); err != nil {
		errors = append(errors, fmt.Errorf("crawl timeout: %w", err))
	}

	// Validate HealthPort (range: 1024-65535)
	if err := config.ValidateIntRange(c.HealthPort, 1024, 65535); err != nil {
		errors = append(errors, fmt.Errorf("health port: %w", err))
	}

	// Return aggregated errors
	if len(errors) > 0 {
		return fmt.Errorf("validation failed: %v", errors)
	}

	return nil
}

// LoadConfigFromEnv loads worker configuration from environment variables
// with validation and automatic fallback to default values on failure.
//
// This function implements the fail-open strategy:
//  1. Start with DefaultConfig() as base
//  2. Load each field from environment variables
//  3. Validate each loaded value
//  4. If validation fails: use default value, log warning, increment metrics
//  5. Never return error - always return a valid configuration
//
// Environment variables:
//   - CRON_SCHEDULE: Cron expression (default: "30 5 * * *")
//   - WORKER_TIMEZONE: IANA timezone name (default: "Asia/Tokyo")
//   - NOTIFY_MAX_CONCURRENT: Integer 1-100 (default: 10)
//   - CRAWL_TIMEOUT: Duration string, e.g., "30m" (default: 30 minutes)
//   - WORKER_HEALTH_PORT: Integer 1024-65535 (default: 9091)
//
// Metrics updated:
//   - ValidationErrorsTotal: Incremented for each validation failure
//   - FallbacksTotal: Incremented for each fallback applied
//   - FallbackActive: Set to 1 if any fallback is active, 0 otherwise
//   - LoadTimestamp: Set to current time after successful load
//
// Parameters:
//   - logger: Structured logger for warnings
//   - metrics: Metrics instance for tracking fallbacks
//
// Returns:
//   - *WorkerConfig: Valid configuration (never nil)
//   - error: Always nil (fail-open strategy)
//
// Example:
//
//	logger := slog.Default()
//	metrics := NewWorkerMetrics()
//	config, _ := LoadConfigFromEnv(logger, metrics)
//	// config is always valid and ready to use
//
// Warning log format:
//
//	logger.Warn("Configuration fallback applied",
//	    slog.String("field", "CronSchedule"),
//	    slog.String("env_key", "CRON_SCHEDULE"),
//	    slog.String("invalid_value", "bad cron"),
//	    slog.String("default_value", "30 5 * * *"),
//	    slog.String("error", "validation error message"))
func LoadConfigFromEnv(logger *slog.Logger, metrics *WorkerMetrics) (*WorkerConfig, error) {
	// Start with default config
	cfg := DefaultConfig()
	fallbackApplied := false

	// Load CronSchedule
	result := config.LoadEnvWithFallback("CRON_SCHEDULE", cfg.CronSchedule, config.ValidateCronSchedule)
	cfg.CronSchedule = result.Value.(string)
	if result.FallbackApplied {
		fallbackApplied = true
		metrics.RecordValidationError("cron_schedule")
		metrics.RecordFallback("cron_schedule", "default")
		for _, warning := range result.Warnings {
			logger.Warn("Configuration fallback applied",
				slog.String("field", "CronSchedule"),
				slog.String("warning", warning))
		}
	}

	// Load Timezone
	result = config.LoadEnvWithFallback("WORKER_TIMEZONE", cfg.Timezone, config.ValidateTimezone)
	cfg.Timezone = result.Value.(string)
	if result.FallbackApplied {
		fallbackApplied = true
		metrics.RecordValidationError("timezone")
		metrics.RecordFallback("timezone", "default")
		for _, warning := range result.Warnings {
			logger.Warn("Configuration fallback applied",
				slog.String("field", "Timezone"),
				slog.String("warning", warning))
		}
	}

	// Load NotifyMaxConcurrent
	result = config.LoadEnvInt("NOTIFY_MAX_CONCURRENT", cfg.NotifyMaxConcurrent, func(v int) error {
		return config.ValidateIntRange(v, 1, 100)
	})
	cfg.NotifyMaxConcurrent = result.Value.(int)
	if result.FallbackApplied {
		fallbackApplied = true
		metrics.RecordValidationError("notify_max_concurrent")
		metrics.RecordFallback("notify_max_concurrent", "default")
		for _, warning := range result.Warnings {
			logger.Warn("Configuration fallback applied",
				slog.String("field", "NotifyMaxConcurrent"),
				slog.String("warning", warning))
		}
	}

	// Load CrawlTimeout (with 1m-4h range limit)
	result = config.LoadEnvDuration("CRAWL_TIMEOUT", cfg.CrawlTimeout, func(d time.Duration) error {
		return config.ValidateDuration(d, 1*time.Minute, 4*time.Hour)
	})
	cfg.CrawlTimeout = result.Value.(time.Duration)
	if result.FallbackApplied {
		fallbackApplied = true
		metrics.RecordValidationError("crawl_timeout")
		metrics.RecordFallback("crawl_timeout", "default")
		for _, warning := range result.Warnings {
			logger.Warn("Configuration fallback applied",
				slog.String("field", "CrawlTimeout"),
				slog.String("warning", warning))
		}
	}

	// Load HealthPort
	result = config.LoadEnvInt("WORKER_HEALTH_PORT", cfg.HealthPort, func(v int) error {
		return config.ValidateIntRange(v, 1024, 65535)
	})
	cfg.HealthPort = result.Value.(int)
	if result.FallbackApplied {
		fallbackApplied = true
		metrics.RecordValidationError("health_port")
		metrics.RecordFallback("health_port", "default")
		for _, warning := range result.Warnings {
			logger.Warn("Configuration fallback applied",
				slog.String("field", "HealthPort"),
				slog.String("warning", warning))
		}
	}

	// Update metrics
	metrics.SetFallbackActive("", fallbackApplied)
	metrics.RecordLoadTimestamp()

	// Always return valid config (fail-open strategy)
	return &cfg, nil
}
