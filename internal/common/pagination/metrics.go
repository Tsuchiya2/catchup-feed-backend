package pagination

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestsTotal counts the total number of pagination requests.
	// Labels: status (HTTP status code), page_range (page bucket: 1-10, 11-50, etc.)
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "article_pagination_requests_total",
			Help: "Total number of pagination requests",
		},
		[]string{"status", "page_range"},
	)

	// DurationSeconds tracks request duration distribution.
	// Labels: operation (handler, service, repository)
	DurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "article_pagination_duration_seconds",
			Help:    "Request duration distribution",
			Buckets: []float64{0.01, 0.05, 0.1, 0.2, 0.5, 1.0, 2.0},
		},
		[]string{"operation"},
	)

	// TotalCount tracks the current total number of articles.
	// This is updated on each COUNT query.
	TotalCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "article_total_count",
			Help: "Current total number of articles",
		},
	)

	// ErrorsTotal counts pagination errors by type.
	// Labels: type (validation, database, timeout)
	ErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "article_pagination_errors_total",
			Help: "Total number of pagination errors",
		},
		[]string{"type"},
	)
)

// RecordRequest records a pagination request metric.
func RecordRequest(statusCode int, page int) {
	pageRange := getPageRangeBucket(page)
	RequestsTotal.WithLabelValues(
		fmt.Sprintf("%d", statusCode),
		pageRange,
	).Inc()
}

// RecordDuration records operation duration in seconds.
func RecordDuration(operation string, duration float64) {
	DurationSeconds.WithLabelValues(operation).Observe(duration)
}

// UpdateTotalCount updates the article count gauge.
func UpdateTotalCount(count int64) {
	TotalCount.Set(float64(count))
}

// RecordError records an error metric.
// errorType should be one of: "validation", "database", "timeout"
func RecordError(errorType string) {
	ErrorsTotal.WithLabelValues(errorType).Inc()
}

// getPageRangeBucket returns the page range bucket for a given page number.
func getPageRangeBucket(page int) string {
	switch {
	case page <= 10:
		return "1-10"
	case page <= 50:
		return "11-50"
	case page <= 100:
		return "51-100"
	default:
		return "100+"
	}
}
