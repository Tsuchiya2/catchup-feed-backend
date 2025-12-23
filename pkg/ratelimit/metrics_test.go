package ratelimit

import (
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

func TestNewPrometheusMetrics(t *testing.T) {
	metrics := NewPrometheusMetrics()

	if metrics == nil {
		t.Fatal("NewPrometheusMetrics() returned nil")
	}

	if metrics.registry == nil {
		t.Error("registry should not be nil")
	}

	if metrics.requestsTotal == nil {
		t.Error("requestsTotal should not be nil")
	}

	if metrics.checkDuration == nil {
		t.Error("checkDuration should not be nil")
	}

	if metrics.activeKeys == nil {
		t.Error("activeKeys should not be nil")
	}

	if metrics.circuitState == nil {
		t.Error("circuitState should not be nil")
	}

	if metrics.degradationLevel == nil {
		t.Error("degradationLevel should not be nil")
	}

	if metrics.evictionsTotal == nil {
		t.Error("evictionsTotal should not be nil")
	}
}

func TestPrometheusMetrics_Registry(t *testing.T) {
	metrics := NewPrometheusMetrics()

	registry := metrics.Registry()
	if registry == nil {
		t.Error("Registry() should not return nil")
	}

	// Record some metrics to ensure they show up in Gather()
	metrics.RecordRequest("test", "/test")
	metrics.RecordCheckDuration("test", 1*time.Millisecond)
	metrics.SetActiveKeys("test", 10)
	metrics.RecordCircuitState("test", "closed")
	metrics.RecordDegradationLevel("test", 0)
	metrics.RecordEviction("test", 1)

	// Verify metrics are registered
	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Should have all 6 metrics registered
	expectedMetrics := []string{
		"http_rate_limit_requests_total",
		"http_rate_limit_check_duration_seconds",
		"http_rate_limit_active_keys",
		"http_rate_limit_circuit_state",
		"http_rate_limit_degradation_level",
		"http_rate_limit_evictions_total",
	}

	metricNames := make(map[string]bool)
	for _, mf := range metricFamilies {
		metricNames[mf.GetName()] = true
	}

	for _, expected := range expectedMetrics {
		if !metricNames[expected] {
			t.Errorf("Expected metric %q not found in registry", expected)
		}
	}
}

func TestPrometheusMetrics_RecordRequest(t *testing.T) {
	metrics := NewPrometheusMetrics()

	// Record some requests
	metrics.RecordRequest("ip", "/api/articles")
	metrics.RecordRequest("ip", "/api/articles")
	metrics.RecordRequest("user", "/api/users")

	// Gather metrics
	metricFamilies, err := metrics.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Find the requests_total metric
	var found bool
	for _, mf := range metricFamilies {
		if mf.GetName() == "http_rate_limit_requests_total" {
			found = true

			// Check that we have the expected metrics
			for _, m := range mf.GetMetric() {
				labels := getLabels(m)

				if labels["limiter_type"] == "ip" && labels["status"] == "allowed" && labels["path"] == "/api/articles" {
					if m.GetCounter().GetValue() != 2 {
						t.Errorf("Expected 2 requests for ip/allowed/articles, got %v", m.GetCounter().GetValue())
					}
				}

				if labels["limiter_type"] == "user" && labels["status"] == "allowed" && labels["path"] == "/api/users" {
					if m.GetCounter().GetValue() != 1 {
						t.Errorf("Expected 1 request for user/allowed/users, got %v", m.GetCounter().GetValue())
					}
				}
			}
		}
	}

	if !found {
		t.Error("requests_total metric not found")
	}
}

func TestPrometheusMetrics_RecordDenied(t *testing.T) {
	metrics := NewPrometheusMetrics()

	// Record denied requests
	metrics.RecordDenied("ip", "/api/articles")
	metrics.RecordDenied("ip", "/api/articles")

	// Gather metrics
	metricFamilies, err := metrics.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Find the requests_total metric
	for _, mf := range metricFamilies {
		if mf.GetName() == "http_rate_limit_requests_total" {
			for _, m := range mf.GetMetric() {
				labels := getLabels(m)

				if labels["limiter_type"] == "ip" && labels["status"] == "denied" && labels["path"] == "/api/articles" {
					if m.GetCounter().GetValue() != 2 {
						t.Errorf("Expected 2 denied requests, got %v", m.GetCounter().GetValue())
					}
				}
			}
		}
	}
}

func TestPrometheusMetrics_RecordAllowed(t *testing.T) {
	metrics := NewPrometheusMetrics()

	// RecordAllowed should be an alias for RecordRequest
	metrics.RecordAllowed("user", "/api/test")

	// Gather metrics
	metricFamilies, err := metrics.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Should have the same effect as RecordRequest
	for _, mf := range metricFamilies {
		if mf.GetName() == "http_rate_limit_requests_total" {
			for _, m := range mf.GetMetric() {
				labels := getLabels(m)

				if labels["limiter_type"] == "user" && labels["status"] == "allowed" && labels["path"] == "/api/test" {
					if m.GetCounter().GetValue() != 1 {
						t.Errorf("Expected 1 allowed request, got %v", m.GetCounter().GetValue())
					}
				}
			}
		}
	}
}

func TestPrometheusMetrics_RecordCheckDuration(t *testing.T) {
	metrics := NewPrometheusMetrics()

	// Record some durations
	metrics.RecordCheckDuration("ip", 1*time.Millisecond)
	metrics.RecordCheckDuration("ip", 5*time.Millisecond)
	metrics.RecordCheckDuration("ip", 10*time.Millisecond)
	metrics.RecordCheckDuration("user", 2*time.Millisecond)

	// Gather metrics
	metricFamilies, err := metrics.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Find the check_duration metric
	var found bool
	for _, mf := range metricFamilies {
		if mf.GetName() == "http_rate_limit_check_duration_seconds" {
			found = true

			for _, m := range mf.GetMetric() {
				labels := getLabels(m)

				if labels["limiter_type"] == "ip" {
					histogram := m.GetHistogram()
					if histogram.GetSampleCount() != 3 {
						t.Errorf("Expected 3 samples for ip, got %v", histogram.GetSampleCount())
					}
				}

				if labels["limiter_type"] == "user" {
					histogram := m.GetHistogram()
					if histogram.GetSampleCount() != 1 {
						t.Errorf("Expected 1 sample for user, got %v", histogram.GetSampleCount())
					}
				}
			}
		}
	}

	if !found {
		t.Error("check_duration metric not found")
	}
}

func TestPrometheusMetrics_SetActiveKeys(t *testing.T) {
	metrics := NewPrometheusMetrics()

	// Set active keys
	metrics.SetActiveKeys("ip", 100)
	metrics.SetActiveKeys("user", 50)

	// Gather metrics
	metricFamilies, err := metrics.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Find the active_keys metric
	for _, mf := range metricFamilies {
		if mf.GetName() == "http_rate_limit_active_keys" {
			for _, m := range mf.GetMetric() {
				labels := getLabels(m)

				if labels["limiter_type"] == "ip" {
					if m.GetGauge().GetValue() != 100 {
						t.Errorf("Expected 100 active keys for ip, got %v", m.GetGauge().GetValue())
					}
				}

				if labels["limiter_type"] == "user" {
					if m.GetGauge().GetValue() != 50 {
						t.Errorf("Expected 50 active keys for user, got %v", m.GetGauge().GetValue())
					}
				}
			}
		}
	}
}

func TestPrometheusMetrics_RecordCircuitState(t *testing.T) {
	metrics := NewPrometheusMetrics()

	tests := []struct {
		name          string
		state         string
		expectedValue float64
	}{
		{"closed state", "closed", 0},
		{"open state", "open", 1},
		{"half-open state", "half-open", 2},
		{"unknown state defaults to closed", "unknown", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metrics.RecordCircuitState("test", tt.state)

			// Gather metrics
			metricFamilies, err := metrics.registry.Gather()
			if err != nil {
				t.Fatalf("Gather() error = %v", err)
			}

			// Find the circuit_state metric
			for _, mf := range metricFamilies {
				if mf.GetName() == "http_rate_limit_circuit_state" {
					for _, m := range mf.GetMetric() {
						labels := getLabels(m)

						if labels["limiter_type"] == "test" {
							if m.GetGauge().GetValue() != tt.expectedValue {
								t.Errorf("Expected circuit state %v, got %v", tt.expectedValue, m.GetGauge().GetValue())
							}
						}
					}
				}
			}
		})
	}
}

