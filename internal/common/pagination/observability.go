package pagination

import (
	"log/slog"
	"time"
)

// LogRequest logs a pagination request with structured fields.
// This enables request tracing and debugging.
func LogRequest(logger *slog.Logger, requestID, userID string, params Params) {
	logger.Info("Paginated request",
		"request_id", requestID,
		"user_id", userID,
		"page", params.Page,
		"limit", params.Limit)
}

// LogResponse logs a pagination response with duration and status.
// This enables performance monitoring and debugging.
func LogResponse(logger *slog.Logger, requestID string, params Params, returnedCount int, duration time.Duration, statusCode int) {
	logger.Info("Paginated response",
		"request_id", requestID,
		"page", params.Page,
		"limit", params.Limit,
		"returned_count", returnedCount,
		"duration_ms", duration.Milliseconds(),
		"status", statusCode)
}

// LogError logs a pagination error with structured fields.
func LogError(logger *slog.Logger, requestID string, params Params, err error, errorType string) {
	logger.Error("Pagination error",
		"request_id", requestID,
		"page", params.Page,
		"limit", params.Limit,
		"error", err.Error(),
		"error_type", errorType)
}
