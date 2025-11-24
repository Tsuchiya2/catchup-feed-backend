// Package metrics provides Prometheus metrics registry and recording utilities.
//
// This package centralizes all application metrics including:
//   - HTTP request metrics (duration, count, size)
//   - Business metrics (articles, sources, summaries)
//   - Database query metrics
//   - Application performance metrics
//
// All metrics are automatically registered with the Prometheus default registry
// and exposed via the /metrics endpoint.
//
// Example usage:
//
//	import "catchup-feed/internal/observability/metrics"
//
//	func processArticles(source string) {
//	    start := time.Now()
//	    // ... process articles ...
//	    count := 10
//
//	    metrics.RecordArticlesFetched(source, count)
//	    metrics.RecordOperationDuration("process_articles", time.Since(start))
//	}
package metrics
