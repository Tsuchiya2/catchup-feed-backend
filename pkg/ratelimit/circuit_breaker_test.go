package ratelimit

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		name  string
		state CircuitState
		want  string
	}{
		{"closed state", StateClosed, "closed"},
		{"open state", StateOpen, "open"},
		{"half-open state", StateHalfOpen, "half-open"},
		{"unknown state", CircuitState(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewCircuitBreaker(t *testing.T) {
	tests := []struct {
		name                   string
		config                 CircuitBreakerConfig
		wantFailureThreshold   int
		wantRecoveryTimeout    time.Duration
		wantInitialState       CircuitState
	}{
		{
			name: "with valid config",
			config: CircuitBreakerConfig{
				FailureThreshold: 5,
				RecoveryTimeout:  10 * time.Second,
				Clock:            &SystemClock{},
				Metrics:          NewNoOpMetrics(),
				LimiterType:      "test",
			},
			wantFailureThreshold: 5,
			wantRecoveryTimeout:  10 * time.Second,
			wantInitialState:     StateClosed,
		},
		{
			name: "with zero failure threshold should use default",
			config: CircuitBreakerConfig{
				FailureThreshold: 0,
				RecoveryTimeout:  10 * time.Second,
			},
			wantFailureThreshold: 10,
			wantRecoveryTimeout:  10 * time.Second,
			wantInitialState:     StateClosed,
		},
		{
			name: "with zero recovery timeout should use default",
			config: CircuitBreakerConfig{
				FailureThreshold: 5,
				RecoveryTimeout:  0,
			},
			wantFailureThreshold: 5,
			wantRecoveryTimeout:  30 * time.Second,
			wantInitialState:     StateClosed,
		},
		{
			name: "with nil clock should use system clock",
			config: CircuitBreakerConfig{
				FailureThreshold: 5,
				RecoveryTimeout:  10 * time.Second,
				Clock:            nil,
			},
			wantFailureThreshold: 5,
			wantRecoveryTimeout:  10 * time.Second,
			wantInitialState:     StateClosed,
		},
		{
			name: "with nil metrics should use noop metrics",
			config: CircuitBreakerConfig{
				FailureThreshold: 5,
				RecoveryTimeout:  10 * time.Second,
				Metrics:          nil,
			},
			wantFailureThreshold: 5,
			wantRecoveryTimeout:  10 * time.Second,
			wantInitialState:     StateClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker(tt.config)
			if cb == nil {
				t.Fatal("NewCircuitBreaker() returned nil")
			}
			if cb.config.FailureThreshold != tt.wantFailureThreshold {
				t.Errorf("FailureThreshold = %v, want %v", cb.config.FailureThreshold, tt.wantFailureThreshold)
			}
			if cb.config.RecoveryTimeout != tt.wantRecoveryTimeout {
				t.Errorf("RecoveryTimeout = %v, want %v", cb.config.RecoveryTimeout, tt.wantRecoveryTimeout)
			}
			if cb.state != tt.wantInitialState {
				t.Errorf("Initial state = %v, want %v", cb.state, tt.wantInitialState)
			}
			if cb.config.Clock == nil {
				t.Error("Clock should not be nil")
			}
			if cb.config.Metrics == nil {
				t.Error("Metrics should not be nil")
			}
		})
	}
}

func TestCircuitBreaker_Execute_ClosedState(t *testing.T) {
	clock := NewMockClock(time.Now())
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  10 * time.Second,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	t.Run("successful operation", func(t *testing.T) {
		successOp := func() error {
			return nil
		}

		err := cb.Execute(successOp)
		if err != nil {
			t.Errorf("Execute() error = %v, want nil", err)
		}

		if cb.State() != StateClosed {
			t.Errorf("State = %v, want %v", cb.State(), StateClosed)
		}
	})

	t.Run("failed operation", func(t *testing.T) {
		failOp := func() error {
			return errors.New("operation failed")
		}

		err := cb.Execute(failOp)
		if err == nil {
			t.Error("Execute() should return error for failed operation")
		}

		if cb.State() != StateClosed {
			t.Errorf("State = %v, want %v (should stay closed)", cb.State(), StateClosed)
		}
	})
}

func TestCircuitBreaker_Execute_TransitionToOpen(t *testing.T) {
	clock := NewMockClock(time.Now())
	failureThreshold := 3

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: failureThreshold,
		RecoveryTimeout:  10 * time.Second,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	failOp := func() error {
		return errors.New("operation failed")
	}

	// Execute failures up to threshold
	for i := 0; i < failureThreshold; i++ {
		err := cb.Execute(failOp)
		if err == nil {
			t.Errorf("Execute() iteration %d should return error", i)
		}
	}

	// Circuit should now be open
	if !cb.IsOpen() {
		t.Errorf("Circuit should be open after %d failures", failureThreshold)
	}
}

func TestCircuitBreaker_Execute_OpenState(t *testing.T) {
	clock := NewMockClock(time.Now())
	failureThreshold := 3

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: failureThreshold,
		RecoveryTimeout:  10 * time.Second,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Trigger circuit to open
	failOp := func() error {
		return errors.New("operation failed")
	}

	for i := 0; i < failureThreshold; i++ {
		cb.Execute(failOp)
	}

	if !cb.IsOpen() {
		t.Fatal("Circuit should be open")
	}

	// In open state, operations should be allowed (fail-open behavior)
	err := cb.Execute(failOp)
	if err != nil {
		t.Errorf("Execute() in open state should return nil (fail-open), got %v", err)
	}

	successOp := func() error {
		return nil
	}

	err = cb.Execute(successOp)
	if err != nil {
		t.Errorf("Execute() in open state should return nil (fail-open), got %v", err)
	}
}

func TestCircuitBreaker_Execute_TransitionToHalfOpen(t *testing.T) {
	clock := NewMockClock(time.Now())
	failureThreshold := 3
	recoveryTimeout := 10 * time.Second

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: failureThreshold,
		RecoveryTimeout:  recoveryTimeout,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Trigger circuit to open
	failOp := func() error {
		return errors.New("operation failed")
	}

	for i := 0; i < failureThreshold; i++ {
		cb.Execute(failOp)
	}

	if !cb.IsOpen() {
		t.Fatal("Circuit should be open")
	}

	// Advance clock past recovery timeout
	clock.Advance(recoveryTimeout + 1*time.Second)

	// Next execute should transition to half-open
	successOp := func() error {
		return nil
	}

	err := cb.Execute(successOp)
	if err != nil {
		t.Errorf("Execute() error = %v, want nil", err)
	}

	// Circuit should be closed after successful half-open test
	if !cb.IsClosed() {
		t.Errorf("Circuit should be closed after successful recovery, got state %v", cb.State())
	}
}

func TestCircuitBreaker_Execute_HalfOpenSuccess(t *testing.T) {
	clock := NewMockClock(time.Now())
	failureThreshold := 3
	recoveryTimeout := 10 * time.Second

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: failureThreshold,
		RecoveryTimeout:  recoveryTimeout,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Open the circuit
	for i := 0; i < failureThreshold; i++ {
		cb.RecordFailure()
	}

	// Advance to recovery time
	clock.Advance(recoveryTimeout + 1*time.Second)

	// Successful operation in half-open should close circuit
	successOp := func() error {
		return nil
	}

	err := cb.Execute(successOp)
	if err != nil {
		t.Errorf("Execute() error = %v, want nil", err)
	}

	if !cb.IsClosed() {
		t.Error("Circuit should be closed after successful half-open test")
	}

	stats := cb.Stats()
	if stats.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %v, want 0 after recovery", stats.ConsecutiveFailures)
	}
}

func TestCircuitBreaker_Execute_HalfOpenFailure(t *testing.T) {
	clock := NewMockClock(time.Now())
	failureThreshold := 3
	recoveryTimeout := 10 * time.Second

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: failureThreshold,
		RecoveryTimeout:  recoveryTimeout,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Open the circuit
	for i := 0; i < failureThreshold; i++ {
		cb.RecordFailure()
	}

	// Advance to recovery time
	clock.Advance(recoveryTimeout + 1*time.Second)

	// Failed operation in half-open should reopen circuit
	failOp := func() error {
		return errors.New("operation failed")
	}

	err := cb.Execute(failOp)
	if err == nil {
		t.Error("Execute() should return error for failed operation")
	}

	if !cb.IsOpen() {
		t.Error("Circuit should be open again after failed half-open test")
	}
}

func TestCircuitBreaker_RecordSuccess(t *testing.T) {
	clock := NewMockClock(time.Now())
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  10 * time.Second,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Record some failures
	cb.RecordFailure()
	cb.RecordFailure()

	stats := cb.Stats()
	if stats.ConsecutiveFailures != 2 {
		t.Errorf("ConsecutiveFailures = %v, want 2", stats.ConsecutiveFailures)
	}

	// Record success should reset counter
	cb.RecordSuccess()

	stats = cb.Stats()
	if stats.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %v, want 0 after success", stats.ConsecutiveFailures)
	}
}

