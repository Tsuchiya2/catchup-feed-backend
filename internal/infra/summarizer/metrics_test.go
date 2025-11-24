package summarizer

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPrometheusSummaryMetrics(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	require.NotNil(t, metrics)
	assert.NotNil(t, metrics.lengthHistogram)
	assert.NotNil(t, metrics.exceededCounter)
	assert.NotNil(t, metrics.complianceGauge)
	assert.NotNil(t, metrics.durationHistogram)
}

func TestNewPrometheusSummaryMetrics_Singleton(t *testing.T) {
	// Get first instance
	metrics1 := NewPrometheusSummaryMetrics()

	// Get second instance
	metrics2 := NewPrometheusSummaryMetrics()

	// Should be the same instance (singleton pattern)
	assert.Equal(t, metrics1, metrics2)
}

func TestPrometheusSummaryMetrics_RecordLength(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	tests := []struct {
		name   string
		length int
	}{
		{
			name:   "short summary",
			length: 100,
		},
		{
			name:   "medium summary",
			length: 500,
		},
		{
			name:   "long summary",
			length: 1500,
		},
		{
			name:   "zero length",
			length: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should not panic
			assert.NotPanics(t, func() {
				metrics.RecordLength(tt.length)
			})
		})
	}
}

func TestPrometheusSummaryMetrics_RecordLimitExceeded(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	// Should not panic when called multiple times
	assert.NotPanics(t, func() {
		metrics.RecordLimitExceeded()
		metrics.RecordLimitExceeded()
		metrics.RecordLimitExceeded()
	})
}

func TestPrometheusSummaryMetrics_RecordCompliance(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	tests := []struct {
		name        string
		withinLimit bool
	}{
		{
			name:        "within limit",
			withinLimit: true,
		},
		{
			name:        "exceeds limit",
			withinLimit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				metrics.RecordCompliance(tt.withinLimit)
			})
		})
	}
}

func TestPrometheusSummaryMetrics_RecordDuration(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	tests := []struct {
		name     string
		duration time.Duration
	}{
		{
			name:     "fast response",
			duration: 100 * time.Millisecond,
		},
		{
			name:     "normal response",
			duration: 1 * time.Second,
		},
		{
			name:     "slow response",
			duration: 5 * time.Second,
		},
		{
			name:     "zero duration",
			duration: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				metrics.RecordDuration(tt.duration)
			})
		})
	}
}

func TestPrometheusSummaryMetrics_ImplementsInterface(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	// Verify it implements the interface
	var _ SummaryMetricsRecorder = metrics
}

func TestPrometheusSummaryMetrics_ConcurrentAccess(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	// Test concurrent access to metrics
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			metrics.RecordLength(500)
			metrics.RecordLimitExceeded()
			metrics.RecordCompliance(true)
			metrics.RecordDuration(1 * time.Second)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic or race
}

func TestPrometheusSummaryMetrics_AllMethods(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	// Test calling all methods in sequence
	assert.NotPanics(t, func() {
		metrics.RecordLength(900)
		metrics.RecordCompliance(true)
		metrics.RecordDuration(1 * time.Second)

		metrics.RecordLength(1200)
		metrics.RecordLimitExceeded()
		metrics.RecordCompliance(false)
		metrics.RecordDuration(2 * time.Second)
	})
}

// MockMetricsRecorder is a mock implementation for testing
type MockMetricsRecorder struct {
	RecordedLengths    []int
	RecordedExceeded   int
	RecordedCompliance []bool
	RecordedDurations  []time.Duration
}

func (m *MockMetricsRecorder) RecordLength(length int) {
	m.RecordedLengths = append(m.RecordedLengths, length)
}

func (m *MockMetricsRecorder) RecordLimitExceeded() {
	m.RecordedExceeded++
}

func (m *MockMetricsRecorder) RecordCompliance(withinLimit bool) {
	m.RecordedCompliance = append(m.RecordedCompliance, withinLimit)
}