func TestPrometheusMetrics_RecordDegradationLevel(t *testing.T) {
	metrics := NewPrometheusMetrics()

	// Record different degradation levels
	metrics.RecordDegradationLevel("ip", 0)
	metrics.RecordDegradationLevel("user", 2)

	// Gather metrics
	metricFamilies, err := metrics.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Find the degradation_level metric
	for _, mf := range metricFamilies {
		if mf.GetName() == "http_rate_limit_degradation_level" {
			for _, m := range mf.GetMetric() {
				labels := getLabels(m)

				if labels["limiter_type"] == "ip" {
					if m.GetGauge().GetValue() != 0 {
						t.Errorf("Expected degradation level 0 for ip, got %v", m.GetGauge().GetValue())
					}
				}

				if labels["limiter_type"] == "user" {
					if m.GetGauge().GetValue() != 2 {
						t.Errorf("Expected degradation level 2 for user, got %v", m.GetGauge().GetValue())
					}
				}
			}
		}
	}
}

func TestPrometheusMetrics_RecordEviction(t *testing.T) {
	metrics := NewPrometheusMetrics()

	// Record evictions
	metrics.RecordEviction("ip", 10)
	metrics.RecordEviction("ip", 5)
	metrics.RecordEviction("user", 3)

	// Gather metrics
	metricFamilies, err := metrics.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Find the evictions_total metric
	for _, mf := range metricFamilies {
		if mf.GetName() == "http_rate_limit_evictions_total" {
			for _, m := range mf.GetMetric() {
				labels := getLabels(m)

				if labels["limiter_type"] == "ip" {
					// Should be 10 + 5 = 15
					if m.GetCounter().GetValue() != 15 {
						t.Errorf("Expected 15 evictions for ip, got %v", m.GetCounter().GetValue())
					}
				}

				if labels["limiter_type"] == "user" {
					if m.GetCounter().GetValue() != 3 {
						t.Errorf("Expected 3 evictions for user, got %v", m.GetCounter().GetValue())
					}
				}
			}
		}
	}
}

