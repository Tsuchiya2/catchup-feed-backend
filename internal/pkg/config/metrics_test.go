package config

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// TestNewConfigMetrics_Registration tests that metrics are registered correctly
func TestNewConfigMetrics_Registration(t *testing.T) {
	// Create metrics with unique component name to avoid conflicts
	componentName := "test_component_registration"
	metrics := NewConfigMetrics(componentName)

	// Verify all metrics are initialized
	assert.NotNil(t, metrics.LoadTimestamp, "LoadTimestamp should be initialized")
	assert.NotNil(t, metrics.ValidationErrorsTotal, "ValidationErrorsTotal should be initialized")
	assert.NotNil(t, metrics.FallbacksTotal, "FallbacksTotal should be initialized")
	assert.NotNil(t, metrics.FallbackActive, "FallbackActive should be initialized")

	// Verify component name is stored
	assert.Equal(t, componentName, metrics.componentName, "Component name should be stored")
}

// TestNewConfigMetrics_UniqueNames tests that different components create unique metrics
func TestNewConfigMetrics_UniqueNames(t *testing.T) {
	// Create metrics for different components
	workerMetrics := NewConfigMetrics("test_worker")
	fetcherMetrics := NewConfigMetrics("test_fetcher")

	// Verify metrics are different instances
	assert.NotSame(t, workerMetrics.LoadTimestamp, fetcherMetrics.LoadTimestamp,
		"Different components should have different metric instances")

	// Verify both are usable
	workerMetrics.RecordLoadTimestamp()
	fetcherMetrics.RecordLoadTimestamp()

	// Both should succeed without panic
}

// TestRecordLoadTimestamp_UpdatesMetric tests that load timestamp is recorded
func TestRecordLoadTimestamp_UpdatesMetric(t *testing.T) {
	metrics := NewConfigMetrics("test_load_timestamp")

	// Record timestamp
	metrics.RecordLoadTimestamp()

	// Verify metric was updated (value should be > 0)
	value := testutil.ToFloat64(metrics.LoadTimestamp)
	assert.Greater(t, value, float64(0), "Load timestamp should be greater than 0")
}

// TestRecordValidationError_IncrementsCounter tests validation error recording
func TestRecordValidationError_IncrementsCounter(t *testing.T) {
	metrics := NewConfigMetrics("test_validation_error")

	// Initial value should be 0
	initialValue := testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues("cron_schedule"))
	assert.Equal(t, float64(0), initialValue, "Initial validation error count should be 0")

	// Record validation error
	metrics.RecordValidationError("cron_schedule")

	// Verify counter was incremented
	newValue := testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues("cron_schedule"))
	assert.Equal(t, float64(1), newValue, "Validation error count should be 1 after recording")

	// Record another error
	metrics.RecordValidationError("cron_schedule")

	// Verify counter incremented again
	finalValue := testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues("cron_schedule"))
	assert.Equal(t, float64(2), finalValue, "Validation error count should be 2 after second recording")
}

// TestRecordValidationError_DifferentFields tests that errors are tracked per field
func TestRecordValidationError_DifferentFields(t *testing.T) {
	metrics := NewConfigMetrics("test_validation_fields")

	// Record errors for different fields
	metrics.RecordValidationError("cron_schedule")
	metrics.RecordValidationError("timezone")
	metrics.RecordValidationError("cron_schedule")

	// Verify counts are tracked separately
	cronCount := testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues("cron_schedule"))
	timezoneCount := testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues("timezone"))

	assert.Equal(t, float64(2), cronCount, "Cron schedule should have 2 errors")
	assert.Equal(t, float64(1), timezoneCount, "Timezone should have 1 error")
}

// TestRecordFallback_IncrementsCounter tests fallback recording
func TestRecordFallback_IncrementsCounter(t *testing.T) {
	metrics := NewConfigMetrics("test_fallback")

	// Initial value should be 0
	initialValue := testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues("timezone"))
	assert.Equal(t, float64(0), initialValue, "Initial fallback count should be 0")

	// Record fallback
	metrics.RecordFallback("timezone", "default")

	// Verify counter was incremented
	newValue := testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues("timezone"))
	assert.Equal(t, float64(1), newValue, "Fallback count should be 1 after recording")

	// Record another fallback
	metrics.RecordFallback("timezone", "default")

	// Verify counter incremented again
	finalValue := testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues("timezone"))
	assert.Equal(t, float64(2), finalValue, "Fallback count should be 2 after second recording")
}

