package slo

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// SLO targets define the service level objectives for the application.
// These targets are used to measure and monitor service reliability.
const (
	// AvailabilitySLO defines the target uptime percentage (99.9% = 43 minutes downtime per month)
	AvailabilitySLO = 99.9

	// LatencyP95SLO defines the target for 95th percentile latency in seconds (200ms)
	LatencyP95SLO = 0.200

	// LatencyP99SLO defines the target for 99th percentile latency in seconds (500ms)
	LatencyP99SLO = 0.500

	// ErrorRateSLO defines the maximum acceptable error rate as a ratio (0.1% = 0.001)
	ErrorRateSLO = 0.001
)

// SLO tracking metrics
// These gauges are updated periodically (e.g., every minute) based on recent measurements
// to track whether the service is meeting its SLO targets.
var (
	// SLOAvailability tracks the current availability ratio (0-1)
	// calculated as: (total_requests - 5xx_errors) / total_requests
	SLOAvailability = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "slo_availability_ratio",
			Help: "Current availability ratio (0-1), target: 0.999",
		},
	)

	// SLOLatencyP95 tracks the current p95 latency in seconds
	// calculated from http_request_duration_seconds histogram
	SLOLatencyP95 = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "slo_latency_p95_seconds",
			Help: "Current p95 latency in seconds, target: 0.200",
		},
	)

	// SLOLatencyP99 tracks the current p99 latency in seconds
	// calculated from http_request_duration_seconds histogram
	SLOLatencyP99 = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "slo_latency_p99_seconds",
			Help: "Current p99 latency in seconds, target: 0.500",
		},
	)

	// SLOErrorRate tracks the current error rate ratio (0-1)
	// calculated as: 5xx_errors / total_requests
	SLOErrorRate = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "slo_error_rate_ratio",
			Help: "Current error rate ratio (0-1), target: 0.001",
		},
	)
)

// UpdateAvailability updates the availability SLO metric.
// Call this periodically (e.g., every minute) with the calculated availability ratio.
//
// Example calculation:
//
//	totalRequests := getTotalRequestCount()
//	errorRequests := get5xxErrorCount()
//	availability := float64(totalRequests - errorRequests) / float64(totalRequests)
//	slo.UpdateAvailability(availability)
func UpdateAvailability(ratio float64) {
	SLOAvailability.Set(ratio)
}

// UpdateLatencyP95 updates the p95 latency SLO metric.
// Call this periodically with the calculated p95 latency in seconds.
//
// Example using Prometheus query:
//
//	histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))
func UpdateLatencyP95(seconds float64) {
	SLOLatencyP95.Set(seconds)
}

// UpdateLatencyP99 updates the p99 latency SLO metric.
// Call this periodically with the calculated p99 latency in seconds.
//
// Example using Prometheus query:
//
//	histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))
func UpdateLatencyP99(seconds float64) {
	SLOLatencyP99.Set(seconds)
}

// UpdateErrorRate updates the error rate SLO metric.
// Call this periodically with the calculated error rate ratio.
//
// Example calculation:
//
//	totalRequests := getTotalRequestCount()
//	errorRequests := get5xxErrorCount()
//	errorRate := float64(errorRequests) / float64(totalRequests)
//	slo.UpdateErrorRate(errorRate)
func UpdateErrorRate(ratio float64) {
	SLOErrorRate.Set(ratio)
}
