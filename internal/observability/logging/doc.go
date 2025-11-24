// Package logging provides structured logging utilities with context propagation.
//
// This package wraps the standard library's log/slog package with helper functions
// for common logging patterns used throughout the application.
//
// Key features:
//   - JSON and text output formats
//   - Request ID propagation
//   - Context-aware logging
//   - Configurable log levels
//
// Example usage:
//
//	import "catchup-feed/internal/observability/logging"
//
//	func main() {
//	    logger := logging.NewLogger()
//	    logger.Info("application started", slog.String("version", "1.0"))
//	}
//
//	func handleRequest(ctx context.Context) {
//	    logger := logging.WithRequestID(ctx, slog.Default())
//	    logger.Info("processing request")
//	}
package logging