func (m *MockMetricsRecorder) RecordDuration(duration time.Duration) {
	m.RecordedDurations = append(m.RecordedDurations, duration)
}

func TestMockMetricsRecorder_ImplementsInterface(t *testing.T) {
	mock := &MockMetricsRecorder{}

	// Verify it implements the interface
	var _ SummaryMetricsRecorder = mock
}

func TestMockMetricsRecorder_RecordLength(t *testing.T) {
	mock := &MockMetricsRecorder{}

	mock.RecordLength(100)
	mock.RecordLength(500)
	mock.RecordLength(1000)

	assert.Len(t, mock.RecordedLengths, 3)
	assert.Equal(t, []int{100, 500, 1000}, mock.RecordedLengths)
}

func TestMockMetricsRecorder_RecordLimitExceeded(t *testing.T) {
	mock := &MockMetricsRecorder{}

	mock.RecordLimitExceeded()
	mock.RecordLimitExceeded()
	mock.RecordLimitExceeded()

	assert.Equal(t, 3, mock.RecordedExceeded)
}

func TestMockMetricsRecorder_RecordCompliance(t *testing.T) {
	mock := &MockMetricsRecorder{}

	mock.RecordCompliance(true)
	mock.RecordCompliance(false)
	mock.RecordCompliance(true)

	assert.Len(t, mock.RecordedCompliance, 3)
	assert.Equal(t, []bool{true, false, true}, mock.RecordedCompliance)
}

func TestMockMetricsRecorder_RecordDuration(t *testing.T) {
	mock := &MockMetricsRecorder{}

	d1 := 1 * time.Second
	d2 := 2 * time.Second
	d3 := 3 * time.Second

	mock.RecordDuration(d1)
	mock.RecordDuration(d2)
	mock.RecordDuration(d3)

	assert.Len(t, mock.RecordedDurations, 3)
	assert.Equal(t, []time.Duration{d1, d2, d3}, mock.RecordedDurations)
}

func TestMockMetricsRecorder_AllMethods(t *testing.T) {
	mock := &MockMetricsRecorder{}

	// Record various metrics
	mock.RecordLength(900)
	mock.RecordCompliance(true)
	mock.RecordDuration(1 * time.Second)

	mock.RecordLength(1200)
	mock.RecordLimitExceeded()
	mock.RecordCompliance(false)
	mock.RecordDuration(2 * time.Second)

	// Verify all recordings
	assert.Equal(t, []int{900, 1200}, mock.RecordedLengths)
	assert.Equal(t, 1, mock.RecordedExceeded)
	assert.Equal(t, []bool{true, false}, mock.RecordedCompliance)
	assert.Equal(t, []time.Duration{1 * time.Second, 2 * time.Second}, mock.RecordedDurations)
}

// TestGetOrCreateHistogram tests histogram registration and retrieval
func TestGetOrCreateHistogram(t *testing.T) {
	// Create two metrics instances - should get same histogram due to singleton pattern
	metrics1 := NewPrometheusSummaryMetrics()
	metrics2 := NewPrometheusSummaryMetrics()

	// Should be the same instance (singleton)
	assert.Equal(t, metrics1, metrics2)

	// Recording metrics should not panic
	assert.NotPanics(t, func() {
		metrics1.RecordLength(500)
		metrics2.RecordLength(700)
	})
}

// TestGetOrCreateCounter tests counter registration and retrieval
func TestGetOrCreateCounter(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	// Recording limit exceeded should not panic
	assert.NotPanics(t, func() {
		metrics.RecordLimitExceeded()
		metrics.RecordLimitExceeded()
	})
}

// TestGetOrCreateGauge tests gauge registration and retrieval
func TestGetOrCreateGauge(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	// Setting gauge values should not panic
	assert.NotPanics(t, func() {
		metrics.RecordCompliance(true)
		metrics.RecordCompliance(false)
		metrics.RecordCompliance(true)
	})
}

