package http

import (
	"net/http"
	"strconv"
	"time"

	"catchup-feed/internal/handler/http/pathutil"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Prometheus metrics
var (
	// HTTP request metrics
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	// httpRequestDuration tracks request latency with optimized buckets for API response times.
	// Buckets are designed to capture:
	// - Fast responses: 5ms, 10ms, 25ms
	// - Normal responses: 50ms, 100ms, 250ms
	// - Slow responses: 500ms, 1s, 2.5s, 5s, 10s
	// This enables accurate p95 and p99 latency measurements.
	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path", "status"},
	)

	// httpRequestsInFlight tracks the current number of HTTP requests being processed.
	// This metric helps identify:
	// - Load levels and capacity
	// - Request queuing issues
	// - Potential bottlenecks
	httpRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Current number of HTTP requests being served",
		},
	)

	httpRequestSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_size_bytes",
			Help:    "HTTP request size in bytes",
			Buckets: prometheus.ExponentialBuckets(100, 10, 8),
		},
		[]string{"method", "path"},
	)

	httpResponseSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_response_size_bytes",
			Help:    "HTTP response size in bytes",
			Buckets: prometheus.ExponentialBuckets(100, 10, 8),
		},
		[]string{"method", "path"},
	)

	// Application metrics
	activeConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_active_connections",
			Help: "Number of active HTTP connections",
		},
	)

	// Business metrics
	articlesTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "articles_total",
			Help: "Total number of articles in the database",
		},
	)

	sourcesTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "sources_total",
			Help: "Total number of sources in the database",
		},
	)

	articlesFetchedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "articles_fetched_total",
			Help: "Total number of articles fetched from sources",
		},
		[]string{"source"},
	)

	articlesSummarizedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "articles_summarized_total",
			Help: "Total number of articles summarized",
		},
		[]string{"status"},
	)

	summarizationDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "summarization_duration_seconds",
			Help:    "Time taken to summarize an article",
			Buckets: prometheus.ExponentialBuckets(0.5, 2, 10),
		},
	)

	dbQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: prometheus.ExponentialBuckets(0.001, 2, 10),
		},
		[]string{"operation"},
	)
)

// responseWriter wraps http.ResponseWriter to record status code and response size.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

// MetricsMiddleware records HTTP request metrics including duration, size, and status codes.
// It uses path normalization to prevent label cardinality explosion from ID-containing paths.
// The middleware tracks:
// - In-flight requests (gauge incremented/decremented per request)
// - Request duration with optimized histogram buckets
// - Request and response sizes
// - Status code distribution
func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Track in-flight requests
		httpRequestsInFlight.Inc()
		defer httpRequestsInFlight.Dec()

		// Track active connections (legacy metric, kept for compatibility)
		activeConnections.Inc()
		defer activeConnections.Dec()

		// Normalize path to prevent cardinality explosion
		// Example: /articles/123 -> /articles/:id
		normalizedPath := pathutil.NormalizePath(r.URL.Path)

		// Record request size
		if r.ContentLength > 0 {
			httpRequestSize.WithLabelValues(r.Method, normalizedPath).Observe(float64(r.ContentLength))
		}

		// Wrap response writer to capture status code and response size
		rw := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Measure request duration
		start := time.Now()
		next.ServeHTTP(rw, r)
		duration := time.Since(start).Seconds()

		// Record metrics (using normalized path to prevent cardinality explosion)
		status := strconv.Itoa(rw.statusCode)
		httpRequestsTotal.WithLabelValues(r.Method, normalizedPath, status).Inc()
		httpRequestDuration.WithLabelValues(r.Method, normalizedPath, status).Observe(duration)
		httpResponseSize.WithLabelValues(r.Method, normalizedPath).Observe(float64(rw.size))
	})
}

// MetricsHandler returns an HTTP handler for the Prometheus metrics endpoint.
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}

// RecordArticlesFetched records the number of articles fetched from a source.
func RecordArticlesFetched(source string, count int) {
	articlesFetchedTotal.WithLabelValues(source).Add(float64(count))
}

// RecordArticleSummarized records the result of an article summarization operation.
func RecordArticleSummarized(success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	articlesSummarizedTotal.WithLabelValues(status).Inc()
}

// RecordSummarizationDuration records the time taken to summarize an article.
func RecordSummarizationDuration(duration time.Duration) {
	summarizationDuration.Observe(duration.Seconds())
}

// RecordDBQuery records the duration of a database query operation.
func RecordDBQuery(operation string, duration time.Duration) {
	dbQueryDuration.WithLabelValues(operation).Observe(duration.Seconds())
}

// UpdateArticlesTotal updates the total count of articles in the database.
func UpdateArticlesTotal(count int) {
	articlesTotal.Set(float64(count))
}

// UpdateSourcesTotal updates the total count of sources in the database.
func UpdateSourcesTotal(count int) {
	sourcesTotal.Set(float64(count))
}
