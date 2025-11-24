package worker

import (
	"catchup-feed/internal/pkg/config"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// WorkerMetrics provides Prometheus metrics for the worker component.
// It embeds the standard ConfigMetrics for configuration monitoring and adds
// worker-specific metrics for cron job execution tracking.
//
// Embedded metrics (from ConfigMetrics):
//   - worker_config_load_timestamp: Unix timestamp of last configuration load
//   - worker_config_validation_errors_total: Total validation errors by field
//   - worker_config_fallbacks_total: Total fallback operations by field
//   - worker_config_fallback_active: 1 if any fallback active, 0 otherwise
//
// Worker-specific metrics:
//   - worker_cron_job_runs_total: Total cron job runs by status (success/failure)
//   - worker_cron_job_duration_seconds: Duration histogram of cron job execution
//   - worker_cron_job_feeds_processed_total: Total feeds processed per job run
//   - worker_cron_job_last_success_timestamp: Unix timestamp of last successful run
//
// Example usage:
//
//	metrics := NewWorkerMetrics()
//	metrics.MustRegister()
//
//	// Record configuration load
//	metrics.RecordLoadTimestamp()
//
//	// Record cron job execution
//	start := time.Now()
//	defer func() {
//	    duration := time.Since(start).Seconds()
//	    metrics.RecordJobRun("success")
//	    metrics.RecordJobDuration(duration)
//	    metrics.RecordFeedsProcessed(42)
//	    metrics.RecordLastSuccess()
//	}()
type WorkerMetrics struct {
	// Embedded configuration metrics
	*config.ConfigMetrics

	// CronJobRunsTotal counts the total number of cron job runs.
	// Type: Counter
	// Labels: status (success, failure)
	// Usage: Increment after each job run based on success/failure
	CronJobRunsTotal *prometheus.CounterVec

	// CronJobDurationSeconds measures the duration of cron job execution.
	// Type: Histogram
	// Labels: none
	// Buckets: 1s, 5s, 30s, 1m, 5m, 15m, 30m (optimized for typical crawl durations)
	// Usage: Observe duration at the end of each job run
	CronJobDurationSeconds prometheus.Histogram

	// CronJobFeedsProcessedTotal counts the total number of feeds processed per job.
	// Type: Counter
	// Labels: none
	// Usage: Add the number of feeds processed after each successful job
	CronJobFeedsProcessedTotal prometheus.Counter

	// CronJobLastSuccessTimestamp records the Unix timestamp of the last successful run.
	// Type: Gauge
	// Labels: none
	// Usage: Set to current time when a job completes successfully
	CronJobLastSuccessTimestamp prometheus.Gauge
}

// NewWorkerMetrics creates a new WorkerMetrics instance with all metrics initialized.
// Metrics are created but not registered with Prometheus. Call MustRegister() to register.
//
// Returns:
//   - *WorkerMetrics: Initialized metrics ready for registration
//
// Example:
//
//	metrics := NewWorkerMetrics()
//	metrics.MustRegister()  // Register with Prometheus
func NewWorkerMetrics() *WorkerMetrics {
	return &WorkerMetrics{
		ConfigMetrics: config.NewConfigMetrics("worker"),

		CronJobRunsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "worker_cron_job_runs_total",
			Help: "Total number of cron job runs by status (success/failure)",
		}, []string{"status"}),

		CronJobDurationSeconds: promauto.NewHistogram(prometheus.HistogramOpts{
			Name:    "worker_cron_job_duration_seconds",
			Help:    "Duration of cron job execution in seconds",
			Buckets: []float64{1, 5, 30, 60, 300, 900, 1800}, // 1s, 5s, 30s, 1m, 5m, 15m, 30m
		}),

		CronJobFeedsProcessedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "worker_cron_job_feeds_processed_total",
			Help: "Total number of feeds processed across all cron job runs",
		}),

		CronJobLastSuccessTimestamp: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "worker_cron_job_last_success_timestamp",
			Help: "Unix timestamp of the last successful cron job run",
		}),
	}
}

// MustRegister is a no-op method for API compatibility.
// Metrics are automatically registered via promauto when created in NewWorkerMetrics.
//
// This method exists to maintain consistency with the expected metrics initialization pattern:
//
//	metrics := NewWorkerMetrics()
//	metrics.MustRegister()
//
// Even though registration happens automatically, this explicit call makes the
// initialization intent clear and maintains compatibility with future changes.
func (m *WorkerMetrics) MustRegister() {
	// No-op: metrics are auto-registered via promauto
}

// RecordJobRun increments the job run counter for the given status.
// Status should be either "success" or "failure".
//
// Parameters:
//   - status: Job execution status ("success" or "failure")
//
// Example:
//
//	if err := runJob(); err != nil {
//	    metrics.RecordJobRun("failure")
//	} else {
//	    metrics.RecordJobRun("success")
//	}
func (m *WorkerMetrics) RecordJobRun(status string) {
	m.CronJobRunsTotal.WithLabelValues(status).Inc()
}

// RecordJobDuration observes the duration of a cron job execution.
// Duration should be in seconds.
//
// Parameters:
//   - seconds: Job execution duration in seconds
//
// Example:
//
//	start := time.Now()
//	// ... execute job ...
//	duration := time.Since(start).Seconds()
//	metrics.RecordJobDuration(duration)
func (m *WorkerMetrics) RecordJobDuration(seconds float64) {
	m.CronJobDurationSeconds.Observe(seconds)
}

// RecordFeedsProcessed adds the number of feeds processed to the total counter.
//
// Parameters:
//   - count: Number of feeds processed in this job run
//
// Example:
//
//	stats, err := svc.CrawlAllSources(ctx)
//	if err == nil {
//	    metrics.RecordFeedsProcessed(stats.Sources)
//	}
func (m *WorkerMetrics) RecordFeedsProcessed(count int) {
	m.CronJobFeedsProcessedTotal.Add(float64(count))
}

// RecordLastSuccess records the current time as the last successful job completion.
//
// Example:
//
//	if err := runJob(); err == nil {
//	    metrics.RecordLastSuccess()
//	}
func (m *WorkerMetrics) RecordLastSuccess() {
	m.CronJobLastSuccessTimestamp.SetToCurrentTime()
}