func TestNewNoOpMetrics(t *testing.T) {
	metrics := NewNoOpMetrics()

	if metrics == nil {
		t.Fatal("NewNoOpMetrics() returned nil")
	}
}

func TestNoOpMetrics_AllMethods(t *testing.T) {
	metrics := NewNoOpMetrics()

	// All methods should not panic and should be no-ops
	t.Run("RecordRequest", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("RecordRequest() panicked: %v", r)
			}
		}()
		metrics.RecordRequest("ip", "/api/test")
	})

	t.Run("RecordDenied", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("RecordDenied() panicked: %v", r)
			}
		}()
		metrics.RecordDenied("ip", "/api/test")
	})

	t.Run("RecordAllowed", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("RecordAllowed() panicked: %v", r)
			}
		}()
		metrics.RecordAllowed("ip", "/api/test")
	})

	t.Run("RecordCheckDuration", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("RecordCheckDuration() panicked: %v", r)
			}
		}()
		metrics.RecordCheckDuration("ip", 1*time.Millisecond)
	})

	t.Run("SetActiveKeys", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SetActiveKeys() panicked: %v", r)
			}
		}()
		metrics.SetActiveKeys("ip", 100)
	})

	t.Run("RecordCircuitState", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("RecordCircuitState() panicked: %v", r)
			}
		}()
		metrics.RecordCircuitState("ip", "closed")
	})

	t.Run("RecordDegradationLevel", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("RecordDegradationLevel() panicked: %v", r)
			}
		}()
		metrics.RecordDegradationLevel("ip", 0)
	})

	t.Run("RecordEviction", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("RecordEviction() panicked: %v", r)
			}
		}()
		metrics.RecordEviction("ip", 10)
	})
}

func TestPrometheusMetrics_MultipleInstances(t *testing.T) {
	// Creating multiple instances should work (each has its own registry)
	metrics1 := NewPrometheusMetrics()
	metrics2 := NewPrometheusMetrics()

	metrics1.RecordRequest("ip", "/api/test1")
	metrics2.RecordRequest("ip", "/api/test2")

	// Each should have only its own metrics
	mf1, err := metrics1.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	mf2, err := metrics2.registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Both should have metrics but they should be independent
	if len(mf1) == 0 {
		t.Error("metrics1 should have metrics")
	}
	if len(mf2) == 0 {
		t.Error("metrics2 should have metrics")
	}
}

// Helper function to extract labels from a metric
func getLabels(m *dto.Metric) map[string]string {
	labels := make(map[string]string)
	for _, label := range m.GetLabel() {
		labels[label.GetName()] = label.GetValue()
	}
	return labels
}

func TestSystemClock_Now(t *testing.T) {
	clock := &SystemClock{}

	before := time.Now()
	now := clock.Now()
	after := time.Now()

	// System clock should return current time
	if now.Before(before) || now.After(after) {
		t.Errorf("SystemClock.Now() = %v, should be between %v and %v", now, before, after)
	}
}

func TestMockClock_Now(t *testing.T) {
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewMockClock(fixedTime)

	now := clock.Now()
	if !now.Equal(fixedTime) {
		t.Errorf("MockClock.Now() = %v, want %v", now, fixedTime)
	}
}

func TestMockClock_Advance(t *testing.T) {
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	clock := NewMockClock(fixedTime)

	clock.Advance(1 * time.Hour)

	expected := fixedTime.Add(1 * time.Hour)
	now := clock.Now()

	if !now.Equal(expected) {
		t.Errorf("After Advance(1h), Now() = %v, want %v", now, expected)
	}
}

func TestMockClock_Set(t *testing.T) {
	clock := NewMockClock(time.Now())

	newTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	clock.Set(newTime)

	now := clock.Now()
	if !now.Equal(newTime) {
		t.Errorf("After Set(), Now() = %v, want %v", now, newTime)
	}
}

func TestMockClock_ConcurrentAccess(t *testing.T) {
	clock := NewMockClock(time.Now())

	var wg sync.WaitGroup
	numGoroutines := 10

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				clock.Now()
			}
		}()
	}

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				clock.Advance(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()

	// Should not panic or deadlock
	clock.Now()
}
