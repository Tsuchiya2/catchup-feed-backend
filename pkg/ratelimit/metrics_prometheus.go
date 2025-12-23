package ratelimit

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusMetrics implements the RateLimitMetrics interface using Prometheus.
//
// This implementation provides observability for rate limiting operations with
// detailed metrics including:
// - Request counters (allowed/denied) by limiter type and status
// - Rate limit check duration histograms
// - Active key gauges for memory monitoring
// - Circuit breaker state tracking
// - Degradation level monitoring
// - LRU eviction counters
//
// All metrics use a custom registry for better testability and isolation.
type PrometheusMetrics struct {
	registry *prometheus.Registry

	// requestsTotal tracks total rate limit requests by limiter type, status, and path.
	// Labels:
	//   - limiter_type: "ip" or "user"
	//   - status: "allowed" or "denied"
	//   - path: Request path (for per-endpoint metrics)
	requestsTotal *prometheus.CounterVec

	// checkDuration tracks the duration of rate limit check operations.
	// Labels:
	//   - limiter_type: "ip" or "user"
	//
	// Buckets are optimized for fast rate limit checks (<5ms target):
	// - 0.5ms, 1ms, 2ms, 5ms (fast checks)
	// - 10ms, 25ms, 50ms (slower checks, potential issues)
	// - 100ms, 250ms, 500ms, 1s (circuit breaker should trigger)
	checkDuration *prometheus.HistogramVec

	// activeKeys tracks the current number of active keys in the rate limiter.
	// Labels:
	//   - limiter_type: "ip" or "user"
	activeKeys *prometheus.GaugeVec

	// circuitState tracks the circuit breaker state.
	// Labels:
	//   - limiter_type: "ip" or "user"
	//
	// Values:
	//   - 0: Closed (normal operation)
	//   - 1: Open (failing, allowing all requests)
	//   - 2: Half-Open (testing recovery)
	circuitState *prometheus.GaugeVec

	// degradationLevel tracks the current degradation level.
	// Labels:
	//   - limiter_type: "ip" or "user"
	//
	// Values:
	//   - 0: Normal (1x limits)
	//   - 1: Relaxed (2x limits)
	//   - 2: Minimal (10x limits)
	//   - 3: Disabled (no limits)
	degradationLevel *prometheus.GaugeVec

	// evictionsTotal tracks the total number of LRU evictions.
	// Labels:
	//   - limiter_type: "ip" or "user"
	evictionsTotal *prometheus.CounterVec
}

// NewPrometheusMetrics creates a new PrometheusMetrics instance with a custom registry.
//
// Using a custom registry (instead of the global prometheus.DefaultRegisterer) provides:
// - Better testability (isolated metrics per test)
// - No metric conflicts when running multiple instances
// - Explicit metric lifecycle management
//
// The registry can be passed to promhttp.HandlerFor() to expose metrics.
func NewPrometheusMetrics() *PrometheusMetrics {
	registry := prometheus.NewRegistry()

	requestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_rate_limit_requests_total",
			Help: "Total rate limit requests by limiter type, status, and path",
		},
		[]string{"limiter_type", "status", "path"},
	)

	checkDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_rate_limit_check_duration_seconds",
			Help:    "Duration of rate limit check operations",
			Buckets: []float64{0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"limiter_type"},
	)

	activeKeys := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_rate_limit_active_keys",
			Help: "Current number of active keys by limiter type",
		},
		[]string{"limiter_type"},
	)

	circuitState := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_rate_limit_circuit_state",
			Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
		},
		[]string{"limiter_type"},
	)

	degradationLevel := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "http_rate_limit_degradation_level",
			Help: "Current degradation level (0=normal, 1=relaxed, 2=minimal, 3=disabled)",
		},
		[]string{"limiter_type"},
	)

	evictionsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_rate_limit_evictions_total",
			Help: "Total LRU evictions by limiter type",
		},
		[]string{"limiter_type"},
	)

	// Register all metrics with the custom registry
	registry.MustRegister(
		requestsTotal,
		checkDuration,
		activeKeys,
		circuitState,
		degradationLevel,
		evictionsTotal,
	)

	return &PrometheusMetrics{
		registry:         registry,
		requestsTotal:    requestsTotal,
		checkDuration:    checkDuration,
		activeKeys:       activeKeys,
		circuitState:     circuitState,
		degradationLevel: degradationLevel,
		evictionsTotal:   evictionsTotal,
	}
}

