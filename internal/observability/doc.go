// Package observability provides production-grade observability infrastructure
// including structured logging, Prometheus metrics, and OpenTelemetry tracing.
//
// This package centralizes observability concerns to enable:
//   - Request tracing across service boundaries
//   - Structured logging with context propagation
//   - Prometheus metrics for monitoring
//   - Performance profiling and debugging
//
// Subpackages:
//   - logging: Structured logging utilities with slog
//   - metrics: Prometheus metrics registry and recorders
//   - tracing: OpenTelemetry tracing integration (future)
//
// Example usage:
//
//	import (
//	    "catchup-feed/internal/observability/logging"
//	    "catchup-feed/internal/observability/metrics"
//	)
//
//	func main() {
//	    logger := logging.NewLogger()
//	    logger.Info("application started")
//
//	    metrics.RecordArticlesFetched("example-source", 10)
//	}
package observability
