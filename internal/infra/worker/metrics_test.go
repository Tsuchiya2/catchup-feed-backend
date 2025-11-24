package worker

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewWorkerMetrics(t *testing.T) {
	// Verify that globalTestMetrics (created via NewWorkerMetrics) is initialized correctly
	// We use the global instance to avoid duplicate Prometheus registration
	metrics := globalTestMetrics

	// Verify that all fields are initialized
	if metrics == nil {
		t.Fatal("NewWorkerMetrics returned nil")
	}

	if metrics.ConfigMetrics == nil {
		t.Error("ConfigMetrics is nil")
	}

	if metrics.CronJobRunsTotal == nil {
		t.Error("CronJobRunsTotal is nil")
	}

	if metrics.CronJobDurationSeconds == nil {
		t.Error("CronJobDurationSeconds is nil")
	}

	if metrics.CronJobFeedsProcessedTotal == nil {
		t.Error("CronJobFeedsProcessedTotal is nil")
	}

	if metrics.CronJobLastSuccessTimestamp == nil {
		t.Error("CronJobLastSuccessTimestamp is nil")
	}

	// Should not panic when calling MustRegister (metrics are auto-registered via promauto)
	metrics.MustRegister()
}

func TestWorkerMetrics_RecordJobRun(t *testing.T) {
	// Create a custom registry for isolated testing
	reg := prometheus.NewRegistry()

	// Create metrics with custom registry
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_worker_cron_job_runs_total",
		Help: "Test counter",
	}, []string{"status"})
	reg.MustRegister(counter)

	metrics := &WorkerMetrics{
		CronJobRunsTotal: counter,
	}

	// Record some job runs
	metrics.RecordJobRun("success")
	metrics.RecordJobRun("success")
	metrics.RecordJobRun("failure")

	// Verify success counter
	successCount := testutil.ToFloat64(metrics.CronJobRunsTotal.WithLabelValues("success"))
	if successCount != 2 {
		t.Errorf("Expected success count 2, got %f", successCount)
	}

	// Verify failure counter
	failureCount := testutil.ToFloat64(metrics.CronJobRunsTotal.WithLabelValues("failure"))
	if failureCount != 1 {
		t.Errorf("Expected failure count 1, got %f", failureCount)
	}
}

func TestWorkerMetrics_RecordJobDuration(t *testing.T) {
	// Create a custom registry for isolated testing
	reg := prometheus.NewRegistry()

	// Create histogram with custom registry
	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_worker_cron_job_duration_seconds",
		Help:    "Test histogram",
		Buckets: []float64{1, 5, 30, 60, 300, 900, 1800},
	})
	reg.MustRegister(histogram)

	metrics := &WorkerMetrics{
		CronJobDurationSeconds: histogram,
	}

	// Record some durations
	metrics.RecordJobDuration(10.5)   // 10.5 seconds
	metrics.RecordJobDuration(120.0)  // 2 minutes
	metrics.RecordJobDuration(600.0)  // 10 minutes

	// For histogram, verify it doesn't panic and metrics are collected
	// We can't easily verify the exact count with testutil.ToFloat64 for histograms
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	// Find our histogram
	found := false
	for _, mf := range metricFamilies {
		if mf.GetName() == "test_worker_cron_job_duration_seconds" {
			found = true
			if mf.GetType() != 4 { // 4 = HISTOGRAM
				t.Errorf("Expected histogram type, got %v", mf.GetType())
			}
			// Verify we have observations
			if len(mf.GetMetric()) == 0 {
				t.Error("Expected metrics to be recorded")
			}
			if mf.GetMetric()[0].GetHistogram().GetSampleCount() != 3 {
				t.Errorf("Expected 3 observations, got %d", mf.GetMetric()[0].GetHistogram().GetSampleCount())
			}
		}
	}

	if !found {
		t.Error("Histogram metric not found in registry")
	}
}

func TestWorkerMetrics_RecordFeedsProcessed(t *testing.T) {
	// Create a custom registry for isolated testing
	reg := prometheus.NewRegistry()

	// Create counter with custom registry
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_worker_cron_job_feeds_processed_total",
		Help: "Test counter",
	})
	reg.MustRegister(counter)

	metrics := &WorkerMetrics{
		CronJobFeedsProcessedTotal: counter,
	}

	// Record feeds processed
	metrics.RecordFeedsProcessed(10)
	metrics.RecordFeedsProcessed(25)
	metrics.RecordFeedsProcessed(5)

	// Verify total
	total := testutil.ToFloat64(metrics.CronJobFeedsProcessedTotal)
	if total != 40 {
		t.Errorf("Expected total 40, got %f", total)
	}
}

func TestWorkerMetrics_RecordFeedsProcessed_ZeroValue(t *testing.T) {
	// Create a custom registry for isolated testing
	reg := prometheus.NewRegistry()

	// Create counter with custom registry
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_worker_cron_job_feeds_processed_zero",
		Help: "Test counter",
	})
	reg.MustRegister(counter)

	metrics := &WorkerMetrics{
		CronJobFeedsProcessedTotal: counter,
	}

	// Record zero feeds (should work)
	metrics.RecordFeedsProcessed(0)

	// Verify total is still 0
	total := testutil.ToFloat64(metrics.CronJobFeedsProcessedTotal)
	if total != 0 {
		t.Errorf("Expected total 0, got %f", total)
	}
}

