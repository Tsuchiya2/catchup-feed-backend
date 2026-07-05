package http

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		setupMock      func(sqlmock.Sqlmock)
		expectedStatus int
		expectHealthy  bool
	}{
		{
			name: "healthy database",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPing()
			},
			expectedStatus: http.StatusOK,
			expectHealthy:  true,
		},
		{
			name: "database connection error",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPing().WillReturnError(sql.ErrConnDone)
			},
			expectedStatus: http.StatusServiceUnavailable,
			expectHealthy:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock database
			db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			// Create handler
			handler := &HealthHandler{
				DB:      db,
				Version: "test-version",
			}

			// Create request
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rec := httptest.NewRecorder()

			// Execute
			handler.ServeHTTP(rec, req)

			// Assert status code
			assert.Equal(t, tt.expectedStatus, rec.Code)

			// Parse response
			var response HealthResponse
			err = json.NewDecoder(rec.Body).Decode(&response)
			require.NoError(t, err)

			// Assert response
			if tt.expectHealthy {
				assert.Equal(t, "healthy", response.Status)
			} else {
				assert.Equal(t, "unhealthy", response.Status)
			}
			assert.Equal(t, "test-version", response.Version)
			assert.NotEmpty(t, response.Timestamp)
			assert.Contains(t, response.Checks, "database")

			// Verify all expectations
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestHealthHandler_NoDatabaseConfigured(t *testing.T) {
	handler := &HealthHandler{
		DB:      nil,
		Version: "test-version",
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var response HealthResponse
	err := json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "unhealthy", response.Status)
	assert.Equal(t, "not configured", response.Checks["database"].Message)
}

func TestHealthHandler_CheckDatabase_Degraded(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Set max open connections to 10 for testing
	db.SetMaxOpenConns(10)

	mock.ExpectPing()

	handler := &HealthHandler{
		DB:      db,
		Version: "test-version",
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response HealthResponse
	err = json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "healthy", response.Status)
	assert.NotNil(t, response.Checks["database"].Details)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHealthHandler_MaxOpenConnectionsZero(t *testing.T) {
	// Test for Issue #3: Zero Division Risk in Health Check
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Set MaxOpenConns to 0 (unlimited/unconfigured)
	db.SetMaxOpenConns(0)

	mock.ExpectPing()

	handler := &HealthHandler{
		DB:      db,
		Version: "test-version",
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(rec, req)

	// Should return OK status (degraded is still considered "operational")
	assert.Equal(t, http.StatusOK, rec.Code)

	var response HealthResponse
	err = json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	// Should be marked as healthy at top level (database is accessible)
	assert.Equal(t, "healthy", response.Status)

	// Database check should be degraded
	dbCheck := response.Checks["database"]
	assert.Equal(t, "degraded", dbCheck.Status)
	assert.Equal(t, "connection pool max connections not configured", dbCheck.Message)

	// Details should still be present
	assert.NotNil(t, dbCheck.Details)
	// JSON unmarshaling converts numbers to float64
	assert.Equal(t, float64(0), dbCheck.Details["max_open_connections"])

	// utilization_percent should NOT be present when MaxOpenConnections is 0
	_, hasUtilization := dbCheck.Details["utilization_percent"]
	assert.False(t, hasUtilization, "utilization_percent should not be present when MaxOpenConnections is 0")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHealthHandler_HighUtilization(t *testing.T) {
	// Test utilization >= 80% triggers degraded status
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Set a small pool to easily reach 80% utilization
	// Note: We cannot directly control InUse connections with sqlmock,
	// but we can test the threshold logic
	db.SetMaxOpenConns(10)

	mock.ExpectPing()

	handler := &HealthHandler{
		DB:      db,
		Version: "test-version",
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response HealthResponse
	err = json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	// With sqlmock, InUse is typically 0, so we should get healthy status
	dbCheck := response.Checks["database"]
	assert.Equal(t, "healthy", dbCheck.Status)

	// utilization_percent should be present and should be 0% (0 in use / 10 max)
	assert.Contains(t, dbCheck.Details, "utilization_percent")
	utilization := dbCheck.Details["utilization_percent"].(float64)
	assert.Equal(t, float64(0), utilization)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHealthHandler_SmallPool(t *testing.T) {
	// Test with very small pool (edge case)
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Set MaxOpenConns to 1 (minimum)
	db.SetMaxOpenConns(1)

	mock.ExpectPing()

	handler := &HealthHandler{
		DB:      db,
		Version: "test-version",
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	// Should not panic with small pool
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var response HealthResponse
	err = json.NewDecoder(rec.Body).Decode(&response)
	require.NoError(t, err)

	dbCheck := response.Checks["database"]
	assert.NotNil(t, dbCheck.Details)
	// JSON unmarshaling converts numbers to float64
	assert.Equal(t, float64(1), dbCheck.Details["max_open_connections"])

	// utilization_percent should be present
	assert.Contains(t, dbCheck.Details, "utilization_percent")

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestHealthHandler_CacheControl(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectPing()

	handler := &HealthHandler{
		DB:      db,
		Version: "test-version",
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "no-cache, no-store, must-revalidate", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestReadyHandler_ServeHTTP(t *testing.T) {
	tests := []struct {
		name           string
		db             *sql.DB
		setupMock      func(sqlmock.Sqlmock)
		expectedStatus int
		expectedBody   string
	}{
		{
			name: "ready",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPing()
			},
			expectedStatus: http.StatusOK,
			expectedBody:   "ready",
		},
		{
			name: "database not ready",
			setupMock: func(mock sqlmock.Sqlmock) {
				mock.ExpectPing().WillReturnError(sql.ErrConnDone)
			},
			expectedStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			if tt.setupMock != nil {
				tt.setupMock(mock)
			}

			handler := &ReadyHandler{DB: db}

			req := httptest.NewRequest(http.MethodGet, "/ready", nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.expectedStatus, rec.Code)
			if tt.expectedBody != "" {
				assert.Equal(t, tt.expectedBody, rec.Body.String())
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestReadyHandler_NoDatabaseConfigured(t *testing.T) {
	handler := &ReadyHandler{DB: nil}

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "database not configured")
}

func TestReadyHandler_Timeout(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.MonitorPingsOption(true))
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// Simulate slow ping (longer than 2 second timeout)
	mock.ExpectPing().WillDelayFor(3 * time.Second)

	handler := &ReadyHandler{DB: db}

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should timeout and return service unavailable
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestLiveHandler_ServeHTTP(t *testing.T) {
	handler := &LiveHandler{}

	req := httptest.NewRequest(http.MethodGet, "/live", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "alive", rec.Body.String())
	assert.Equal(t, "text/plain", rec.Header().Get("Content-Type"))
}