func TestCircuitBreaker_RecordFailure(t *testing.T) {
	clock := NewMockClock(time.Now())
	failureThreshold := 3

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: failureThreshold,
		RecoveryTimeout:  10 * time.Second,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Record failures
	for i := 1; i <= failureThreshold; i++ {
		cb.RecordFailure()

		stats := cb.Stats()
		if stats.ConsecutiveFailures != i {
			t.Errorf("ConsecutiveFailures = %v, want %v", stats.ConsecutiveFailures, i)
		}
	}

	// Circuit should be open
	if !cb.IsOpen() {
		t.Error("Circuit should be open after threshold failures")
	}
}

func TestCircuitBreaker_Allow(t *testing.T) {
	clock := NewMockClock(time.Now())
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  10 * time.Second,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Closed state - should allow
	if !cb.Allow() {
		t.Error("Allow() should return true in closed state")
	}

	// Open circuit
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	// Open state - should allow (fail-open behavior)
	if !cb.Allow() {
		t.Error("Allow() should return true in open state (fail-open)")
	}

	// Half-open state - should allow
	clock.Advance(11 * time.Second)
	if !cb.Allow() {
		t.Error("Allow() should return true in half-open state")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	clock := NewMockClock(time.Now())
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  10 * time.Second,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Open the circuit
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if !cb.IsOpen() {
		t.Fatal("Circuit should be open")
	}

	// Reset
	cb.Reset()

	// Should be closed
	if !cb.IsClosed() {
		t.Error("Circuit should be closed after Reset()")
	}

	stats := cb.Stats()
	if stats.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %v, want 0 after Reset()", stats.ConsecutiveFailures)
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	now := time.Now()
	clock := NewMockClock(now)

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  10 * time.Second,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Initial stats
	stats := cb.Stats()
	if stats.State != StateClosed {
		t.Errorf("Initial state = %v, want %v", stats.State, StateClosed)
	}
	if stats.ConsecutiveFailures != 0 {
		t.Errorf("Initial failures = %v, want 0", stats.ConsecutiveFailures)
	}

	// Record failure
	cb.RecordFailure()
	stats = cb.Stats()

	if stats.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %v, want 1", stats.ConsecutiveFailures)
	}
	if stats.LastFailureTime.IsZero() {
		t.Error("LastFailureTime should be set")
	}

	// Open circuit
	cb.RecordFailure()
	cb.RecordFailure()

	stats = cb.Stats()
	if stats.State != StateOpen {
		t.Errorf("State = %v, want %v", stats.State, StateOpen)
	}

	// Check TimeSinceLastChange
	clock.Advance(5 * time.Second)
	stats = cb.Stats()

	if stats.TimeSinceLastChange < 5*time.Second {
		t.Errorf("TimeSinceLastChange = %v, want >= 5s", stats.TimeSinceLastChange)
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	clock := NewMockClock(time.Now())
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 100,
		RecoveryTimeout:  10 * time.Second,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	var wg sync.WaitGroup
	numGoroutines := 10
	operationsPerGoroutine := 100

	// Concurrent operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				op := func() error {
					if j%2 == 0 {
						return nil
					}
					return errors.New("error")
				}

				cb.Execute(op)
			}
		}(i)
	}

	wg.Wait()

	// Should not panic or deadlock
	stats := cb.Stats()
	if stats.State == StateOpen && stats.ConsecutiveFailures < cb.config.FailureThreshold {
		t.Errorf("State is open but failures (%d) < threshold (%d)",
			stats.ConsecutiveFailures, cb.config.FailureThreshold)
	}
}

