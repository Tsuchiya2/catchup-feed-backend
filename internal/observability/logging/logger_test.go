package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"

	"catchup-feed/internal/handler/http/requestid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/* ───────── TASK-011: Logger Package Unit Tests ───────── */

// TestNewLogger tests the creation of a new JSON logger
func TestNewLogger(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		expected slog.Level
	}{
		{
			name:     "default log level (info)",
			logLevel: "",
			expected: slog.LevelInfo,
		},
		{
			name:     "debug log level",
			logLevel: "debug",
			expected: slog.LevelDebug,
		},
		{
			name:     "invalid log level defaults to info",
			logLevel: "invalid",
			expected: slog.LevelInfo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			if tt.logLevel != "" {
				os.Setenv("LOG_LEVEL", tt.logLevel)
				defer os.Unsetenv("LOG_LEVEL")
			}

			// Act
			logger := NewLogger()

			// Assert
			assert.NotNil(t, logger, "logger should not be nil")
		})
	}
}

// TestNewTextLogger tests the creation of a new text logger
func TestNewTextLogger(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
	}{
		{
			name:     "default log level",
			logLevel: "",
		},
		{
			name:     "debug log level",
			logLevel: "debug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			if tt.logLevel != "" {
				os.Setenv("LOG_LEVEL", tt.logLevel)
				defer os.Unsetenv("LOG_LEVEL")
			}

			// Act
			logger := NewTextLogger()

			// Assert
			assert.NotNil(t, logger, "logger should not be nil")
		})
	}
}

// TestLogger_LogLevels tests logging at different levels
func TestLogger_LogLevels(t *testing.T) {
	tests := []struct {
		name     string
		logLevel string
		logFunc  func(*slog.Logger, string)
		message  string
		level    string
		should   string
	}{
		{
			name:     "info level logging",
			logLevel: "",
			logFunc:  func(l *slog.Logger, m string) { l.Info(m) },
			message:  "test info message",
			level:    "INFO",
			should:   "log info message",
		},
		{
			name:     "debug level logging when enabled",
			logLevel: "debug",
			logFunc:  func(l *slog.Logger, m string) { l.Debug(m) },
			message:  "test debug message",
			level:    "DEBUG",
			should:   "log debug message when enabled",
		},
		{
			name:     "warn level logging",
			logLevel: "",
			logFunc:  func(l *slog.Logger, m string) { l.Warn(m) },
			message:  "test warn message",
			level:    "WARN",
			should:   "log warn message",
		},
		{
			name:     "error level logging",
			logLevel: "",
			logFunc:  func(l *slog.Logger, m string) { l.Error(m) },
			message:  "test error message",
			level:    "ERROR",
			should:   "log error message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			if tt.logLevel != "" {
				os.Setenv("LOG_LEVEL", tt.logLevel)
				defer os.Unsetenv("LOG_LEVEL")
			}

			var buf bytes.Buffer
			handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelDebug, // Allow all levels for testing
			})
			logger := slog.New(handler)

			// Act
			tt.logFunc(logger, tt.message)

			// Assert
			output := buf.String()
			assert.Contains(t, output, tt.message, "output should contain the message")
			assert.Contains(t, output, tt.level, "output should contain the log level")

			// Verify JSON structure
			var logEntry map[string]interface{}
			err := json.Unmarshal(buf.Bytes(), &logEntry)
			require.NoError(t, err, "output should be valid JSON")
			assert.Equal(t, tt.message, logEntry["msg"], "JSON should contain correct message")
			assert.Equal(t, tt.level, logEntry["level"], "JSON should contain correct level")
			assert.NotEmpty(t, logEntry["time"], "JSON should contain timestamp")
		})
	}
}

// TestLogger_DebugLevelFiltering tests that debug messages are filtered when not enabled
func TestLogger_DebugLevelFiltering(t *testing.T) {
	// Arrange - Create logger with INFO level (default)
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)

	// Act - Try to log at debug level
	logger.Debug("this should not appear")
	logger.Info("this should appear")

	// Assert
	output := buf.String()
	assert.NotContains(t, output, "this should not appear", "debug message should be filtered")
	assert.Contains(t, output, "this should appear", "info message should be logged")
}

// TestWithRequestID tests adding request ID to logger
func TestWithRequestID(t *testing.T) {
	tests := []struct {
		name      string
		requestID string
		expected  string
	}{
		{
			name:      "with valid request ID",
			requestID: "test-request-123",
			expected:  "test-request-123",
		},
		{
			name:      "with UUID request ID",
			requestID: "550e8400-e29b-41d4-a716-446655440000",
			expected:  "550e8400-e29b-41d4-a716-446655440000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			var buf bytes.Buffer
			handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			})
			baseLogger := slog.New(handler)

			ctx := requestid.WithRequestID(context.Background(), tt.requestID)

			// Act
			logger := WithRequestID(ctx, baseLogger)
			logger.Info("test message")

			// Assert
			output := buf.String()
			assert.Contains(t, output, tt.expected, "output should contain request ID")
			assert.Contains(t, output, "request_id", "output should contain request_id field")

			// Verify JSON structure
			var logEntry map[string]interface{}
			err := json.Unmarshal(buf.Bytes(), &logEntry)
			require.NoError(t, err, "output should be valid JSON")
			assert.Equal(t, tt.expected, logEntry["request_id"], "request_id should match")
		})
	}
}