// Registry returns the Prometheus registry containing all rate limit metrics.
//
// This can be used with promhttp.HandlerFor() to expose metrics:
//
//	metrics := NewPrometheusMetrics()
//	http.Handle("/metrics", promhttp.HandlerFor(metrics.Registry(), promhttp.HandlerOpts{}))
func (m *PrometheusMetrics) Registry() *prometheus.Registry {
	return m.registry
}

// RecordRequest records a rate limit check that resulted in an allowed request.
func (m *PrometheusMetrics) RecordRequest(limiterType, endpoint string) {
	m.requestsTotal.WithLabelValues(limiterType, "allowed", endpoint).Inc()
}

// RecordDenied records a rate limit violation (request denied).
func (m *PrometheusMetrics) RecordDenied(limiterType, endpoint string) {
	m.requestsTotal.WithLabelValues(limiterType, "denied", endpoint).Inc()
}

// RecordAllowed records a rate limit check that resulted in an allowed request.
//
// This is an alias for RecordRequest for better API clarity.
func (m *PrometheusMetrics) RecordAllowed(limiterType, endpoint string) {
	m.RecordRequest(limiterType, endpoint)
}

// RecordCheckDuration records the duration of a rate limit check operation.
//
// If duration exceeds 10ms, this may indicate performance issues that could
// trigger the circuit breaker.
func (m *PrometheusMetrics) RecordCheckDuration(limiterType string, duration time.Duration) {
	m.checkDuration.WithLabelValues(limiterType).Observe(duration.Seconds())
}

// SetActiveKeys records the current number of active keys in the rate limiter.
//
// This metric is useful for monitoring memory usage and triggering alerts when
// approaching capacity limits (e.g., >80% of max keys).
func (m *PrometheusMetrics) SetActiveKeys(limiterType string, count int) {
	m.activeKeys.WithLabelValues(limiterType).Set(float64(count))
}

// RecordCircuitState records the current state of the circuit breaker.
//
// States:
//   - "closed": Normal operation, rate limiting active
//   - "open": Circuit breaker open, all requests allowed (fail-open)
//   - "half-open": Testing recovery, limited requests allowed
//
// The state is mapped to a numeric gauge for Prometheus alerting:
//   - 0 = closed
//   - 1 = open
//   - 2 = half-open
func (m *PrometheusMetrics) RecordCircuitState(limiterType, state string) {
	var stateValue float64
	switch state {
	case "closed":
		stateValue = 0
	case "open":
		stateValue = 1
	case "half-open":
		stateValue = 2
	default:
		// Unknown state, default to closed
		stateValue = 0
	}
	m.circuitState.WithLabelValues(limiterType).Set(stateValue)
}

// RecordDegradationLevel records the current degradation level.
//
// Degradation levels:
//   - 0: Normal (1x limits)
//   - 1: Relaxed (2x limits, triggered by >5% error rate or >10ms p99 latency)
//   - 2: Minimal (10x limits, triggered by >20% error rate or >50ms p99 latency)
//   - 3: Disabled (no limits, triggered by >50% error rate or circuit open)
func (m *PrometheusMetrics) RecordDegradationLevel(limiterType string, level int) {
	m.degradationLevel.WithLabelValues(limiterType).Set(float64(level))
}

// RecordEviction records that keys were evicted from the store.
//
// Evictions occur when the in-memory store reaches capacity (max keys).
// High eviction rates may indicate:
// - DoS attack with many unique IPs
// - Need to increase max keys configuration
// - Need to migrate to Redis for distributed storage
func (m *PrometheusMetrics) RecordEviction(limiterType string, count int) {
	m.evictionsTotal.WithLabelValues(limiterType).Add(float64(count))
}