func TestWorkerMetrics_RecordLastSuccess(t *testing.T) {
	// Create a custom registry for isolated testing
	reg := prometheus.NewRegistry()

	// Create gauge with custom registry
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_worker_cron_job_last_success_timestamp",
		Help: "Test gauge",
	})
	reg.MustRegister(gauge)

	metrics := &WorkerMetrics{
		CronJobLastSuccessTimestamp: gauge,
	}

	// Initially should be 0
	initialValue := testutil.ToFloat64(metrics.CronJobLastSuccessTimestamp)
	if initialValue != 0 {
		t.Errorf("Expected initial value 0, got %f", initialValue)
	}

	// Record last success
	metrics.RecordLastSuccess()

	// Should now be a positive timestamp
	afterValue := testutil.ToFloat64(metrics.CronJobLastSuccessTimestamp)
	if afterValue <= 0 {
		t.Errorf("Expected positive timestamp, got %f", afterValue)
	}
}

func TestWorkerMetrics_MultipleJobRuns(t *testing.T) {
	// Test realistic scenario with multiple job runs
	reg := prometheus.NewRegistry()

	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_worker_cron_job_runs_multiple",
		Help: "Test counter",
	}, []string{"status"})
	reg.MustRegister(counter)

	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_worker_cron_job_duration_multiple",
		Help:    "Test histogram",
		Buckets: []float64{1, 5, 30, 60, 300, 900, 1800},
	})
	reg.MustRegister(histogram)

	feedsCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_worker_cron_job_feeds_multiple",
		Help: "Test counter",
	})
	reg.MustRegister(feedsCounter)

	lastSuccessGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_worker_cron_job_last_success_multiple",
		Help: "Test gauge",
	})
	reg.MustRegister(lastSuccessGauge)

	metrics := &WorkerMetrics{
		CronJobRunsTotal:            counter,
		CronJobDurationSeconds:      histogram,
		CronJobFeedsProcessedTotal:  feedsCounter,
		CronJobLastSuccessTimestamp: lastSuccessGauge,
	}

	// Simulate multiple job runs
	// Job 1: Success
	metrics.RecordJobRun("success")
	metrics.RecordJobDuration(45.5)
	metrics.RecordFeedsProcessed(10)
	metrics.RecordLastSuccess()

	// Job 2: Success
	metrics.RecordJobRun("success")
	metrics.RecordJobDuration(38.2)
	metrics.RecordFeedsProcessed(12)
	metrics.RecordLastSuccess()

	// Job 3: Failure
	metrics.RecordJobRun("failure")
	metrics.RecordJobDuration(5.0)
	// Don't record feeds or last success on failure

	// Verify counters
	successCount := testutil.ToFloat64(metrics.CronJobRunsTotal.WithLabelValues("success"))
	if successCount != 2 {
		t.Errorf("Expected 2 successful runs, got %f", successCount)
	}

	failureCount := testutil.ToFloat64(metrics.CronJobRunsTotal.WithLabelValues("failure"))
	if failureCount != 1 {
		t.Errorf("Expected 1 failed run, got %f", failureCount)
	}

	// Verify duration observations (histogram)
	metricFamilies, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}
	for _, mf := range metricFamilies {
		if mf.GetName() == "test_worker_cron_job_duration_multiple" {
			if mf.GetMetric()[0].GetHistogram().GetSampleCount() != 3 {
				t.Errorf("Expected 3 duration observations, got %d", mf.GetMetric()[0].GetHistogram().GetSampleCount())
			}
		}
	}

	// Verify feeds processed total
	totalFeeds := testutil.ToFloat64(metrics.CronJobFeedsProcessedTotal)
	if totalFeeds != 22 {
		t.Errorf("Expected 22 total feeds, got %f", totalFeeds)
	}

	// Verify last success timestamp is set
	lastSuccess := testutil.ToFloat64(metrics.CronJobLastSuccessTimestamp)
	if lastSuccess <= 0 {
		t.Errorf("Expected positive last success timestamp, got %f", lastSuccess)
	}
}

func TestWorkerMetrics_ConcurrentAccess(t *testing.T) {
	// Test concurrent metric updates (should be safe due to Prometheus implementation)
	reg := prometheus.NewRegistry()

	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_worker_cron_job_runs_concurrent",
		Help: "Test counter",
	}, []string{"status"})
	reg.MustRegister(counter)

	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_worker_cron_job_duration_concurrent",
		Help:    "Test histogram",
		Buckets: []float64{1, 5, 30, 60, 300, 900, 1800},
	})
	reg.MustRegister(histogram)

	feedsCounter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_worker_cron_job_feeds_concurrent",
		Help: "Test counter",
	})
	reg.MustRegister(feedsCounter)

	lastSuccessGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "test_worker_cron_job_last_success_concurrent",
		Help: "Test gauge",
	})
	reg.MustRegister(lastSuccessGauge)

	metrics := &WorkerMetrics{
		CronJobRunsTotal:            counter,
		CronJobDurationSeconds:      histogram,
		CronJobFeedsProcessedTotal:  feedsCounter,
		CronJobLastSuccessTimestamp: lastSuccessGauge,
	}

	// Run concurrent updates
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			metrics.RecordJobRun("success")
			metrics.RecordJobDuration(10.0)
			metrics.RecordFeedsProcessed(1)
			metrics.RecordLastSuccess()
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify metrics were updated (exact values depend on timing, but should be non-zero)
	// This test mainly ensures no panics occur during concurrent access
	successCount := testutil.ToFloat64(metrics.CronJobRunsTotal.WithLabelValues("success"))
	if successCount != 10 {
		t.Errorf("Expected 10 successful runs, got %f", successCount)
	}

	totalFeeds := testutil.ToFloat64(metrics.CronJobFeedsProcessedTotal)
	if totalFeeds != 10 {
		t.Errorf("Expected 10 total feeds, got %f", totalFeeds)
	}
}