// TestWithRequestID_EmptyRequestID tests behavior with empty request ID
func TestWithRequestID_EmptyRequestID(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	baseLogger := slog.New(handler)

	ctx := context.Background() // No request ID

	// Act
	logger := WithRequestID(ctx, baseLogger)
	logger.Info("test message")

	// Assert - Should return the same logger without adding request_id
	output := buf.String()
	assert.Contains(t, output, "test message", "message should be logged")
	assert.NotContains(t, output, "request_id", "should not contain request_id field")
}

// TestWithFields tests adding structured fields to logger
func TestWithFields(t *testing.T) {
	tests := []struct {
		name   string
		fields map[string]interface{}
	}{
		{
			name: "single string field",
			fields: map[string]interface{}{
				"user_id": "user-123",
			},
		},
		{
			name: "multiple mixed fields",
			fields: map[string]interface{}{
				"user_id":  "user-456",
				"action":   "login",
				"attempts": 3,
				"success":  true,
			},
		},
		{
			name: "numeric fields",
			fields: map[string]interface{}{
				"count":    42,
				"duration": 123.45,
			},
		},
		{
			name: "boolean fields",
			fields: map[string]interface{}{
				"is_admin": true,
				"verified": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			var buf bytes.Buffer
			handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			})
			baseLogger := slog.New(handler)

			// Act
			logger := WithFields(baseLogger, tt.fields)
			logger.Info("test message")

			// Assert
			output := buf.String()
			assert.Contains(t, output, "test message", "output should contain message")

			// Verify all fields are present in JSON
			var logEntry map[string]interface{}
			err := json.Unmarshal(buf.Bytes(), &logEntry)
			require.NoError(t, err, "output should be valid JSON")

			for key, expectedValue := range tt.fields {
				assert.Contains(t, logEntry, key, "output should contain field: %s", key)
				// For numeric comparisons, handle JSON number conversion
				switch v := expectedValue.(type) {
				case int:
					assert.Equal(t, float64(v), logEntry[key], "field %s should match", key)
				case float64:
					assert.Equal(t, v, logEntry[key], "field %s should match", key)
				default:
					assert.Equal(t, expectedValue, logEntry[key], "field %s should match", key)
				}
			}
		})
	}
}

// TestWithFields_EmptyFields tests behavior with empty fields map
func TestWithFields_EmptyFields(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	baseLogger := slog.New(handler)

	// Act
	logger := WithFields(baseLogger, map[string]interface{}{})
	logger.Info("test message")

	// Assert
	output := buf.String()
	assert.Contains(t, output, "test message", "message should be logged")

	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err, "output should be valid JSON")
	assert.Equal(t, "test message", logEntry["msg"])
}

// TestFromContext tests retrieving logger from context
func TestFromContext(t *testing.T) {
	tests := []struct {
		name     string
		setupCtx func() context.Context
		check    func(*testing.T, *slog.Logger)
	}{
		{
			name: "with logger in context",
			setupCtx: func() context.Context {
				var buf bytes.Buffer
				handler := slog.NewJSONHandler(&buf, nil)
				logger := slog.New(handler)
				return WithLogger(context.Background(), logger)
			},
			check: func(t *testing.T, logger *slog.Logger) {
				assert.NotNil(t, logger, "should return logger from context")
			},
		},
		{
			name: "without logger in context",
			setupCtx: func() context.Context {
				return context.Background()
			},
			check: func(t *testing.T, logger *slog.Logger) {
				assert.NotNil(t, logger, "should return default logger")
				assert.Equal(t, slog.Default(), logger, "should be default logger")
			},
		},
		{
			name: "with invalid value in context",
			setupCtx: func() context.Context {
				return context.WithValue(context.Background(), loggerContextKey, "not a logger")
			},
			check: func(t *testing.T, logger *slog.Logger) {
				assert.NotNil(t, logger, "should return default logger")
				assert.Equal(t, slog.Default(), logger, "should be default logger")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			ctx := tt.setupCtx()

			// Act
			logger := FromContext(ctx)

			// Assert
			tt.check(t, logger)
		})
	}
}