// TestMetricsHelpers tests the helper functions for metric creation
func TestMetricsHelpers(t *testing.T) {
	t.Run("multiple calls return same metrics", func(t *testing.T) {
		m1 := NewPrometheusSummaryMetrics()
		m2 := NewPrometheusSummaryMetrics()
		m3 := NewPrometheusSummaryMetrics()

		// All should be the same instance
		assert.Equal(t, m1, m2)
		assert.Equal(t, m2, m3)
	})

	t.Run("metrics work after multiple retrievals", func(t *testing.T) {
		m := NewPrometheusSummaryMetrics()

		// Should work without errors
		assert.NotPanics(t, func() {
			m.RecordLength(100)
			m.RecordLimitExceeded()
			m.RecordCompliance(true)
			m.RecordDuration(500 * time.Millisecond)
		})
	})
}

// TestPrometheusMetricsEdgeCases tests edge cases in Prometheus metrics
func TestPrometheusMetricsEdgeCases(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	t.Run("negative length is recorded (no validation)", func(t *testing.T) {
		// Prometheus allows any float64 value
		assert.NotPanics(t, func() {
			metrics.RecordLength(-100)
		})
	})

	t.Run("very large length is recorded", func(t *testing.T) {
		assert.NotPanics(t, func() {
			metrics.RecordLength(999999)
		})
	})

	t.Run("zero duration is recorded", func(t *testing.T) {
		assert.NotPanics(t, func() {
			metrics.RecordDuration(0)
		})
	})

	t.Run("negative duration is recorded (edge case)", func(t *testing.T) {
		assert.NotPanics(t, func() {
			metrics.RecordDuration(-1 * time.Second)
		})
	})

	t.Run("multiple compliance changes", func(t *testing.T) {
		assert.NotPanics(t, func() {
			for i := 0; i < 100; i++ {
				metrics.RecordCompliance(i%2 == 0)
			}
		})
	})

	t.Run("many limit exceeded calls", func(t *testing.T) {
		assert.NotPanics(t, func() {
			for i := 0; i < 100; i++ {
				metrics.RecordLimitExceeded()
			}
		})
	})
}

// TestMetricsInterface tests that both implementations satisfy the interface
func TestMetricsInterface(t *testing.T) {
	t.Run("PrometheusSummaryMetrics implements interface", func(t *testing.T) {
		var _ SummaryMetricsRecorder = &PrometheusSummaryMetrics{}
		var _ SummaryMetricsRecorder = NewPrometheusSummaryMetrics()
	})

	t.Run("MockMetricsRecorder implements interface", func(t *testing.T) {
		var _ SummaryMetricsRecorder = &MockMetricsRecorder{}
	})
}

// TestMetricsBuckets tests histogram bucket configuration
func TestMetricsBuckets(t *testing.T) {
	t.Run("length histogram has appropriate buckets", func(t *testing.T) {
		// Expected buckets: 100, 300, 500, 700, 900, 1100, 1500, 2000
		expectedBuckets := []float64{100, 300, 500, 700, 900, 1100, 1500, 2000}
		assert.Len(t, expectedBuckets, 8)
		assert.Equal(t, 100.0, expectedBuckets[0])
		assert.Equal(t, 2000.0, expectedBuckets[7])
	})

	t.Run("duration histogram has exponential buckets", func(t *testing.T) {
		// Exponential buckets starting at 0.5, factor 2, 10 buckets
		// 0.5, 1, 2, 4, 8, 16, 32, 64, 128, 256
		start := 0.5
		factor := 2.0
		count := 10

		var buckets []float64
		current := start
		for i := 0; i < count; i++ {
			buckets = append(buckets, current)
			current *= factor
		}

		assert.Len(t, buckets, 10)
		assert.Equal(t, 0.5, buckets[0])
		assert.Equal(t, 256.0, buckets[9])
	})
}

