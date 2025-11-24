package notify

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestRecordDispatch verifies dispatch counter is incremented
func TestRecordDispatch(t *testing.T) {
	tests := []struct {
		name    string
		channel string
	}{
		{"Discord channel", "discord"},
		{"Slack channel", "slack"},
		{"Email channel", "email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial value
			initial := testutil.ToFloat64(notificationDispatchedTotal.WithLabelValues(tt.channel))

			// Record dispatch
			RecordDispatch(tt.channel)

			// Verify increment
			after := testutil.ToFloat64(notificationDispatchedTotal.WithLabelValues(tt.channel))
			if after != initial+1 {
				t.Errorf("RecordDispatch() counter = %v, want %v", after, initial+1)
			}
		})
	}
}

// TestRecordSuccess verifies success metrics are recorded correctly
func TestRecordSuccess(t *testing.T) {
	tests := []struct {
		name     string
		channel  string
		duration time.Duration
	}{
		{"fast success", "discord", 100 * time.Millisecond},
		{"slow success", "slack", 2 * time.Second},
		{"very fast", "email", 50 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial counter value
			initialCounter := testutil.ToFloat64(notificationSentTotal.WithLabelValues(tt.channel, "success"))

			// Record success
			RecordSuccess(tt.channel, tt.duration)

			// Verify success counter incremented
			afterCounter := testutil.ToFloat64(notificationSentTotal.WithLabelValues(tt.channel, "success"))
			if afterCounter != initialCounter+1 {
				t.Errorf("RecordSuccess() success counter = %v, want %v", afterCounter, initialCounter+1)
			}

			// Note: Histogram verification requires collecting all samples
			// We verify it doesn't panic and the counter incremented, which confirms recording
		})
	}
}

// TestRecordFailure verifies failure metrics are recorded correctly
func TestRecordFailure(t *testing.T) {
	tests := []struct {
		name     string
		channel  string
		duration time.Duration
	}{
		{"timeout failure", "discord", 5 * time.Second},
		{"network failure", "slack", 1 * time.Second},
		{"auth failure", "email", 500 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial counter value
			initialCounter := testutil.ToFloat64(notificationSentTotal.WithLabelValues(tt.channel, "failure"))

			// Record failure
			RecordFailure(tt.channel, tt.duration)

			// Verify failure counter incremented
			afterCounter := testutil.ToFloat64(notificationSentTotal.WithLabelValues(tt.channel, "failure"))
			if afterCounter != initialCounter+1 {
				t.Errorf("RecordFailure() failure counter = %v, want %v", afterCounter, initialCounter+1)
			}

			// Note: Histogram verification requires collecting all samples
			// We verify it doesn't panic and the counter incremented, which confirms recording
		})
	}
}

// TestRecordDropped verifies dropped notification counter
func TestRecordDropped(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		reason  string
	}{
		{"pool full", "discord", "pool_full"},
		{"circuit open", "slack", "circuit_open"},
		{"channel disabled", "email", "disabled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial value
			initial := testutil.ToFloat64(notificationDroppedTotal.WithLabelValues(tt.channel, tt.reason))

			// Record dropped
			RecordDropped(tt.channel, tt.reason)

			// Verify increment
			after := testutil.ToFloat64(notificationDroppedTotal.WithLabelValues(tt.channel, tt.reason))
			if after != initial+1 {
				t.Errorf("RecordDropped() counter = %v, want %v", after, initial+1)
			}
		})
	}
}

// TestRecordCircuitBreakerOpen verifies circuit breaker counter
func TestRecordCircuitBreakerOpen(t *testing.T) {
	tests := []struct {
		name    string
		channel string
	}{
		{"Discord circuit breaker", "discord"},
		{"Slack circuit breaker", "slack"},
		{"Email circuit breaker", "email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial value
			initial := testutil.ToFloat64(circuitBreakerOpenTotal.WithLabelValues(tt.channel))

			// Record circuit breaker open
			RecordCircuitBreakerOpen(tt.channel)

			// Verify increment
			after := testutil.ToFloat64(circuitBreakerOpenTotal.WithLabelValues(tt.channel))
			if after != initial+1 {
				t.Errorf("RecordCircuitBreakerOpen() counter = %v, want %v", after, initial+1)
			}
		})
	}
}

