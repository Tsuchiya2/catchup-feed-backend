package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestMiddleware_CreatesSpan(t *testing.T) {
	// Set up in-memory span exporter for testing
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(sdktrace.NewTracerProvider())

	// Create test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Wrap with tracing middleware
	handler := Middleware(testHandler)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	// Execute request
	handler.ServeHTTP(rr, req)

	// Force flush spans using background context
	ctx := context.Background()
	_ = tp.ForceFlush(ctx)

	// Verify span was created
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	// Verify span properties
	span := spans[0]
	if span.Name != "GET /test" {
		t.Errorf("expected span name 'GET /test', got '%s'", span.Name)
	}

	// Verify span attributes
	attrs := span.Attributes
	foundMethod := false
	foundPath := false
	foundStatus := false

	for _, attr := range attrs {
		switch attr.Key {
		case "http.method":
			foundMethod = true
			if attr.Value.AsString() != "GET" {
				t.Errorf("expected http.method=GET, got %s", attr.Value.AsString())
			}
		case "http.path":
			foundPath = true
			if attr.Value.AsString() != "/test" {
				t.Errorf("expected http.path=/test, got %s", attr.Value.AsString())
			}
		case "http.status_code":
			foundStatus = true
			if attr.Value.AsInt64() != 200 {
				t.Errorf("expected http.status_code=200, got %d", attr.Value.AsInt64())
			}
		}
	}

	if !foundMethod {
		t.Error("http.method attribute not found")
	}
	if !foundPath {
		t.Error("http.path attribute not found")
	}
	if !foundStatus {
		t.Error("http.status_code attribute not found")
	}
}

func TestMiddleware_AddsTraceIDToResponse(t *testing.T) {
	// Set up tracer provider
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(sdktrace.NewTracerProvider())

	// Create test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with tracing middleware
	handler := Middleware(testHandler)

	// Create test request
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	// Execute request
	handler.ServeHTTP(rr, req)

	// Verify X-Trace-Id header is present
	traceID := rr.Header().Get("X-Trace-Id")
	if traceID == "" {
		t.Error("X-Trace-Id header not found in response")
	}

	// Verify trace ID format (32 hex characters)
	if len(traceID) != 32 {
		t.Errorf("expected trace ID length 32, got %d", len(traceID))
	}
}

func TestMiddleware_PropagatesTraceContext(t *testing.T) {
	// Set up tracer provider
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		otel.SetTracerProvider(sdktrace.NewTracerProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	}()

	// Re-initialize global tracer with new provider
	tracer = otel.Tracer("catchup-feed")

	// Create test handler
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with tracing middleware
	handler := Middleware(testHandler)

	// Create test request with trace context headers
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rr := httptest.NewRecorder()

	// Execute request
	handler.ServeHTTP(rr, req)

	// Force flush spans using background context
	ctx := context.Background()
	_ = tp.ForceFlush(ctx)

	// Verify span was created with propagated trace context
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	// Verify trace ID matches the propagated one
	span := spans[0]
	expectedTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	actualTraceID := span.SpanContext.TraceID().String()
	if actualTraceID != expectedTraceID {
		t.Errorf("expected trace ID %s, got %s", expectedTraceID, actualTraceID)
	}
}

func TestMiddleware_MarksErrorSpansFor5xx(t *testing.T) {
	// Set up in-memory span exporter
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(sdktrace.NewTracerProvider())

	// Re-initialize global tracer with new provider
	tracer = otel.Tracer("catchup-feed")

	// Create test handler that returns 500
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	// Wrap with tracing middleware
	handler := Middleware(testHandler)

	// Create test request
	req := httptest.NewRequest("GET", "/error", nil)
	rr := httptest.NewRecorder()

	// Execute request
	handler.ServeHTTP(rr, req)

	// Force flush spans using background context
	ctx := context.Background()
	_ = tp.ForceFlush(ctx)

	// Verify span has error attribute
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	foundError := false

	for _, attr := range span.Attributes {
		if attr.Key == "error" && attr.Value.AsBool() {
			foundError = true
			break
		}
	}

	if !foundError {
		t.Error("expected error attribute for 5xx response")
	}
}

func TestMiddleware_NoErrorAttributeFor4xx(t *testing.T) {
	// Set up in-memory span exporter
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(sdktrace.NewTracerProvider())

	// Re-initialize global tracer with new provider
	tracer = otel.Tracer("catchup-feed")

	// Create test handler that returns 404
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	// Wrap with tracing middleware
	handler := Middleware(testHandler)

	// Create test request
	req := httptest.NewRequest("GET", "/notfound", nil)
	rr := httptest.NewRecorder()

	// Execute request
	handler.ServeHTTP(rr, req)

	// Force flush spans using background context
	ctx := context.Background()
	_ = tp.ForceFlush(ctx)

	// Verify span does NOT have error attribute
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	for _, attr := range span.Attributes {
		if attr.Key == "error" {
			t.Error("unexpected error attribute for 4xx response")
		}
	}
}

func TestResponseWriter_CapturesStatusCode(t *testing.T) {
	w := httptest.NewRecorder()
	rw := newResponseWriter(w)

	// Default status should be 200
	if rw.statusCode != http.StatusOK {
		t.Errorf("expected default status code 200, got %d", rw.statusCode)
	}

	// Write a custom status code
	rw.WriteHeader(http.StatusCreated)

	if rw.statusCode != http.StatusCreated {
		t.Errorf("expected status code 201, got %d", rw.statusCode)
	}
}
