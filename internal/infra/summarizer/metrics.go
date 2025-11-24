package summarizer

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// SummaryMetricsRecorder defines the interface for recording summary-related metrics.
// This interface abstracts the metrics recording implementation, enabling:
//   - Mocking in unit tests (inject mock recorder instead of Prometheus)
//   - Swapping metrics systems (DataDog, New Relic, OpenTelemetry, etc.)
//   - Reusability across different AI providers (Claude, OpenAI, Gemini)
//
// Example usage:
//
//	type ClaudeSummarizer struct {
//	    metricsRecorder SummaryMetricsRecorder
//	}
//
//	func (c *ClaudeSummarizer) Summarize(ctx context.Context, text string) (string, error) {
//	    // ... API call ...
//	    c.metricsRecorder.RecordLength(len([]rune(summary)))
//	    c.metricsRecorder.RecordCompliance(withinLimit)
//	    return summary, nil
//	}
//
// For testing with mocks:
//
//	type MockMetricsRecorder struct {
//	    RecordedLengths []int
//	}
//
//	func (m *MockMetricsRecorder) RecordLength(length int) {
//	    m.RecordedLengths = append(m.RecordedLengths, length)
//	}
type SummaryMetricsRecorder interface {
	// RecordLength records the length of a generated summary in characters.
	RecordLength(length int)

	// RecordLimitExceeded increments the counter when a summary exceeds the configured character limit.
	RecordLimitExceeded()

	// RecordCompliance records whether a summary is within the configured character limit.
	// This is used to calculate the compliance ratio gauge.
	RecordCompliance(withinLimit bool)

	// RecordDuration records the time taken to generate a summary.
	RecordDuration(duration time.Duration)
}

// PrometheusSummaryMetrics implements SummaryMetricsRecorder using Prometheus metrics.
// This is the production implementation that records metrics to Prometheus.
type PrometheusSummaryMetrics struct {
	lengthHistogram   prometheus.Histogram
	exceededCounter   prometheus.Counter
	complianceGauge   prometheus.Gauge
	durationHistogram prometheus.Histogram
}

var (
	prometheusMetricsInstance *PrometheusSummaryMetrics
	prometheusMetricsOnce     sync.Once
)

// getOrCreateHistogram gets an existing histogram or creates a new one if it doesn't exist
func getOrCreateHistogram(opts prometheus.HistogramOpts) prometheus.Histogram {
	h := prometheus.NewHistogram(opts)
	if err := prometheus.Register(h); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			return are.ExistingCollector.(prometheus.Histogram)
		}
		// If it's not an AlreadyRegisteredError, use promauto which handles this gracefully
		return promauto.NewHistogram(opts)
	}
	return h
}

// getOrCreateCounter gets an existing counter or creates a new one if it doesn't exist
func getOrCreateCounter(opts prometheus.CounterOpts) prometheus.Counter {
	c := prometheus.NewCounter(opts)
	if err := prometheus.Register(c); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			return are.ExistingCollector.(prometheus.Counter)
		}
		return promauto.NewCounter(opts)
	}
	return c
}

// getOrCreateGauge gets an existing gauge or creates a new one if it doesn't exist
func getOrCreateGauge(opts prometheus.GaugeOpts) prometheus.Gauge {
	g := prometheus.NewGauge(opts)
	if err := prometheus.Register(g); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			return are.ExistingCollector.(prometheus.Gauge)
		}
		return promauto.NewGauge(opts)
	}
	return g
}

// NewPrometheusSummaryMetrics creates a new Prometheus-based metrics recorder.
// It initializes and registers all required Prometheus metrics.
// Uses singleton pattern to avoid duplicate metric registration in tests.
func NewPrometheusSummaryMetrics() *PrometheusSummaryMetrics {
	prometheusMetricsOnce.Do(func() {
		prometheusMetricsInstance = &PrometheusSummaryMetrics{
			lengthHistogram: getOrCreateHistogram(prometheus.HistogramOpts{
				Name:    "article_summary_length_characters",
				Help:    "Distribution of summary lengths in characters (Unicode runes)",
				Buckets: []float64{100, 300, 500, 700, 900, 1100, 1500, 2000},
			}),
			exceededCounter: getOrCreateCounter(prometheus.CounterOpts{
				Name: "article_summary_limit_exceeded_total",
				Help: "Total number of summaries exceeding the configured character limit",
			}),
			complianceGauge: getOrCreateGauge(prometheus.GaugeOpts{
				Name: "article_summary_limit_compliance_ratio",
				Help: "Ratio of summaries within character limit (0.0-1.0, target: ≥0.95)",
			}),
			durationHistogram: getOrCreateHistogram(prometheus.HistogramOpts{
				Name:    "article_summarization_duration_seconds",
				Help:    "Time taken to generate a summary via AI API",
				Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
			}),
		}
	})
	return prometheusMetricsInstance
}

// RecordLength implements SummaryMetricsRecorder.RecordLength
func (p *PrometheusSummaryMetrics) RecordLength(length int) {
	p.lengthHistogram.Observe(float64(length))
}

// RecordLimitExceeded implements SummaryMetricsRecorder.RecordLimitExceeded
func (p *PrometheusSummaryMetrics) RecordLimitExceeded() {
	p.exceededCounter.Inc()
}

// RecordCompliance implements SummaryMetricsRecorder.RecordCompliance
func (p *PrometheusSummaryMetrics) RecordCompliance(withinLimit bool) {
	if withinLimit {
		p.complianceGauge.Set(1.0)
	} else {
		p.complianceGauge.Set(0.0)
	}
}

// RecordDuration implements SummaryMetricsRecorder.RecordDuration
func (p *PrometheusSummaryMetrics) RecordDuration(duration time.Duration) {
	p.durationHistogram.Observe(duration.Seconds())
}
