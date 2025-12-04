package auth

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// authRequestsTotal counts authentication requests by role and result.
	authRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "auth_requests_total",
			Help: "Total authentication requests by role and result",
		},
		[]string{"role", "result"}, // result: success | failure
	)

	// authDuration tracks authentication duration by role.
	authDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "auth_duration_seconds",
			Help:    "Authentication duration by role",
			Buckets: []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1.0},
		},
		[]string{"role"},
	)

	// authzCheckDuration tracks authorization check duration.
	authzCheckDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "authz_check_duration_seconds",
			Help:    "Authorization check duration",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01},
		},
	)

	// forbiddenAttempts counts forbidden access attempts by role and method.
	forbiddenAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "forbidden_attempts_total",
			Help: "Forbidden access attempts by role and method",
		},
		[]string{"role", "method"},
	)
)

// RecordAuthRequest records an authentication request.
func RecordAuthRequest(role, result string) {
	authRequestsTotal.WithLabelValues(role, result).Inc()
}

// RecordAuthDuration records authentication duration.
func RecordAuthDuration(role string, durationSeconds float64) {
	authDuration.WithLabelValues(role).Observe(durationSeconds)
}

// RecordAuthzCheckDuration records authorization check duration.
func RecordAuthzCheckDuration(durationSeconds float64) {
	authzCheckDuration.Observe(durationSeconds)
}

// RecordForbiddenAttempt records a forbidden access attempt.
func RecordForbiddenAttempt(role, method string) {
	forbiddenAttempts.WithLabelValues(role, method).Inc()
}
