package tracing

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// tracer is the global tracer instance for the catchup-feed application.
var tracer = otel.Tracer("catchup-feed")

// GetTracer returns the global tracer for creating spans.
// This tracer can be used throughout the application to create new spans.
//
// Example usage:
//
//	ctx, span := tracing.GetTracer().Start(ctx, "operation-name")
//	defer span.End()
func GetTracer() trace.Tracer {
	return tracer
}
