// Package circuitbreaker provides circuit breaker implementations for database operations.
// This file implements a database-specific wrapper that protects database calls from cascading failures.
package circuitbreaker

import (
	"context"
	"database/sql"
	"time"

	"github.com/sony/gobreaker"
)

// DBCircuitBreaker wraps a database connection with circuit breaker protection.
// It prevents cascading failures when the database becomes unavailable or slow.
type DBCircuitBreaker struct {
	cb *CircuitBreaker
	db *sql.DB
}

// DBConfig returns configuration optimized for database circuit breakers.
// Opens after 5 consecutive failures, 30 second timeout.
func DBConfig() Config {
	return Config{
		Name:             "database",
		MaxRequests:      3, // Allow 3 test requests in half-open state
		Interval:         time.Minute,
		Timeout:          30 * time.Second,
		FailureThreshold: 1.0, // Open on 100% failure (5+ consecutive failures)
		MinRequests:      5,   // Require 5 failures before tripping
	}
}

// NewDBCircuitBreaker creates a new database circuit breaker.
// It wraps the provided database connection with circuit breaker protection.
func NewDBCircuitBreaker(db *sql.DB) *DBCircuitBreaker {
	return &DBCircuitBreaker{
		cb: New(DBConfig()),
		db: db,
	}
}

// NewDBCircuitBreakerWithConfig creates a new database circuit breaker with custom configuration.
func NewDBCircuitBreakerWithConfig(db *sql.DB, cfg Config) *DBCircuitBreaker {
	return &DBCircuitBreaker{
		cb: New(cfg),
		db: db,
	}
}

// QueryContext executes a query with circuit breaker protection.
// If the circuit is open, it returns ErrOpenState immediately without hitting the database.
func (dcb *DBCircuitBreaker) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	result, err := dcb.cb.Execute(func() (interface{}, error) {
		return dcb.db.QueryContext(ctx, query, args...)
	})

	if err != nil {
		return nil, err
	}

	return result.(*sql.Rows), nil
}

// ExecContext executes a statement with circuit breaker protection.
// If the circuit is open, it returns ErrOpenState immediately without hitting the database.
func (dcb *DBCircuitBreaker) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	result, err := dcb.cb.Execute(func() (interface{}, error) {
		return dcb.db.ExecContext(ctx, query, args...)
	})

	if err != nil {
		return nil, err
	}

	return result.(sql.Result), nil
}

// QueryRowContext executes a query that returns at most one row with circuit breaker protection.
// Note: sql.Row doesn't return an error immediately, so circuit breaker protection is limited.
// The error is only returned when scanning the row.
func (dcb *DBCircuitBreaker) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	// Note: We can't use circuit breaker effectively here because QueryRow doesn't return error
	// The error is deferred until Scan() is called
	return dcb.db.QueryRowContext(ctx, query, args...)
}

// State returns the current state of the circuit breaker.
func (dcb *DBCircuitBreaker) State() gobreaker.State {
	return dcb.cb.State()
}

// IsOpen returns true if the circuit breaker is in the open state.
func (dcb *DBCircuitBreaker) IsOpen() bool {
	return dcb.cb.IsOpen()
}

// DB returns the underlying database connection.
// This should only be used for operations that don't need circuit breaker protection.
func (dcb *DBCircuitBreaker) DB() *sql.DB {
	return dcb.db
}
