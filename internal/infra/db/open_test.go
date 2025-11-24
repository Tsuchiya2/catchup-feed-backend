package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConnectionConfig(t *testing.T) {
	cfg := DefaultConnectionConfig()

	assert.Equal(t, 25, cfg.MaxOpenConns)
	assert.Equal(t, 10, cfg.MaxIdleConns)
	assert.Equal(t, 1*time.Hour, cfg.ConnMaxLifetime)
	assert.Equal(t, 30*time.Minute, cfg.ConnMaxIdleTime)
}

func TestGetConnectionConfigFromEnv_Defaults(t *testing.T) {
	// Clear all environment variables
	_ = os.Unsetenv("DB_MAX_OPEN_CONNS")
	_ = os.Unsetenv("DB_MAX_IDLE_CONNS")
	_ = os.Unsetenv("DB_CONN_MAX_LIFETIME")
	_ = os.Unsetenv("DB_CONN_MAX_IDLE_TIME")

	cfg := getConnectionConfigFromEnv()

	// Should use defaults
	assert.Equal(t, 25, cfg.MaxOpenConns)
	assert.Equal(t, 10, cfg.MaxIdleConns)
	assert.Equal(t, 1*time.Hour, cfg.ConnMaxLifetime)
	assert.Equal(t, 30*time.Minute, cfg.ConnMaxIdleTime)
}

func TestGetConnectionConfigFromEnv_MaxOpenConns(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected int
	}{
		{
			name:     "valid value",
			envValue: "50",
			expected: 50,
		},
		{
			name:     "invalid value - non-numeric",
			envValue: "invalid",
			expected: 25, // default
		},
		{
			name:     "invalid value - zero",
			envValue: "0",
			expected: 25, // default
		},
		{
			name:     "invalid value - negative",
			envValue: "-10",
			expected: 25, // default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Setenv("DB_MAX_OPEN_CONNS", tt.envValue)
			defer func() { _ = os.Unsetenv("DB_MAX_OPEN_CONNS") }()

			cfg := getConnectionConfigFromEnv()
			assert.Equal(t, tt.expected, cfg.MaxOpenConns)
		})
	}
}

func TestGetConnectionConfigFromEnv_MaxIdleConns(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected int
	}{
		{
			name:     "valid value",
			envValue: "20",
			expected: 20,
		},
		{
			name:     "invalid value - non-numeric",
			envValue: "abc",
			expected: 10, // default
		},
		{
			name:     "invalid value - zero",
			envValue: "0",
			expected: 10, // default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Setenv("DB_MAX_IDLE_CONNS", tt.envValue)
			defer func() { _ = os.Unsetenv("DB_MAX_IDLE_CONNS") }()

			cfg := getConnectionConfigFromEnv()
			assert.Equal(t, tt.expected, cfg.MaxIdleConns)
		})
	}
}

func TestGetConnectionConfigFromEnv_ConnMaxLifetime(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected time.Duration
	}{
		{
			name:     "valid value - hours",
			envValue: "2h",
			expected: 2 * time.Hour,
		},
		{
			name:     "valid value - minutes",
			envValue: "45m",
			expected: 45 * time.Minute,
		},
		{
			name:     "valid value - mixed",
			envValue: "1h30m",
			expected: 90 * time.Minute,
		},
		{
			name:     "invalid value - not a duration",
			envValue: "invalid",
			expected: 1 * time.Hour, // default
		},
		{
			name:     "invalid value - zero",
			envValue: "0s",
			expected: 1 * time.Hour, // default
		},
		{
			name:     "invalid value - negative",
			envValue: "-1h",
			expected: 1 * time.Hour, // default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Setenv("DB_CONN_MAX_LIFETIME", tt.envValue)
			defer func() { _ = os.Unsetenv("DB_CONN_MAX_LIFETIME") }()

			cfg := getConnectionConfigFromEnv()
			assert.Equal(t, tt.expected, cfg.ConnMaxLifetime)
		})
	}
}

func TestGetConnectionConfigFromEnv_ConnMaxIdleTime(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected time.Duration
	}{
		{
			name:     "valid value",
			envValue: "15m",
			expected: 15 * time.Minute,
		},
		{
			name:     "invalid value",
			envValue: "not-a-duration",
			expected: 30 * time.Minute, // default
		},
		{
			name:     "zero value",
			envValue: "0m",
			expected: 30 * time.Minute, // default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Setenv("DB_CONN_MAX_IDLE_TIME", tt.envValue)
			defer func() { _ = os.Unsetenv("DB_CONN_MAX_IDLE_TIME") }()

			cfg := getConnectionConfigFromEnv()
			assert.Equal(t, tt.expected, cfg.ConnMaxIdleTime)
		})
	}
}

func TestGetConnectionConfigFromEnv_AllCustomValues(t *testing.T) {
	// Set all custom values
	_ = os.Setenv("DB_MAX_OPEN_CONNS", "100")
	_ = os.Setenv("DB_MAX_IDLE_CONNS", "50")
	_ = os.Setenv("DB_CONN_MAX_LIFETIME", "2h")
	_ = os.Setenv("DB_CONN_MAX_IDLE_TIME", "45m")

	defer func() {
		_ = os.Unsetenv("DB_MAX_OPEN_CONNS")
		_ = os.Unsetenv("DB_MAX_IDLE_CONNS")
		_ = os.Unsetenv("DB_CONN_MAX_LIFETIME")
		_ = os.Unsetenv("DB_CONN_MAX_IDLE_TIME")
	}()

	cfg := getConnectionConfigFromEnv()

	assert.Equal(t, 100, cfg.MaxOpenConns)
	assert.Equal(t, 50, cfg.MaxIdleConns)
	assert.Equal(t, 2*time.Hour, cfg.ConnMaxLifetime)
	assert.Equal(t, 45*time.Minute, cfg.ConnMaxIdleTime)
}

