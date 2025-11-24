package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sony/gobreaker"
)

func TestNewDBCircuitBreaker(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer func() { _ = db.Close() }()

	dcb := NewDBCircuitBreaker(db)

	if dcb == nil {
		t.Fatal("expected non-nil DBCircuitBreaker")
	}

	if dcb.db != db {
		t.Error("expected db to be set")
	}

	if dcb.cb == nil {
		t.Error("expected circuit breaker to be set")
	}

	if dcb.State() != gobreaker.StateClosed {
		t.Errorf("expected initial state to be Closed, got %s", dcb.State())
	}
}

func TestDBCircuitBreaker_QueryContext_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer func() { _ = db.Close() }()

	dcb := NewDBCircuitBreaker(db)
	ctx := context.Background()

	// Setup mock expectation
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "test")
	mock.ExpectQuery("SELECT (.+) FROM users").WillReturnRows(rows)

	// Execute query
	result, err := dcb.QueryContext(ctx, "SELECT id, name FROM users WHERE id = ?", 1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	defer func() { _ = result.Close() }()

	// Verify result
	if !result.Next() {
		t.Fatal("expected at least one row")
	}

	var id int
	var name string
	if err := result.Scan(&id, &name); err != nil {
		t.Fatalf("failed to scan row: %v", err)
	}

	if id != 1 || name != "test" {
		t.Errorf("expected id=1, name=test, got id=%d, name=%s", id, name)
	}

	// Verify circuit breaker state
	if dcb.State() != gobreaker.StateClosed {
		t.Errorf("expected state to remain Closed after success, got %s", dcb.State())
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestDBCircuitBreaker_QueryContext_Failure(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer func() { _ = db.Close() }()

	dcb := NewDBCircuitBreaker(db)
	ctx := context.Background()

	// Setup mock to return error
	expectedErr := errors.New("database connection failed")
	mock.ExpectQuery("SELECT (.+) FROM users").WillReturnError(expectedErr)

	// Execute query
	_, err = dcb.QueryContext(ctx, "SELECT id, name FROM users")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify circuit breaker recorded the failure
	if dcb.State() == gobreaker.StateOpen {
		t.Error("circuit should not be open after single failure")
	}
}

func TestDBCircuitBreaker_ExecContext_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer func() { _ = db.Close() }()

	dcb := NewDBCircuitBreaker(db)
	ctx := context.Background()

	// Setup mock expectation
	mock.ExpectExec("INSERT INTO users").
		WithArgs("test").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// Execute statement
	result, err := dcb.ExecContext(ctx, "INSERT INTO users (name) VALUES (?)", "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Verify result
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("failed to get rows affected: %v", err)
	}

	if rowsAffected != 1 {
		t.Errorf("expected 1 row affected, got %d", rowsAffected)
	}

	// Verify circuit breaker state
	if dcb.State() != gobreaker.StateClosed {
		t.Errorf("expected state to remain Closed after success, got %s", dcb.State())
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestDBCircuitBreaker_CircuitOpens_AfterConsecutiveFailures(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Create circuit breaker with custom config for faster testing
	cfg := Config{
		Name:             "test-db",
		MaxRequests:      3,
		Interval:         time.Minute,
		Timeout:          100 * time.Millisecond, // Short timeout for testing
		FailureThreshold: 1.0,                    // Open on 100% failure
		MinRequests:      5,                      // Trip after 5 consecutive failures
	}
	dcb := NewDBCircuitBreakerWithConfig(db, cfg)
	ctx := context.Background()

	// Setup mock to return error for 5 consecutive queries
	expectedErr := errors.New("database connection failed")
	for i := 0; i < 5; i++ {
		mock.ExpectQuery("SELECT (.+)").WillReturnError(expectedErr)
	}

	// Execute 5 failing queries
	for i := 0; i < 5; i++ {
		_, err := dcb.QueryContext(ctx, "SELECT * FROM users")
		if err == nil {
			t.Errorf("attempt %d: expected error, got nil", i+1)
		}
	}

	// Circuit should now be open
	if !dcb.IsOpen() {
		t.Errorf("expected circuit to be open after %d consecutive failures, state: %s", 5, dcb.State())
	}

	// Verify that next request fails immediately without hitting the database
	_, err = dcb.QueryContext(ctx, "SELECT * FROM users")
	if err == nil {
		t.Fatal("expected error when circuit is open")
	}
	if !errors.Is(err, gobreaker.ErrOpenState) {
		t.Errorf("expected ErrOpenState, got %v", err)
	}

	// No more mock expectations should be set since circuit is open
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestDBCircuitBreaker_CircuitHalfOpen_AfterTimeout(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Create circuit breaker with short timeout for testing
	cfg := Config{
		Name:             "test-db",
		MaxRequests:      3,
		Interval:         time.Minute,
		Timeout:          50 * time.Millisecond, // Very short timeout for testing
		FailureThreshold: 1.0,
		MinRequests:      5,
	}
	dcb := NewDBCircuitBreakerWithConfig(db, cfg)
	ctx := context.Background()

	// Trip the circuit (5 consecutive failures)
	expectedErr := errors.New("database connection failed")
	for i := 0; i < 5; i++ {
		mock.ExpectQuery("SELECT (.+)").WillReturnError(expectedErr)
	}
	for i := 0; i < 5; i++ {
		_, _ = dcb.QueryContext(ctx, "SELECT * FROM users")
	}

	// Verify circuit is open
	if !dcb.IsOpen() {
		t.Fatal("expected circuit to be open")
	}

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Setup successful query for half-open state
	rows := sqlmock.NewRows([]string{"id"}).AddRow(1)
	mock.ExpectQuery("SELECT (.+)").WillReturnRows(rows)

	// Execute query - should transition to half-open and succeed
	result, err := dcb.QueryContext(ctx, "SELECT * FROM users")
	if err != nil {
		t.Fatalf("expected query to succeed in half-open state, got %v", err)
	}
	_ = result.Close()

	// After successful requests in half-open state, circuit should close
	// Note: This may require multiple successful requests depending on MaxRequests
}

func TestDBCircuitBreaker_QueryRowContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer func() { _ = db.Close() }()

	dcb := NewDBCircuitBreaker(db)
	ctx := context.Background()

	// Setup mock expectation
	rows := sqlmock.NewRows([]string{"id", "name"}).
		AddRow(1, "test")
	mock.ExpectQuery("SELECT (.+) FROM users WHERE id = ?").
		WithArgs(1).
		WillReturnRows(rows)

	// Execute query
	row := dcb.QueryRowContext(ctx, "SELECT id, name FROM users WHERE id = ?", 1)

	// Scan result
	var id int
	var name string
	if err := row.Scan(&id, &name); err != nil {
		t.Fatalf("failed to scan row: %v", err)
	}

	if id != 1 || name != "test" {
		t.Errorf("expected id=1, name=test, got id=%d, name=%s", id, name)
	}

	// Verify all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestDBCircuitBreaker_DB(t *testing.T) {
	db, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create mock db: %v", err)
	}
	defer func() { _ = db.Close() }()

	dcb := NewDBCircuitBreaker(db)

	if dcb.DB() != db {
		t.Error("expected DB() to return underlying database connection")
	}
}

func TestDBConfig(t *testing.T) {
	cfg := DBConfig()

	if cfg.Name != "database" {
		t.Errorf("expected name 'database', got '%s'", cfg.Name)
	}

	if cfg.MaxRequests != 3 {
		t.Errorf("expected MaxRequests 3, got %d", cfg.MaxRequests)
	}

	if cfg.Timeout != 30*time.Second {
		t.Errorf("expected Timeout 30s, got %v", cfg.Timeout)
	}

	if cfg.MinRequests != 5 {
		t.Errorf("expected MinRequests 5, got %d", cfg.MinRequests)
	}

	if cfg.FailureThreshold != 1.0 {
		t.Errorf("expected FailureThreshold 1.0, got %f", cfg.FailureThreshold)
	}
}
