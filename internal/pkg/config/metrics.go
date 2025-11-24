package config

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ConfigMetrics provides parameterized Prometheus metrics for configuration management.
// This factory creates a standard set of metrics for tracking configuration state,
// validation errors, and fallback behavior across all components (worker, fetcher, summarizer).
//
// Metrics generated (parameterized by component name):
//   - {component}_config_load_timestamp: Unix timestamp of last configuration load
//   - {component}_config_validation_errors_total: Total validation errors by field
//   - {component}_config_fallbacks_total: Total fallback operations by field
//   - {component}_config_fallback_active: 1 if any fallback active, 0 otherwise
//
// Example usage:
//
//	// In worker package
//	var ConfigMetrics = config.NewConfigMetrics("worker")
//
//	// Record configuration load
//	ConfigMetrics.RecordLoadTimestamp()
//
//	// Record validation error
//	ConfigMetrics.RecordValidationError("cron_schedule")
//
//	// Record fallback operation
//	ConfigMetrics.RecordFallback("timezone", "invalid_value")
//
//	// Set fallback status
//	ConfigMetrics.SetFallbackActive("timezone", true)
//
// For testing:
//
//	metrics := config.NewConfigMetrics("test_component")
//	metrics.RecordLoadTimestamp()
//	// Verify metrics via Prometheus registry or test metrics endpoint
type ConfigMetrics struct {
	// LoadTimestamp records the Unix timestamp of the last configuration load.
	// Type: Gauge
	// Labels: none
	// Usage: Set to time.Now().Unix() when configuration is loaded
	LoadTimestamp prometheus.Gauge

	// ValidationErrorsTotal counts configuration validation errors by field.
	// Type: Counter
	// Labels: field (e.g., "cron_schedule", "timezone", "timeout")
	// Usage: Increment when validation fails for a specific field
	ValidationErrorsTotal *prometheus.CounterVec

	// FallbacksTotal counts fallback operations by field.
	// Type: Counter
	// Labels: field (e.g., "cron_schedule", "timezone", "timeout")
	// Usage: Increment when a fallback value is applied
	FallbacksTotal *prometheus.CounterVec

	// FallbackActive indicates whether any fallback is currently active.
	// Type: Gauge
	// Labels: none
	// Values: 1 (fallback active), 0 (no fallback)
	// Usage: Set to 1 if any field uses fallback, 0 if all fields valid
	FallbackActive prometheus.Gauge

	componentName string
}

// NewConfigMetrics creates a new ConfigMetrics instance with component-specific metric names.
// The component name is used as a prefix for all metrics to avoid naming conflicts
// between different components (e.g., "worker", "fetcher", "summarizer").
//
// Parameters:
//   - componentName: The name of the component (e.g., "worker", "fetcher", "summarizer")
//
// Returns:
//   - *ConfigMetrics: Initialized metrics factory with all metrics registered
//
// Example:
//
//	workerMetrics := config.NewConfigMetrics("worker")
//	// Creates: worker_config_load_timestamp, worker_config_validation_errors_total, etc.
//
//	fetcherMetrics := config.NewConfigMetrics("fetcher")
//	// Creates: fetcher_config_load_timestamp, fetcher_config_validation_errors_total, etc.
//
// Note: Metrics are automatically registered with Prometheus default registry.
// If metrics with the same name already exist, this function will panic.
// Use unique component names to avoid conflicts.
func NewConfigMetrics(componentName string) *ConfigMetrics {
	return &ConfigMetrics{
		LoadTimestamp: promauto.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_config_load_timestamp", componentName),
			Help: fmt.Sprintf("Unix timestamp of last %s configuration load", componentName),
		}),

		ValidationErrorsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_config_validation_errors_total", componentName),
			Help: fmt.Sprintf("Total number of %s configuration validation errors", componentName),
		}, []string{"field"}),

		FallbacksTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: fmt.Sprintf("%s_config_fallbacks_total", componentName),
			Help: fmt.Sprintf("Total number of %s configuration fallback operations", componentName),
		}, []string{"field"}),

		FallbackActive: promauto.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s_config_fallback_active", componentName),
			Help: fmt.Sprintf("1 if any %s configuration fallback is active, 0 otherwise", componentName),
		}),

		componentName: componentName,
	}
}

// RecordLoadTimestamp records the current time as the configuration load timestamp.
// This should be called whenever configuration is loaded or reloaded.
//
// Example:
//
//	config, warnings, err := LoadConfigFromEnv()
//	if err == nil {
//	    metrics.RecordLoadTimestamp()
//	}
func (m *ConfigMetrics) RecordLoadTimestamp() {
	m.LoadTimestamp.SetToCurrentTime()
}

// RecordValidationError increments the validation error counter for a specific field.
// This should be called whenever a configuration value fails validation.
//
// Parameters:
//   - field: The name of the configuration field that failed validation
//
// Example:
//
//	if err := validateCronSchedule(schedule); err != nil {
//	    metrics.RecordValidationError("cron_schedule")
//	}
func (m *ConfigMetrics) RecordValidationError(field string) {
	m.ValidationErrorsTotal.WithLabelValues(field).Inc()
}

// RecordFallback increments the fallback counter for a specific field.
// This should be called whenever a fallback value is applied due to validation failure.
//
// Parameters:
//   - field: The name of the configuration field that triggered fallback
//   - fallbackType: The type of fallback applied (e.g., "default", "safe_value", "runtime")
//
// Example:
//
//	if invalidSchedule {
//	    metrics.RecordFallback("cron_schedule", "default")
//	    schedule = defaultSchedule
//	}
func (m *ConfigMetrics) RecordFallback(field, fallbackType string) {
	m.FallbacksTotal.WithLabelValues(field).Inc()
}

// SetFallbackActive sets the fallback active status.
// Set to true (1) if any configuration field is using a fallback value.
// Set to false (0) if all fields are using their configured values.
//
// Parameters:
//   - field: The name of the configuration field (for logging context)
//   - active: true if fallback is active, false otherwise
//
// Example:
//
//	config, warnings, err := LoadConfigFromEnv()
//	if len(warnings) > 0 {
//	    metrics.SetFallbackActive("any", true)
//	} else {
//	    metrics.SetFallbackActive("any", false)
//	}
//
// Alternatively, use the FallbackApplied field from config:
//
//	if config.FallbackApplied {
//	    metrics.SetFallbackActive("", true)
//	} else {
//	    metrics.SetFallbackActive("", false)
//	}
func (m *ConfigMetrics) SetFallbackActive(field string, active bool) {
	if active {
		m.FallbackActive.Set(1)
	} else {
		m.FallbackActive.Set(0)
	}
}
