package ratelimit

import "time"

// NoOpMetrics implements the RateLimitMetrics interface with no-op implementations.
//
// This implementation is useful for:
// - Testing environments where metrics are not needed
// - Disabling metrics collection (e.g., development mode)
// - Benchmarking rate limiter performance without metrics overhead
//
// All methods are no-ops and have minimal performance impact.
type NoOpMetrics struct{}

// NewNoOpMetrics creates a new NoOpMetrics instance.
func NewNoOpMetrics() *NoOpMetrics {
	return &NoOpMetrics{}
}

// RecordRequest is a no-op implementation.
func (m *NoOpMetrics) RecordRequest(limiterType, endpoint string) {
	// No-op
}

// RecordDenied is a no-op implementation.
func (m *NoOpMetrics) RecordDenied(limiterType, endpoint string) {
	// No-op
}

// RecordAllowed is a no-op implementation.
func (m *NoOpMetrics) RecordAllowed(limiterType, endpoint string) {
	// No-op
}

// RecordCheckDuration is a no-op implementation.
func (m *NoOpMetrics) RecordCheckDuration(limiterType string, duration time.Duration) {
	// No-op
}

// SetActiveKeys is a no-op implementation.
func (m *NoOpMetrics) SetActiveKeys(limiterType string, count int) {
	// No-op
}

// RecordCircuitState is a no-op implementation.
func (m *NoOpMetrics) RecordCircuitState(limiterType, state string) {
	// No-op
}

// RecordDegradationLevel is a no-op implementation.
func (m *NoOpMetrics) RecordDegradationLevel(limiterType string, level int) {
	// No-op
}

// RecordEviction is a no-op implementation.
func (m *NoOpMetrics) RecordEviction(limiterType string, count int) {
	// No-op
}
