package ratelimit

import (
	"log/slog"
	"sync"
	"time"
)

// CircuitState represents the current state of the circuit breaker.
type CircuitState int

const (
	// StateClosed indicates the circuit is closed and requests are allowed.
	// This is the normal operating state.
	StateClosed CircuitState = iota

	// StateOpen indicates the circuit is open due to excessive failures.
	// When open, the circuit breaker allows all requests through (fail-open behavior)
	// for availability, but logs critical alerts.
	StateOpen

	// StateHalfOpen indicates the circuit is testing recovery.
	// The first request is allowed through to test if the underlying system has recovered.
	StateHalfOpen
)

// String returns a string representation of the circuit state.
func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig holds configuration for the circuit breaker.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of consecutive failures required to open the circuit.
	// Default: 10
	FailureThreshold int

	// RecoveryTimeout is the duration to wait before attempting recovery (half-open state).
	// Default: 30 seconds
	RecoveryTimeout time.Duration

	// Clock provides time abstraction for testing.
	// Default: SystemClock
	Clock Clock

	// Metrics for recording circuit state changes.
	// Default: NoOpMetrics
	Metrics RateLimitMetrics

	// LimiterType identifies which rate limiter this circuit breaker protects.
	// Examples: "ip", "user"
	LimiterType string
}

// CircuitBreaker implements the circuit breaker pattern for rate limiting operations.
//
// The circuit breaker protects against cascading failures when the rate limiter
// experiences errors. It has three states:
//
// - Closed (normal): All operations are executed normally
// - Open (failing): After N consecutive failures, circuit opens and allows all requests
//   (fail-open behavior for availability)
// - Half-Open (testing): After recovery timeout, allows one test request to check recovery
//
// Security Trade-off:
// This circuit breaker uses fail-open behavior, meaning when the rate limiter fails,
// requests are allowed through. This prioritizes availability over strict rate limiting.
// This is acceptable for DoS protection but may not be suitable for all security contexts.
type CircuitBreaker struct {
	config CircuitBreakerConfig

	mu               sync.RWMutex
	state            CircuitState
	consecutiveFailures int
	lastFailureTime  time.Time
	lastStateChange  time.Time
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration.
//
// If config.FailureThreshold is 0, it defaults to 10.
// If config.RecoveryTimeout is 0, it defaults to 30 seconds.
// If config.Clock is nil, it defaults to SystemClock.
// If config.Metrics is nil, it defaults to NoOpMetrics.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	if config.FailureThreshold <= 0 {
		config.FailureThreshold = 10
	}
	if config.RecoveryTimeout <= 0 {
		config.RecoveryTimeout = 30 * time.Second
	}
	if config.Clock == nil {
		config.Clock = &SystemClock{}
	}
	if config.Metrics == nil {
		config.Metrics = &NoOpMetrics{}
	}

	cb := &CircuitBreaker{
		config:          config,
		state:           StateClosed,
		lastStateChange: config.Clock.Now(),
	}

	// Record initial state
	config.Metrics.RecordCircuitState(config.LimiterType, cb.state.String())

	return cb
}

// Execute runs the given operation with circuit breaker protection.
//
// Behavior by state:
// - Closed: Execute operation normally, track failures
// - Open: Allow operation (fail-open), log alert
// - Half-Open: Execute operation, close on success or reopen on failure
//
// Returns:
// - nil if operation succeeded or circuit is open (fail-open)
// - error if operation failed
func (cb *CircuitBreaker) Execute(operation func() error) error {
	// Check if we should attempt recovery
	cb.attemptRecovery()

	cb.mu.RLock()
	currentState := cb.state
	cb.mu.RUnlock()

	switch currentState {
	case StateClosed:
		return cb.executeInClosedState(operation)

	case StateOpen:
		// Fail-open: Allow the request through but don't execute the operation
		// This prioritizes availability over strict rate limiting
		return nil

	case StateHalfOpen:
		return cb.executeInHalfOpenState(operation)

	default:
		return cb.executeInClosedState(operation)
	}
}

// executeInClosedState executes the operation in closed state.
func (cb *CircuitBreaker) executeInClosedState(operation func() error) error {
	err := operation()
	if err != nil {
		cb.RecordFailure()
		return err
	}

	cb.RecordSuccess()
	return nil
}