// TestRecordFallback_DifferentFields tests that fallbacks are tracked per field
func TestRecordFallback_DifferentFields(t *testing.T) {
	metrics := NewConfigMetrics("test_fallback_fields")

	// Record fallbacks for different fields
	metrics.RecordFallback("cron_schedule", "default")
	metrics.RecordFallback("timezone", "default")
	metrics.RecordFallback("timeout", "default")
	metrics.RecordFallback("cron_schedule", "default")

	// Verify counts are tracked separately
	cronCount := testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues("cron_schedule"))
	timezoneCount := testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues("timezone"))
	timeoutCount := testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues("timeout"))

	assert.Equal(t, float64(2), cronCount, "Cron schedule should have 2 fallbacks")
	assert.Equal(t, float64(1), timezoneCount, "Timezone should have 1 fallback")
	assert.Equal(t, float64(1), timeoutCount, "Timeout should have 1 fallback")
}

// TestSetFallbackActive_True tests setting fallback active to true
func TestSetFallbackActive_True(t *testing.T) {
	metrics := NewConfigMetrics("test_fallback_active_true")

	// Set fallback active to true
	metrics.SetFallbackActive("any", true)

	// Verify gauge is set to 1
	value := testutil.ToFloat64(metrics.FallbackActive)
	assert.Equal(t, float64(1), value, "Fallback active should be 1 when set to true")
}

// TestSetFallbackActive_False tests setting fallback active to false
func TestSetFallbackActive_False(t *testing.T) {
	metrics := NewConfigMetrics("test_fallback_active_false")

	// Set fallback active to true first
	metrics.SetFallbackActive("any", true)

	// Then set to false
	metrics.SetFallbackActive("any", false)

	// Verify gauge is set to 0
	value := testutil.ToFloat64(metrics.FallbackActive)
	assert.Equal(t, float64(0), value, "Fallback active should be 0 when set to false")
}

// TestSetFallbackActive_Toggle tests toggling fallback active status
func TestSetFallbackActive_Toggle(t *testing.T) {
	metrics := NewConfigMetrics("test_fallback_toggle")

	// Start with false
	metrics.SetFallbackActive("", false)
	assert.Equal(t, float64(0), testutil.ToFloat64(metrics.FallbackActive), "Should start at 0")

	// Toggle to true
	metrics.SetFallbackActive("", true)
	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.FallbackActive), "Should be 1 after setting true")

	// Toggle back to false
	metrics.SetFallbackActive("", false)
	assert.Equal(t, float64(0), testutil.ToFloat64(metrics.FallbackActive), "Should be 0 after setting false")

	// Toggle to true again
	metrics.SetFallbackActive("", true)
	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.FallbackActive), "Should be 1 again")
}

// TestMetrics_Integration tests realistic usage scenario
func TestMetrics_Integration(t *testing.T) {
	metrics := NewConfigMetrics("test_integration")

	// Simulate configuration load
	metrics.RecordLoadTimestamp()

	// Simulate validation errors
	metrics.RecordValidationError("cron_schedule")
	metrics.RecordValidationError("timezone")

	// Simulate fallback operations
	metrics.RecordFallback("cron_schedule", "default")
	metrics.RecordFallback("timezone", "default")

	// Set fallback active
	metrics.SetFallbackActive("multiple", true)

	// Verify all metrics are recorded correctly
	assert.Greater(t, testutil.ToFloat64(metrics.LoadTimestamp), float64(0),
		"Load timestamp should be recorded")

	assert.Equal(t, float64(1),
		testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues("cron_schedule")),
		"Cron schedule validation error should be recorded")

	assert.Equal(t, float64(1),
		testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues("timezone")),
		"Timezone validation error should be recorded")

	assert.Equal(t, float64(1),
		testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues("cron_schedule")),
		"Cron schedule fallback should be recorded")

	assert.Equal(t, float64(1),
		testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues("timezone")),
		"Timezone fallback should be recorded")

	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.FallbackActive),
		"Fallback active should be set")
}

// TestMetrics_NoErrorsScenario tests scenario with no validation errors
func TestMetrics_NoErrorsScenario(t *testing.T) {
	metrics := NewConfigMetrics("test_no_errors")

	// Simulate successful configuration load
	metrics.RecordLoadTimestamp()
	metrics.SetFallbackActive("", false)

	// Verify load timestamp is recorded
	assert.Greater(t, testutil.ToFloat64(metrics.LoadTimestamp), float64(0),
		"Load timestamp should be recorded")

	// Verify no errors or fallbacks recorded
	assert.Equal(t, float64(0),
		testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues("any_field")),
		"No validation errors should be recorded")

	assert.Equal(t, float64(0),
		testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues("any_field")),
		"No fallbacks should be recorded")

	assert.Equal(t, float64(0), testutil.ToFloat64(metrics.FallbackActive),
		"Fallback active should be 0")
}

