package middleware

import (
	"log/slog"
)

// SlogAdapter adapts Go's standard log/slog Logger to the CORSLogger interface.
// It converts the map-based fields to slog.Any attributes for structured logging.
//
// Example usage:
//
//	logger := &SlogAdapter{Logger: slog.Default()}
//	logger.Warn("CORS: origin not allowed", map[string]interface{}{
//	    "origin": "http://malicious.com",
//	    "path": "/api/sources",
//	    "method": "GET",
//	})
type SlogAdapter struct {
	Logger *slog.Logger
}

// Info logs informational messages using slog.Info.
//
// Parameters:
//   - msg: The log message
//   - fields: A map of structured log fields
func (a *SlogAdapter) Info(msg string, fields map[string]interface{}) {
	if fields == nil {
		a.Logger.Info(msg)
		return
	}

	// Convert map fields to slog.Any attributes
	args := make([]interface{}, 0, len(fields))
	for k, v := range fields {
		args = append(args, slog.Any(k, v))
	}
	a.Logger.Info(msg, args...)
}

// Warn logs warning messages using slog.Warn.
//
// Parameters:
//   - msg: The log message
//   - fields: A map of structured log fields
func (a *SlogAdapter) Warn(msg string, fields map[string]interface{}) {
	if fields == nil {
		a.Logger.Warn(msg)
		return
	}

	// Convert map fields to slog.Any attributes
	args := make([]interface{}, 0, len(fields))
	for k, v := range fields {
		args = append(args, slog.Any(k, v))
	}
	a.Logger.Warn(msg, args...)
}

// Debug logs debug messages using slog.Debug.
//
// Parameters:
//   - msg: The log message
//   - fields: A map of structured log fields
func (a *SlogAdapter) Debug(msg string, fields map[string]interface{}) {
	if fields == nil {
		a.Logger.Debug(msg)
		return
	}

	// Convert map fields to slog.Any attributes
	args := make([]interface{}, 0, len(fields))
	for k, v := range fields {
		args = append(args, slog.Any(k, v))
	}
	a.Logger.Debug(msg, args...)
}

// NoOpLogger is a no-operation logger for testing purposes.
// All log methods are empty and do nothing.
//
// Example usage:
//
//	logger := &NoOpLogger{}
//	logger.Warn("test message", nil) // no-op
type NoOpLogger struct{}

// Info does nothing.
func (l *NoOpLogger) Info(msg string, fields map[string]interface{}) {}

// Warn does nothing.
func (l *NoOpLogger) Warn(msg string, fields map[string]interface{}) {}

// Debug does nothing.
func (l *NoOpLogger) Debug(msg string, fields map[string]interface{}) {}