// executeInHalfOpenState executes the operation in half-open state.
func (cb *CircuitBreaker) executeInHalfOpenState(operation func() error) error {
	err := operation()
	if err != nil {
		// Test failed, reopen the circuit
		cb.mu.Lock()
		oldState := cb.state
		cb.state = StateOpen
		cb.consecutiveFailures++
		cb.lastFailureTime = cb.config.Clock.Now()
		cb.lastStateChange = cb.config.Clock.Now()
		cb.mu.Unlock()

		cb.config.Metrics.RecordCircuitState(cb.config.LimiterType, StateOpen.String())

		// Log circuit breaker state change at WARN level
		slog.Warn("circuit breaker state changed",
			slog.String("limiter_type", cb.config.LimiterType),
			slog.String("previous_state", oldState.String()),
			slog.String("new_state", StateOpen.String()),
			slog.Int("consecutive_failures", cb.consecutiveFailures),
			slog.Duration("recovery_timeout", cb.config.RecoveryTimeout),
		)
		return err
	}

	// Test succeeded, close the circuit
	cb.mu.Lock()
	oldState := cb.state
	cb.state = StateClosed
	cb.consecutiveFailures = 0
	cb.lastStateChange = cb.config.Clock.Now()
	cb.mu.Unlock()

	cb.config.Metrics.RecordCircuitState(cb.config.LimiterType, StateClosed.String())

	// Log circuit breaker state change at WARN level
	slog.Warn("circuit breaker state changed",
		slog.String("limiter_type", cb.config.LimiterType),
		slog.String("previous_state", oldState.String()),
		slog.String("new_state", StateClosed.String()),
		slog.Int("consecutive_failures", 0),
	)
	return nil
}

// Allow returns true if the operation should be allowed.
//
// When the circuit is open, this returns true (fail-open behavior).
// This is a convenience method for checking if requests should be allowed.
func (cb *CircuitBreaker) Allow() bool {
	cb.attemptRecovery()

	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// In all states, we allow the request
	// - Closed: Normal operation
	// - Open: Fail-open for availability
	// - Half-Open: Testing recovery
	return true
}

// RecordSuccess records a successful operation.
//
// This resets the consecutive failure count.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.consecutiveFailures > 0 {
		cb.consecutiveFailures = 0
	}
}

// RecordFailure records a failed operation.
//
// If consecutive failures reach the threshold, the circuit opens.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	oldState := cb.state
	cb.consecutiveFailures++
	cb.lastFailureTime = cb.config.Clock.Now()

	if cb.consecutiveFailures >= cb.config.FailureThreshold && cb.state == StateClosed {
		cb.state = StateOpen
		cb.lastStateChange = cb.config.Clock.Now()
		cb.config.Metrics.RecordCircuitState(cb.config.LimiterType, StateOpen.String())

		// Log circuit breaker state change at WARN level
		slog.Warn("circuit breaker state changed",
			slog.String("limiter_type", cb.config.LimiterType),
			slog.String("previous_state", oldState.String()),
			slog.String("new_state", StateOpen.String()),
			slog.Int("consecutive_failures", cb.consecutiveFailures),
			slog.Duration("recovery_timeout", cb.config.RecoveryTimeout),
		)
	}
}

// State returns the current circuit state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// IsOpen returns true if the circuit is currently open.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state == StateOpen
}

// IsClosed returns true if the circuit is currently closed.
func (cb *CircuitBreaker) IsClosed() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state == StateClosed
}

// IsHalfOpen returns true if the circuit is currently half-open.
func (cb *CircuitBreaker) IsHalfOpen() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state == StateHalfOpen
}

// Reset resets the circuit breaker to the closed state.
//
// This is useful for testing or manual intervention.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.consecutiveFailures = 0
	cb.lastFailureTime = time.Time{}
	cb.lastStateChange = cb.config.Clock.Now()

	cb.config.Metrics.RecordCircuitState(cb.config.LimiterType, StateClosed.String())
}

// attemptRecovery checks if the circuit should transition from open to half-open.
//
// This is called before each operation to see if enough time has passed
// to attempt recovery.
func (cb *CircuitBreaker) attemptRecovery() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state != StateOpen {
		return
	}

	now := cb.config.Clock.Now()
	if now.Sub(cb.lastStateChange) >= cb.config.RecoveryTimeout {
		cb.state = StateHalfOpen
		cb.lastStateChange = now
		cb.config.Metrics.RecordCircuitState(cb.config.LimiterType, StateHalfOpen.String())
	}
}

// Stats returns statistics about the circuit breaker.
type CircuitBreakerStats struct {
	State               CircuitState
	ConsecutiveFailures int
	LastFailureTime     time.Time
	LastStateChange     time.Time
	TimeSinceLastChange time.Duration
}

// Stats returns current circuit breaker statistics.
//
// This is useful for monitoring and debugging.
func (cb *CircuitBreaker) Stats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	now := cb.config.Clock.Now()
	return CircuitBreakerStats{
		State:               cb.state,
		ConsecutiveFailures: cb.consecutiveFailures,
		LastFailureTime:     cb.lastFailureTime,
		LastStateChange:     cb.lastStateChange,
		TimeSinceLastChange: now.Sub(cb.lastStateChange),
	}
}