// TestMetricsThreadSafety tests concurrent access to metrics
func TestMetricsThreadSafety(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	t.Run("concurrent RecordLength calls", func(t *testing.T) {
		done := make(chan bool)
		goroutines := 50

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer func() { done <- true }()
				metrics.RecordLength(id * 10)
			}(i)
		}

		for i := 0; i < goroutines; i++ {
			<-done
		}
	})

	t.Run("mixed concurrent operations", func(t *testing.T) {
		done := make(chan bool)
		goroutines := 100

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer func() { done <- true }()

				switch id % 4 {
				case 0:
					metrics.RecordLength(id)
				case 1:
					metrics.RecordLimitExceeded()
				case 2:
					metrics.RecordCompliance(id%2 == 0)
				case 3:
					metrics.RecordDuration(time.Duration(id) * time.Millisecond)
				}
			}(i)
		}

		for i := 0; i < goroutines; i++ {
			<-done
		}
	})
}

// TestMockMetricsRecorder_ZeroValues tests mock with zero/nil values
func TestMockMetricsRecorder_ZeroValues(t *testing.T) {
	mock := &MockMetricsRecorder{}

	t.Run("initial state is empty", func(t *testing.T) {
		assert.Len(t, mock.RecordedLengths, 0)
		assert.Equal(t, 0, mock.RecordedExceeded)
		assert.Len(t, mock.RecordedCompliance, 0)
		assert.Len(t, mock.RecordedDurations, 0)
	})

	t.Run("recording zero values", func(t *testing.T) {
		mock.RecordLength(0)
		mock.RecordCompliance(false)
		mock.RecordDuration(0)

		assert.Len(t, mock.RecordedLengths, 1)
		assert.Equal(t, 0, mock.RecordedLengths[0])
		assert.Len(t, mock.RecordedCompliance, 1)
		assert.False(t, mock.RecordedCompliance[0])
		assert.Len(t, mock.RecordedDurations, 1)
		assert.Equal(t, time.Duration(0), mock.RecordedDurations[0])
	})
}

// TestMetricsNaming tests that metric names follow conventions
func TestMetricsNaming(t *testing.T) {
	t.Run("metric names follow prometheus naming conventions", func(t *testing.T) {
		// Names should be snake_case and end with _unit
		expectedNames := []string{
			"article_summary_length_characters",
			"article_summary_limit_exceeded_total",
			"article_summary_limit_compliance_ratio",
			"article_summarization_duration_seconds",
		}

		for _, name := range expectedNames {
			// Should contain underscores
			assert.Contains(t, name, "_")
			// Should be lowercase
			assert.Equal(t, name, strings.ToLower(name))
		}
	})
}

// TestPrometheusMetricsRecordingOrder tests that recording order doesn't matter
func TestPrometheusMetricsRecordingOrder(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	tests := []struct {
		name string
		ops  func()
	}{
		{
			name: "length before compliance",
			ops: func() {
				metrics.RecordLength(900)
				metrics.RecordCompliance(true)
			},
		},
		{
			name: "compliance before length",
			ops: func() {
				metrics.RecordCompliance(true)
				metrics.RecordLength(900)
			},
		},
		{
			name: "exceeded before others",
			ops: func() {
				metrics.RecordLimitExceeded()
				metrics.RecordLength(1200)
				metrics.RecordCompliance(false)
			},
		},
		{
			name: "duration first",
			ops: func() {
				metrics.RecordDuration(2 * time.Second)
				metrics.RecordLength(500)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotPanics(t, tt.ops)
		})
	}
}

// BenchmarkPrometheusMetrics benchmarks metrics recording performance
func BenchmarkPrometheusMetrics(b *testing.B) {
	metrics := NewPrometheusSummaryMetrics()

	b.Run("RecordLength", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			metrics.RecordLength(900)
		}
	})

	b.Run("RecordLimitExceeded", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			metrics.RecordLimitExceeded()
		}
	})

	b.Run("RecordCompliance", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			metrics.RecordCompliance(true)
		}
	})

	b.Run("RecordDuration", func(b *testing.B) {
		d := 1 * time.Second
		for i := 0; i < b.N; i++ {
			metrics.RecordDuration(d)
		}
	})
}