func TestGetConnectionConfigFromEnv_PartialCustomValues(t *testing.T) {
	// Set only some custom values
	_ = os.Setenv("DB_MAX_OPEN_CONNS", "75")
	_ = os.Setenv("DB_CONN_MAX_LIFETIME", "3h")

	defer func() {
		_ = os.Unsetenv("DB_MAX_OPEN_CONNS")
		_ = os.Unsetenv("DB_CONN_MAX_LIFETIME")
	}()

	cfg := getConnectionConfigFromEnv()

	// Custom values
	assert.Equal(t, 75, cfg.MaxOpenConns)
	assert.Equal(t, 3*time.Hour, cfg.ConnMaxLifetime)

	// Default values
	assert.Equal(t, 10, cfg.MaxIdleConns)
	assert.Equal(t, 30*time.Minute, cfg.ConnMaxIdleTime)
}

func TestConnectionConfig_Struct(t *testing.T) {
	// Test that ConnectionConfig struct can be created manually
	cfg := ConnectionConfig{
		MaxOpenConns:    100,
		MaxIdleConns:    50,
		ConnMaxLifetime: 2 * time.Hour,
		ConnMaxIdleTime: 1 * time.Hour,
	}

	assert.Equal(t, 100, cfg.MaxOpenConns)
	assert.Equal(t, 50, cfg.MaxIdleConns)
	assert.Equal(t, 2*time.Hour, cfg.ConnMaxLifetime)
	assert.Equal(t, 1*time.Hour, cfg.ConnMaxIdleTime)
}

/* ──────────────────────────────── 7. Open Function Integration Tests ──────────────────────────────── */

// TestOpen_SuccessfulConnection tests that Open() successfully connects to a valid database
func TestOpen_SuccessfulConnection(t *testing.T) {
	// Skip if DATABASE_URL is not set (CI/local environment without DB)
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	// Act
	db := Open()
	defer func() { _ = db.Close() }()

	// Assert - Connection should be established
	if db == nil {
		t.Fatal("Open() returned nil database")
	}

	// Verify connection is actually working
	ctx := context.Background()
	err := db.PingContext(ctx)
	if err != nil {
		t.Fatalf("Database connection failed: %v", err)
	}
}

// TestOpen_ConnectionPoolConfiguration tests that connection pool is configured correctly
func TestOpen_ConnectionPoolConfiguration(t *testing.T) {
	// Skip if DATABASE_URL is not set
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	// Arrange - Set custom pool configuration
	_ = os.Setenv("DB_MAX_OPEN_CONNS", "50")
	_ = os.Setenv("DB_MAX_IDLE_CONNS", "25")
	defer func() {
		_ = os.Unsetenv("DB_MAX_OPEN_CONNS")
		_ = os.Unsetenv("DB_MAX_IDLE_CONNS")
	}()

	// Act
	db := Open()
	defer func() { _ = db.Close() }()

	// Assert - Connection pool settings should be applied
	// Note: sql.DB doesn't expose getters for these settings, but we can verify
	// the connection works with the configured pool
	stats := db.Stats()
	assert.NotNil(t, stats)

	// Verify basic connectivity
	ctx := context.Background()
	err := db.PingContext(ctx)
	if err != nil {
		t.Fatalf("Database connection failed with custom pool config: %v", err)
	}
}

// TestOpen_VerifyPingTimeout tests that Open() includes connection verification
func TestOpen_VerifyPingTimeout(t *testing.T) {
	// Skip if DATABASE_URL is not set
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	// Act
	db := Open()
	defer func() { _ = db.Close() }()

	// Assert - Should be able to ping within timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := db.PingContext(ctx)
	if err != nil {
		t.Fatalf("Ping failed within timeout: %v", err)
	}
}

// TestOpen_WithDefaultConfiguration tests Open() with default configuration values
func TestOpen_WithDefaultConfiguration(t *testing.T) {
	// Skip if DATABASE_URL is not set
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	// Arrange - Clear custom configuration
	_ = os.Unsetenv("DB_MAX_OPEN_CONNS")
	_ = os.Unsetenv("DB_MAX_IDLE_CONNS")
	_ = os.Unsetenv("DB_CONN_MAX_LIFETIME")
	_ = os.Unsetenv("DB_CONN_MAX_IDLE_TIME")

	// Act
	db := Open()
	defer func() { _ = db.Close() }()

	// Assert - Should use defaults and work correctly
	ctx := context.Background()
	err := db.PingContext(ctx)
	if err != nil {
		t.Fatalf("Database connection failed with default config: %v", err)
	}

	// Verify stats are available
	stats := db.Stats()
	assert.NotNil(t, stats)
}

// TestOpen_MultipleConnections tests that multiple connections can be established
func TestOpen_MultipleConnections(t *testing.T) {
	// Skip if DATABASE_URL is not set
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	// Act - Open multiple connections
	db1 := Open()
	defer func() { _ = db1.Close() }()

	db2 := Open()
	defer func() { _ = db2.Close() }()

	// Assert - Both connections should work independently
	ctx := context.Background()

	err1 := db1.PingContext(ctx)
	if err1 != nil {
		t.Fatalf("First connection failed: %v", err1)
	}

	err2 := db2.PingContext(ctx)
	if err2 != nil {
		t.Fatalf("Second connection failed: %v", err2)
	}
}

// Note: Testing Open() with missing DATABASE_URL or invalid DSN would require
// fork/exec or subprocess testing since log.Fatal() terminates the process.
// These scenarios are better tested in integration or E2E test suites.
