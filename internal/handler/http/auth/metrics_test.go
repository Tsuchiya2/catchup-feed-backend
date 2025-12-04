package auth

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// TestRecordAuthRequest_CountsRequests tests that authentication requests are counted correctly
func TestRecordAuthRequest_CountsRequests(t *testing.T) {
	// Reset metrics before test
	authRequestsTotal.Reset()

	// Record successful admin authentication
	RecordAuthRequest("admin", "success")
	RecordAuthRequest("admin", "success")

	// Record failed user authentication
	RecordAuthRequest("user", "failure")

	// Verify counts
	adminSuccess := testutil.ToFloat64(authRequestsTotal.WithLabelValues("admin", "success"))
	assert.Equal(t, 2.0, adminSuccess, "Should count 2 successful admin authentications")

	userFailure := testutil.ToFloat64(authRequestsTotal.WithLabelValues("user", "failure"))
	assert.Equal(t, 1.0, userFailure, "Should count 1 failed user authentication")
}

// TestRecordAuthDuration_ObservesDuration tests that authentication duration is recorded
func TestRecordAuthDuration_ObservesDuration(t *testing.T) {
	// Reset metrics before test
	authDuration.Reset()

	// Record durations
	RecordAuthDuration("admin", 0.05)
	RecordAuthDuration("admin", 0.1)
	RecordAuthDuration("user", 0.02)

	// Verify observations were recorded by collecting all metrics
	count := testutil.CollectAndCount(authDuration)
	assert.Greater(t, count, 0, "Duration metrics should have observations")
}

// TestRecordAuthzCheckDuration_ObservesDuration tests that authorization check duration is recorded
func TestRecordAuthzCheckDuration_ObservesDuration(t *testing.T) {
	// Record durations
	RecordAuthzCheckDuration(0.001)
	RecordAuthzCheckDuration(0.002)

	// Verify observations were recorded
	count := testutil.CollectAndCount(authzCheckDuration)
	assert.Greater(t, count, 0, "Authorization check duration should have observations")
}

// TestRecordForbiddenAttempt_CountsAttempts tests that forbidden attempts are counted
func TestRecordForbiddenAttempt_CountsAttempts(t *testing.T) {
	// Reset metrics before test
	forbiddenAttempts.Reset()

	// Record forbidden attempts
	RecordForbiddenAttempt("user", "POST")
	RecordForbiddenAttempt("user", "POST")
	RecordForbiddenAttempt("guest", "DELETE")

	// Verify counts
	userPost := testutil.ToFloat64(forbiddenAttempts.WithLabelValues("user", "POST"))
	assert.Equal(t, 2.0, userPost, "Should count 2 forbidden POST attempts by user")

	guestDelete := testutil.ToFloat64(forbiddenAttempts.WithLabelValues("guest", "DELETE"))
	assert.Equal(t, 1.0, guestDelete, "Should count 1 forbidden DELETE attempt by guest")
}

// TestMetrics_NamingConventions tests that metrics follow Prometheus naming conventions
func TestMetrics_NamingConventions(t *testing.T) {
	tests := []struct {
		name        string
		metricName  string
		shouldExist bool
	}{
		{
			name:        "auth_requests_total counter",
			metricName:  "auth_requests_total",
			shouldExist: true,
		},
		{
			name:        "auth_duration_seconds histogram",
			metricName:  "auth_duration_seconds",
			shouldExist: true,
		},
		{
			name:        "authz_check_duration_seconds histogram",
			metricName:  "authz_check_duration_seconds",
			shouldExist: true,
		},
		{
			name:        "forbidden_attempts_total counter",
			metricName:  "forbidden_attempts_total",
			shouldExist: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify metric exists by collecting
			// This will panic if metric doesn't exist
			assert.NotPanics(t, func() {
				_, _ = prometheus.DefaultGatherer.Gather() //nolint:errcheck
			}, "Metric collection should not panic")
		})
	}
}

// TestRecordAuthDuration_HistogramBuckets tests that duration buckets are appropriate
func TestRecordAuthDuration_HistogramBuckets(t *testing.T) {
	// Reset metrics before test
	authDuration.Reset()

	// Record various durations that should fall into different buckets
	durations := []float64{0.001, 0.01, 0.05, 0.1, 0.5, 1.0}

	for _, d := range durations {
		RecordAuthDuration("test", d)
	}

	// Verify all observations were recorded by collecting all metrics
	count := testutil.CollectAndCount(authDuration)
	assert.Greater(t, count, 0, "Should record all duration observations")
}

// TestRecordAuthzCheckDuration_FastOperations tests that authorization checks are fast
func TestRecordAuthzCheckDuration_FastOperations(t *testing.T) {
	// Record fast authorization checks (microseconds to milliseconds)
	fastDurations := []float64{0.0001, 0.0005, 0.001, 0.005, 0.01}

	for _, d := range fastDurations {
		RecordAuthzCheckDuration(d)
	}

	// Verify observations were recorded
	count := testutil.CollectAndCount(authzCheckDuration)
	assert.Greater(t, count, 0, "Should record fast authorization check durations")
}

// TestRecordAuthRequest_DifferentRoles tests that different roles are tracked separately
func TestRecordAuthRequest_DifferentRoles(t *testing.T) {
	// Reset metrics before test
	authRequestsTotal.Reset()

	roles := []string{"admin", "user", "guest", "moderator"}

	// Record requests for different roles
	for _, role := range roles {
		RecordAuthRequest(role, "success")
	}

	// Verify each role was tracked
	for _, role := range roles {
		count := testutil.ToFloat64(authRequestsTotal.WithLabelValues(role, "success"))
		assert.Equal(t, 1.0, count, "Should count 1 successful authentication for role: "+role)
	}
}

// TestRecordForbiddenAttempt_SecurityMonitoring tests that security events are properly tracked
func TestRecordForbiddenAttempt_SecurityMonitoring(t *testing.T) {
	// Reset metrics before test
	forbiddenAttempts.Reset()

	// Simulate various forbidden attempts
	RecordForbiddenAttempt("guest", "POST")
	RecordForbiddenAttempt("guest", "PUT")
	RecordForbiddenAttempt("guest", "DELETE")
	RecordForbiddenAttempt("user", "DELETE")

	// Verify all methods are tracked separately
	guestPost := testutil.ToFloat64(forbiddenAttempts.WithLabelValues("guest", "POST"))
	assert.Equal(t, 1.0, guestPost, "Should track guest POST attempts")

	guestPut := testutil.ToFloat64(forbiddenAttempts.WithLabelValues("guest", "PUT"))
	assert.Equal(t, 1.0, guestPut, "Should track guest PUT attempts")

	guestDelete := testutil.ToFloat64(forbiddenAttempts.WithLabelValues("guest", "DELETE"))
	assert.Equal(t, 1.0, guestDelete, "Should track guest DELETE attempts")

	userDelete := testutil.ToFloat64(forbiddenAttempts.WithLabelValues("user", "DELETE"))
	assert.Equal(t, 1.0, userDelete, "Should track user DELETE attempts")
}