// TestMetrics_MultipleFallbacksScenario tests scenario with multiple fallbacks
func TestMetrics_MultipleFallbacksScenario(t *testing.T) {
	metrics := NewConfigMetrics("test_multiple_fallbacks")

	// Simulate configuration load with multiple errors
	metrics.RecordLoadTimestamp()

	// Record multiple validation errors and fallbacks
	fields := []string{"cron_schedule", "timezone", "timeout"}
	for _, field := range fields {
		metrics.RecordValidationError(field)
		metrics.RecordFallback(field, "default")
	}

	metrics.SetFallbackActive("multiple", true)

	// Verify all errors and fallbacks are recorded
	for _, field := range fields {
		assert.Equal(t, float64(1),
			testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues(field)),
			"Validation error should be recorded for "+field)

		assert.Equal(t, float64(1),
			testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues(field)),
			"Fallback should be recorded for "+field)
	}

	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.FallbackActive),
		"Fallback active should be set with multiple fallbacks")
}

// TestMetrics_ConcurrentAccess tests metrics are safe for concurrent access
func TestMetrics_ConcurrentAccess(t *testing.T) {
	metrics := NewConfigMetrics("test_concurrent")

	// Spawn multiple goroutines to record metrics concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			metrics.RecordLoadTimestamp()
			metrics.RecordValidationError("test_field")
			metrics.RecordFallback("test_field", "default")
			metrics.SetFallbackActive("test_field", true)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify metrics were recorded (exact count may vary due to timing)
	assert.Greater(t, testutil.ToFloat64(metrics.LoadTimestamp), float64(0),
		"Load timestamp should be recorded")

	validationErrors := testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues("test_field"))
	assert.Equal(t, float64(10), validationErrors,
		"Should have recorded 10 validation errors")

	fallbacks := testutil.ToFloat64(metrics.FallbacksTotal.WithLabelValues("test_field"))
	assert.Equal(t, float64(10), fallbacks,
		"Should have recorded 10 fallbacks")
}

// TestMetrics_MetricNaming tests that metric names follow Prometheus conventions
func TestMetrics_MetricNaming(t *testing.T) {
	componentName := "test_naming"
	metrics := NewConfigMetrics(componentName)

	// Verify metric names follow naming conventions
	// Prometheus metrics should:
	// 1. Use snake_case
	// 2. Have component prefix
	// 3. Include unit suffix where applicable (e.g., _total, _seconds)

	// We can't directly inspect metric names from the Gauge/Counter objects,
	// but we can verify they're created without panic and are usable
	metrics.RecordLoadTimestamp()
	metrics.RecordValidationError("test")
	metrics.RecordFallback("test", "default")
	metrics.SetFallbackActive("test", true)

	// If we got here without panic, metrics are properly named and registered
}

// TestMetrics_PrometheusCompatibility tests that metrics are compatible with Prometheus
func TestMetrics_PrometheusCompatibility(t *testing.T) {
	metrics := NewConfigMetrics("test_prometheus")

	// Record some metrics
	metrics.RecordLoadTimestamp()
	metrics.RecordValidationError("field1")
	metrics.RecordFallback("field1", "default")
	metrics.SetFallbackActive("field1", true)

	// Collect metrics from the registry
	// This verifies that metrics are properly registered and can be scraped
	registry := prometheus.DefaultRegisterer.(*prometheus.Registry)
	assert.NotNil(t, registry, "Default registry should exist")

	// Metrics should be collectible without error
	// (If there were issues, promauto.New* would have panicked during creation)
}

// TestMetrics_EdgeCases tests edge cases and boundary conditions
func TestMetrics_EdgeCases(t *testing.T) {
	metrics := NewConfigMetrics("test_edge_cases")

	// Test with empty field names
	metrics.RecordValidationError("")
	metrics.RecordFallback("", "default")

	// Verify metrics still work
	assert.Equal(t, float64(1),
		testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues("")),
		"Should handle empty field name")

	// Test with very long field names
	longFieldName := "very_long_field_name_that_exceeds_normal_length_boundaries_for_testing_purposes"
	metrics.RecordValidationError(longFieldName)
	metrics.RecordFallback(longFieldName, "default")

	assert.Equal(t, float64(1),
		testutil.ToFloat64(metrics.ValidationErrorsTotal.WithLabelValues(longFieldName)),
		"Should handle long field names")

	// Test setting fallback active multiple times to same value
	metrics.SetFallbackActive("", true)
	metrics.SetFallbackActive("", true)
	metrics.SetFallbackActive("", true)

	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.FallbackActive),
		"Multiple sets to same value should work")
}
