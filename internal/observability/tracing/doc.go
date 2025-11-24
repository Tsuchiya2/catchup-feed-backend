// Package tracing provides OpenTelemetry tracing integration.
//
// This package will provide distributed tracing capabilities using OpenTelemetry.
// Implementation is planned for Phase 7 Part 2.
//
// Planned features:
//   - Automatic HTTP request tracing
//   - Database query tracing
//   - Cross-service trace propagation
//   - Jaeger/Zipkin exporter integration
//
// Example usage (planned):
//
//	import "catchup-feed/internal/observability/tracing"
//
//	func main() {
//	    shutdown := tracing.InitTracer("catchup-feed")
//	    defer shutdown()
//	}
//
//	func processRequest(ctx context.Context) {
//	    ctx, span := tracing.StartSpan(ctx, "process-request")
//	    defer span.End()
//	    // ... process request ...
//	}
package tracing