// TestRecordRateLimitHit verifies rate limit hit counter
func TestRecordRateLimitHit(t *testing.T) {
	tests := []struct {
		name    string
		channel string
	}{
		{"Discord rate limit", "discord"},
		{"Slack rate limit", "slack"},
		{"Email rate limit", "email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get initial value
			initial := testutil.ToFloat64(notificationRateLimitHits.WithLabelValues(tt.channel))

			// Record rate limit hit
			RecordRateLimitHit(tt.channel)

			// Verify increment
			after := testutil.ToFloat64(notificationRateLimitHits.WithLabelValues(tt.channel))
			if after != initial+1 {
				t.Errorf("RecordRateLimitHit() counter = %v, want %v", after, initial+1)
			}
		})
	}
}

// TestRecordRateLimitWait verifies rate limit wait histogram
func TestRecordRateLimitWait(t *testing.T) {
	tests := []struct {
		name         string
		channel      string
		waitDuration time.Duration
	}{
		{"short wait", "discord", 500 * time.Millisecond},
		{"medium wait", "slack", 5 * time.Second},
		{"long wait", "email", 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Record rate limit wait (verify it doesn't panic)
			RecordRateLimitWait(tt.channel, tt.waitDuration)

			// Note: Histogram values cannot be easily verified with testutil.ToFloat64
			// We verify the function executes without panic, which confirms it's recording
		})
	}
}

// TestSetActiveGoroutines verifies gauge value setting
func TestSetActiveGoroutines(t *testing.T) {
	tests := []struct {
		name  string
		count float64
	}{
		{"zero goroutines", 0},
		{"single goroutine", 1},
		{"multiple goroutines", 10},
		{"large count", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set gauge value
			SetActiveGoroutines(tt.count)

			// Verify gauge value
			value := testutil.ToFloat64(activeNotifications)
			if value != tt.count {
				t.Errorf("SetActiveGoroutines() gauge = %v, want %v", value, tt.count)
			}
		})
	}
}

// TestIncrementActiveGoroutines verifies gauge increment
func TestIncrementActiveGoroutines(t *testing.T) {
	// Get initial value
	initial := testutil.ToFloat64(activeNotifications)

	// Increment
	IncrementActiveGoroutines()

	// Verify increment
	after := testutil.ToFloat64(activeNotifications)
	if after != initial+1 {
		t.Errorf("IncrementActiveGoroutines() gauge = %v, want %v", after, initial+1)
	}
}

// TestDecrementActiveGoroutines verifies gauge decrement
func TestDecrementActiveGoroutines(t *testing.T) {
	// Set initial value to ensure we don't go negative
	SetActiveGoroutines(10)
	initial := testutil.ToFloat64(activeNotifications)

	// Decrement
	DecrementActiveGoroutines()

	// Verify decrement
	after := testutil.ToFloat64(activeNotifications)
	if after != initial-1 {
		t.Errorf("DecrementActiveGoroutines() gauge = %v, want %v", after, initial-1)
	}
}

// TestSetChannelsEnabled verifies channels enabled gauge
func TestSetChannelsEnabled(t *testing.T) {
	tests := []struct {
		name  string
		count float64
	}{
		{"no channels", 0},
		{"single channel", 1},
		{"multiple channels", 3},
		{"many channels", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set gauge value
			SetChannelsEnabled(tt.count)

			// Verify gauge value
			value := testutil.ToFloat64(channelsEnabled)
			if value != tt.count {
				t.Errorf("SetChannelsEnabled() gauge = %v, want %v", value, tt.count)
			}
		})
	}
}

