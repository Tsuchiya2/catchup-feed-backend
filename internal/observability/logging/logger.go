// Package logging provides structured logging utilities using the standard library's log/slog package.
// It offers helper functions for creating loggers with consistent configuration and context propagation.
package logging

import (
	"context"
	"log/slog"
	"os"

	"catchup-feed/internal/handler/http/requestid"
)

// NewLogger creates a new structured logger with JSON output.
// The log level can be controlled via the LOG_LEVEL environment variable.
// Supported levels: debug, info, warn, error
// Default level: info
func NewLogger() *slog.Logger {
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
		// Add source code location for error and warn levels
		AddSource: logLevel <= slog.LevelWarn,
	})

	return slog.New(handler)
}

// NewTextLogger creates a new structured logger with human-readable text output.
// This is useful for local development and debugging.
func NewTextLogger() *slog.Logger {
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: logLevel <= slog.LevelWarn,
	})

	return slog.New(handler)
}

// WithRequestID returns a new logger that includes the request ID from the context.
// This enables request tracing across log entries.
func WithRequestID(ctx context.Context, logger *slog.Logger) *slog.Logger {
	reqID := requestid.FromContext(ctx)
	if reqID == "" {
		return logger
	}
	return logger.With("request_id", reqID)
}

// WithFields returns a new logger with additional structured fields.
// Fields are provided as key-value pairs.
func WithFields(logger *slog.Logger, fields map[string]interface{}) *slog.Logger {
	args := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return logger.With(args...)
}

// FromContext retrieves the logger from the context, or returns the default logger if not found.
// This enables passing loggers through the application via context.
func FromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerContextKey).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

// WithLogger adds a logger to the context.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey, logger)
}

type contextKey string

const loggerContextKey contextKey = "logger"
