// Package metrics provides centralized Prometheus metrics for the application.
package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTP metrics track HTTP request patterns and performance
var (
	// HTTPRequestsTotal counts total HTTP requests by method, path, and status
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	// HTTPRequestDuration measures HTTP request duration in seconds
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	// HTTPRequestSize measures HTTP request body size in bytes
	HTTPRequestSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_size_bytes",
			Help:    "HTTP request size in bytes",
			Buckets: prometheus.ExponentialBuckets(100, 10, 8),
		},
		[]string{"method", "path"},
	)

	// HTTPResponseSize measures HTTP response body size in bytes
	HTTPResponseSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_response_size_bytes",
			Help:    "HTTP response size in bytes",
			Buckets: prometheus.ExponentialBuckets(100, 10, 8),
		},
		[]string{"method", "path"},
	)

	// ActiveConnections tracks the number of active HTTP connections
	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_active_connections",
			Help: "Number of active HTTP connections",
		},
	)
)

// Business metrics track application-specific operations
var (
	// ArticlesTotal tracks total number of articles in database
	ArticlesTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "articles_total",
			Help: "Total number of articles in the database",
		},
	)

	// SourcesTotal tracks total number of sources in database
	SourcesTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "sources_total",
			Help: "Total number of sources in the database",
		},
	)

	// ArticlesFetchedTotal counts articles fetched from each source
	ArticlesFetchedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "articles_fetched_total",
			Help: "Total number of articles fetched from sources",
		},
		[]string{"source", "source_id"},
	)

	// ArticlesSummarizedTotal counts articles summarized by status
	ArticlesSummarizedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "articles_summarized_total",
			Help: "Total number of articles summarized",
		},
		[]string{"status"},
	)

	// SummarizationDuration measures time to summarize an article
	SummarizationDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "summarization_duration_seconds",
			Help:    "Time taken to summarize an article",
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
		},
	)

	// FeedCrawlDuration measures time to crawl a feed source
	FeedCrawlDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "feed_crawl_duration_seconds",
			Help:    "Time taken to crawl a feed source",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
		},
		[]string{"source_id"},
	)

	// FeedCrawlErrors counts errors during feed crawling
	FeedCrawlErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "feed_crawl_errors_total",
			Help: "Total number of feed crawl errors",
		},
		[]string{"source_id", "error_type"},
	)

	// ContentFetchAttemptsTotal counts content fetch attempts by result
	ContentFetchAttemptsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "content_fetch_attempts_total",
			Help: "Total number of content fetch attempts",
		},
		[]string{"result"}, // result: success, failure, skipped
	)

	// ContentFetchDuration measures time to fetch article content
	ContentFetchDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "content_fetch_duration_seconds",
			Help:    "Time taken to fetch article content",
			Buckets: []float64{0.1, 0.2, 0.4, 0.8, 1.6, 3.2, 6.4, 12.8},
		},
	)

	// ContentFetchSize measures fetched content size in bytes
	ContentFetchSize = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name: "content_fetch_size_bytes",
			Help: "Fetched article content size in bytes",
			Buckets: []float64{
				100, 200, 400, 800, 1600, 3200, 6400, 12800,
				25600, 51200, 102400, 204800, 409600, 819200,
				1638400, 3276800, 6553600, 10485760, // up to 10MB
			},
		},
	)
)

// Database metrics track database performance
var (
	// DBQueryDuration measures database query duration
	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
		},
		[]string{"operation"},
	)

	// DBConnectionsActive tracks active database connections
	DBConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "db_connections_active",
			Help: "Number of active database connections",
		},
	)

	// DBConnectionsIdle tracks idle database connections
	DBConnectionsIdle = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "db_connections_idle",
			Help: "Number of idle database connections",
		},
	)
)

// RecordHTTPRequest records an HTTP request with its metadata
func RecordHTTPRequest(method, path, status string, duration time.Duration, requestSize, responseSize int) {
	HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
	HTTPRequestDuration.WithLabelValues(method, path, status).Observe(duration.Seconds())

	if requestSize > 0 {
		HTTPRequestSize.WithLabelValues(method, path).Observe(float64(requestSize))
	}
	if responseSize > 0 {
		HTTPResponseSize.WithLabelValues(method, path).Observe(float64(responseSize))
	}
}

// RecordOperationDuration records the duration of a named operation
func RecordOperationDuration(operation string, duration time.Duration) {
	DBQueryDuration.WithLabelValues(operation).Observe(duration.Seconds())
}