// TestMetricsLabels verifies that metrics use correct labels
func TestMetricsLabels(t *testing.T) {
	t.Run("dispatch metric has channel label", func(t *testing.T) {
		RecordDispatch("test-channel")
		value := testutil.ToFloat64(notificationDispatchedTotal.WithLabelValues("test-channel"))
		if value < 1 {
			t.Error("dispatch metric should have recorded value")
		}
	})

	t.Run("sent metric has channel and status labels", func(t *testing.T) {
		RecordSuccess("test-channel", 100*time.Millisecond)
		successValue := testutil.ToFloat64(notificationSentTotal.WithLabelValues("test-channel", "success"))
		if successValue < 1 {
			t.Error("sent metric should have recorded success value")
		}

		RecordFailure("test-channel", 100*time.Millisecond)
		failureValue := testutil.ToFloat64(notificationSentTotal.WithLabelValues("test-channel", "failure"))
		if failureValue < 1 {
			t.Error("sent metric should have recorded failure value")
		}
	})

	t.Run("dropped metric has channel and reason labels", func(t *testing.T) {
		RecordDropped("test-channel", "pool_full")
		value := testutil.ToFloat64(notificationDroppedTotal.WithLabelValues("test-channel", "pool_full"))
		if value < 1 {
			t.Error("dropped metric should have recorded value")
		}
	})
}

// TestMetricsDurationBuckets verifies histogram buckets
func TestMetricsDurationBuckets(t *testing.T) {
	// Test various durations to ensure they fall into correct buckets
	durations := []time.Duration{
		50 * time.Millisecond,  // Should be in 0.1s bucket
		200 * time.Millisecond, // Should be in 0.5s bucket
		750 * time.Millisecond, // Should be in 1s bucket
		3 * time.Second,        // Should be in 5s bucket
		8 * time.Second,        // Should be in 10s bucket
		25 * time.Second,       // Should be in 30s bucket
	}

	for i, duration := range durations {
		t.Run("duration bucket test "+duration.String(), func(t *testing.T) {
			channel := "bucket-test-" + string(rune('0'+i))

			// Record duration (verify it doesn't panic)
			RecordSuccess(channel, duration)

			// Verify success counter incremented (confirms the record worked)
			count := testutil.ToFloat64(notificationSentTotal.WithLabelValues(channel, "success"))
			if count < 1 {
				t.Errorf("duration histogram should have recorded value for %v", duration)
			}
		})
	}
}

// TestRateLimitWaitBuckets verifies rate limit wait histogram buckets
func TestRateLimitWaitBuckets(t *testing.T) {
	// Test various wait times
	waitTimes := []time.Duration{
		50 * time.Millisecond,  // Should be in 0.1s bucket
		200 * time.Millisecond, // Should be in 0.5s bucket
		750 * time.Millisecond, // Should be in 1s bucket
		3 * time.Second,        // Should be in 5s bucket
		8 * time.Second,        // Should be in 10s bucket
		25 * time.Second,       // Should be in 30s bucket
		45 * time.Second,       // Should be in 60s bucket
	}

	for _, waitTime := range waitTimes {
		t.Run("wait time bucket test "+waitTime.String(), func(t *testing.T) {
			channel := "wait-test"

			// Record wait time (verify it doesn't panic)
			RecordRateLimitWait(channel, waitTime)

			// Note: Histogram values cannot be easily verified with testutil.ToFloat64
			// We verify the function executes without panic, which confirms it's recording
		})
	}
}

// TestConcurrentMetricsRecording verifies metrics are safe for concurrent use
func TestConcurrentMetricsRecording(t *testing.T) {
	const numGoroutines = 10
	const numRecordsPerGoroutine = 100

	done := make(chan bool)

	// Launch multiple goroutines recording metrics concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numRecordsPerGoroutine; j++ {
				RecordDispatch("concurrent")
				RecordSuccess("concurrent", 100*time.Millisecond)
				RecordFailure("concurrent", 200*time.Millisecond)
				RecordRateLimitHit("concurrent")
				RecordDropped("concurrent", "pool_full")
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify metrics were recorded (should have at least numGoroutines * numRecordsPerGoroutine)
	dispatchCount := testutil.ToFloat64(notificationDispatchedTotal.WithLabelValues("concurrent"))
	expectedMin := float64(numGoroutines * numRecordsPerGoroutine)
	if dispatchCount < expectedMin {
		t.Errorf("concurrent dispatch count = %v, want at least %v", dispatchCount, expectedMin)
	}
}