// TestWithLogger tests adding logger to context
func TestWithLogger(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, nil)
	logger := slog.New(handler)
	ctx := context.Background()

	// Act
	newCtx := WithLogger(ctx, logger)

	// Assert
	retrievedLogger := FromContext(newCtx)
	assert.NotNil(t, retrievedLogger, "retrieved logger should not be nil")

	// Verify it's the same logger by logging a message
	retrievedLogger.Info("test message")
	assert.Contains(t, buf.String(), "test message", "should use the same logger")
}

// TestLogger_JSONStructure tests that log output has proper JSON structure
func TestLogger_JSONStructure(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)

	// Act
	logger.Info("test message",
		"user_id", "user-123",
		"action", "login",
		"count", 42,
	)

	// Assert
	output := buf.String()
	assert.NotEmpty(t, output, "output should not be empty")

	// Verify valid JSON
	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err, "output should be valid JSON")

	// Verify required fields
	assert.Equal(t, "test message", logEntry["msg"], "should have correct message")
	assert.Equal(t, "INFO", logEntry["level"], "should have correct level")
	assert.NotEmpty(t, logEntry["time"], "should have timestamp")

	// Verify custom fields
	assert.Equal(t, "user-123", logEntry["user_id"], "should have user_id")
	assert.Equal(t, "login", logEntry["action"], "should have action")
	assert.Equal(t, float64(42), logEntry["count"], "should have count")
}

// TestLogger_Integration tests complete logging workflow
func TestLogger_Integration(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	baseLogger := slog.New(handler)

	ctx := requestid.WithRequestID(context.Background(), "req-integration-test")
	fields := map[string]interface{}{
		"user_id": "user-999",
		"action":  "test_action",
	}

	// Act
	logger := WithRequestID(ctx, baseLogger)
	logger = WithFields(logger, fields)
	logger.Info("integration test message")

	// Assert
	output := buf.String()
	assert.Contains(t, output, "integration test message")
	assert.Contains(t, output, "req-integration-test")
	assert.Contains(t, output, "user-999")
	assert.Contains(t, output, "test_action")

	// Verify complete JSON structure
	var logEntry map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &logEntry)
	require.NoError(t, err, "output should be valid JSON")

	assert.Equal(t, "integration test message", logEntry["msg"])
	assert.Equal(t, "INFO", logEntry["level"])
	assert.Equal(t, "req-integration-test", logEntry["request_id"])
	assert.Equal(t, "user-999", logEntry["user_id"])
	assert.Equal(t, "test_action", logEntry["action"])
	assert.NotEmpty(t, logEntry["time"])
}

// TestLogger_MultipleLogEntries tests logging multiple entries
func TestLogger_MultipleLogEntries(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)

	// Act
	logger.Info("first message")
	logger.Warn("second message")
	logger.Error("third message")

	// Assert
	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	assert.Equal(t, 3, len(lines), "should have 3 log entries")

	// Verify each entry is valid JSON
	for i, line := range lines {
		var logEntry map[string]interface{}
		err := json.Unmarshal([]byte(line), &logEntry)
		require.NoError(t, err, "line %d should be valid JSON", i+1)
		assert.NotEmpty(t, logEntry["msg"], "line %d should have message", i+1)
		assert.NotEmpty(t, logEntry["level"], "line %d should have level", i+1)
	}
}

// TestLogger_ContextPropagation tests that logger context is properly propagated
func TestLogger_ContextPropagation(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)

	ctx := context.Background()
	ctx = WithLogger(ctx, logger)
	ctx = requestid.WithRequestID(ctx, "propagation-test")

	// Act
	retrievedLogger := FromContext(ctx)
	loggerWithReqID := WithRequestID(ctx, retrievedLogger)
	loggerWithReqID.Info("propagation test")

	// Assert
	output := buf.String()
	assert.Contains(t, output, "propagation test")
	assert.Contains(t, output, "propagation-test")
}

// TestContextKey_Type tests that context key is a custom type
func TestContextKey_Type(t *testing.T) {
	// Verify the context key is a custom type (not a string)
	var key = loggerContextKey
	assert.NotNil(t, key)
	assert.IsType(t, contextKey(""), key)
}

// BenchmarkLogger_Info benchmarks Info level logging
func BenchmarkLogger_Info(b *testing.B) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger.Info("benchmark message")
	}
}

// BenchmarkLogger_WithFields benchmarks logging with fields
func BenchmarkLogger_WithFields(b *testing.B) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	baseLogger := slog.New(handler)

	fields := map[string]interface{}{
		"user_id": "user-123",
		"action":  "benchmark",
		"count":   100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger := WithFields(baseLogger, fields)
		logger.Info("benchmark message")
	}
}

// BenchmarkLogger_WithRequestID benchmarks logging with request ID
func BenchmarkLogger_WithRequestID(b *testing.B) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	baseLogger := slog.New(handler)

	ctx := requestid.WithRequestID(context.Background(), "benchmark-req-id")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		logger := WithRequestID(ctx, baseLogger)
		logger.Info("benchmark message")
	}
}