func TestCircuitBreaker_StateChecks(t *testing.T) {
	clock := NewMockClock(time.Now())
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  10 * time.Second,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Closed state
	if !cb.IsClosed() {
		t.Error("IsClosed() should return true initially")
	}
	if cb.IsOpen() {
		t.Error("IsOpen() should return false initially")
	}
	if cb.IsHalfOpen() {
		t.Error("IsHalfOpen() should return false initially")
	}

	// Open circuit
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	// Open state
	if cb.IsClosed() {
		t.Error("IsClosed() should return false when open")
	}
	if !cb.IsOpen() {
		t.Error("IsOpen() should return true when open")
	}
	if cb.IsHalfOpen() {
		t.Error("IsHalfOpen() should return false when open")
	}

	// Advance to recovery time (triggers half-open on next operation)
	clock.Advance(11 * time.Second)

	// Trigger half-open check
	cb.Allow()

	// Half-open state
	if cb.IsClosed() {
		t.Error("IsClosed() should return false when half-open")
	}
	if cb.IsOpen() {
		t.Error("IsOpen() should return false when half-open")
	}
	if !cb.IsHalfOpen() {
		t.Error("IsHalfOpen() should return true when half-open")
	}
}

func TestCircuitBreaker_AttemptRecovery(t *testing.T) {
	clock := NewMockClock(time.Now())
	recoveryTimeout := 10 * time.Second

	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  recoveryTimeout,
		Clock:            clock,
		Metrics:          NewNoOpMetrics(),
		LimiterType:      "test",
	})

	// Open the circuit
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	// Should be open
	if !cb.IsOpen() {
		t.Fatal("Circuit should be open")
	}

	// Advance clock but not enough
	clock.Advance(5 * time.Second)
	cb.Allow() // Triggers attemptRecovery

	// Should still be open
	if !cb.IsOpen() {
		t.Error("Circuit should still be open")
	}

	// Advance past recovery timeout
	clock.Advance(6 * time.Second)
	cb.Allow() // Triggers attemptRecovery

	// Should transition to half-open
	if !cb.IsHalfOpen() {
		t.Errorf("Circuit should be half-open, got %v", cb.State())
	}
}
