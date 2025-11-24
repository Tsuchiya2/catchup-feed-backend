package tracing

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

// newResponseWriter creates a new responseWriter with default status code 200.
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

// WriteHeader captures the status code and calls the underlying ResponseWriter.
func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Middleware creates OpenTelemetry tracing middleware for HTTP handlers.
// It extracts trace context from incoming requests, creates a new span,
// and propagates the trace ID in response headers.
//
// The middleware:
//   - Extracts trace context from incoming request headers (W3C Trace Context format)
//   - Creates a new server span for the request
//   - Adds trace ID to response headers (X-Trace-Id)
//   - Records HTTP method, path, and status code as span attributes
//   - Automatically ends the span when the request completes
//
// Example usage:
//
//	mux := http.NewServeMux()
//	mux.Handle("/", someHandler)
//	handler := tracing.Middleware(mux)
//	http.ListenAndServe(":8080", handler)
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract trace context from incoming request headers
		ctx := otel.GetTextMapPropagator().Extract(
			r.Context(),
			propagation.HeaderCarrier(r.Header),
		)

		// Start new span for this request
		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path,
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		// Add trace ID to response headers for client-side correlation
		traceID := span.SpanContext().TraceID().String()
		w.Header().Set("X-Trace-Id", traceID)

		// Wrap response writer to capture status code
		rw := newResponseWriter(w)

		// Call next handler with traced context
		r = r.WithContext(ctx)
		next.ServeHTTP(rw, r)

		// Add span attributes after request completes
		span.SetAttributes(
			attribute.Int("http.status_code", rw.statusCode),
			attribute.String("http.method", r.Method),
			attribute.String("http.path", r.URL.Path),
		)

		// Mark span as error if status code is 5xx
		if rw.statusCode >= 500 {
			span.SetAttributes(attribute.Bool("error", true))
		}
	})
}
