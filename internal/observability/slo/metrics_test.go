package slo

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
)

func TestSLOConstants(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		expected float64
	}{
		{"AvailabilitySLO", AvailabilitySLO, 99.9},
		{"LatencyP95SLO", LatencyP95SLO, 0.200},
		{"LatencyP99SLO", LatencyP99SLO, 0.500},
		{"ErrorRateSLO", ErrorRateSLO, 0.001},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.value != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.value, tt.expected)
			}
		})
	}
}

func TestUpdateAvailability(t *testing.T) {
	// Reset metric before test
	SLOAvailability.Set(0)

	testValue := 0.9995
	UpdateAvailability(testValue)

	metric := &io_prometheus_client.Metric{}
	if err := SLOAvailability.Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}

	got := metric.GetGauge().GetValue()
	if got != testValue {
		t.Errorf("SLOAvailability = %v, want %v", got, testValue)
	}
}

func TestUpdateLatencyP95(t *testing.T) {
	// Reset metric before test
	SLOLatencyP95.Set(0)

	testValue := 0.150
	UpdateLatencyP95(testValue)

	metric := &io_prometheus_client.Metric{}
	if err := SLOLatencyP95.Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}

	got := metric.GetGauge().GetValue()
	if got != testValue {
		t.Errorf("SLOLatencyP95 = %v, want %v", got, testValue)
	}
}

func TestUpdateLatencyP99(t *testing.T) {
	// Reset metric before test
	SLOLatencyP99.Set(0)

	testValue := 0.450
	UpdateLatencyP99(testValue)

	metric := &io_prometheus_client.Metric{}
	if err := SLOLatencyP99.Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}

	got := metric.GetGauge().GetValue()
	if got != testValue {
		t.Errorf("SLOLatencyP99 = %v, want %v", got, testValue)
	}
}

func TestUpdateErrorRate(t *testing.T) {
	// Reset metric before test
	SLOErrorRate.Set(0)

	testValue := 0.0005
	UpdateErrorRate(testValue)

	metric := &io_prometheus_client.Metric{}
	if err := SLOErrorRate.Write(metric); err != nil {
		t.Fatalf("failed to write metric: %v", err)
	}

	got := metric.GetGauge().GetValue()
	if got != testValue {
		t.Errorf("SLOErrorRate = %v, want %v", got, testValue)
	}
}

func TestMetricsAreRegistered(t *testing.T) {
	metrics := []prometheus.Collector{
		SLOAvailability,
		SLOLatencyP95,
		SLOLatencyP99,
		SLOErrorRate,
	}

	for _, metric := range metrics {
		desc := make(chan *prometheus.Desc, 1)
		metric.Describe(desc)
		select {
		case d := <-desc:
			if d == nil {
				t.Error("metric descriptor is nil")
			}
		default:
			t.Error("no descriptor received")
		}
	}
}

func TestSLOMetricsCanBeObserved(t *testing.T) {
	// Set test values
	UpdateAvailability(0.999)
	UpdateLatencyP95(0.180)
	UpdateLatencyP99(0.420)
	UpdateErrorRate(0.0008)

	// Verify all metrics can be collected
	metrics := []prometheus.Collector{
		SLOAvailability,
		SLOLatencyP95,
		SLOLatencyP99,
		SLOErrorRate,
	}

	for _, metric := range metrics {
		ch := make(chan prometheus.Metric, 1)
		metric.Collect(ch)
		select {
		case m := <-ch:
			if m == nil {
				t.Error("collected metric is nil")
			}
		default:
			t.Error("no metric collected")
		}
	}
}

func TestSLOTargetsAreReasonable(t *testing.T) {
	// Availability should be between 90% and 100%
	if AvailabilitySLO < 90.0 || AvailabilitySLO > 100.0 {
		t.Errorf("AvailabilitySLO = %v, should be between 90 and 100", AvailabilitySLO)
	}

	// Latency P95 should be positive and less than 1 second for API
	if LatencyP95SLO <= 0 || LatencyP95SLO > 1.0 {
		t.Errorf("LatencyP95SLO = %v, should be between 0 and 1 second", LatencyP95SLO)
	}

	// Latency P99 should be greater than P95 and less than 2 seconds
	if LatencyP99SLO <= LatencyP95SLO || LatencyP99SLO > 2.0 {
		t.Errorf("LatencyP99SLO = %v, should be greater than P95 (%v) and less than 2 seconds",
			LatencyP99SLO, LatencyP95SLO)
	}

	// Error rate should be less than 1%
	if ErrorRateSLO < 0 || ErrorRateSLO > 0.01 {
		t.Errorf("ErrorRateSLO = %v, should be between 0 and 0.01 (1%%)", ErrorRateSLO)
	}
}
