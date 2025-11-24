package circuitbreaker

import (
	"errors"
	"testing"
	"time"

	"github.com/sony/gobreaker"
)

func TestNew(t *testing.T) {
	cfg := Config{
		Name:             "test-circuit",
		MaxRequests:      3,
		Interval:         10 * time.Second,
		Timeout:          20 * time.Second,
		FailureThreshold: 0.6,
		MinRequests:      5,
	}

	cb := New(cfg)

	if cb == nil {
		t.Fatal("expected circuit breaker, got nil")
	}
	if cb.Name() != "test-circuit" {
		t.Errorf("expected name='test-circuit', got %q", cb.Name())
	}
	if cb.State() != gobreaker.StateClosed {
		t.Errorf("expected initial state=Closed, got %v", cb.State())
	}
}

func TestCircuitBreaker_Execute_Success(t *testing.T) {
	cfg := Config{
		Name:             "test-circuit",
		MaxRequests:      3,
		Interval:         10 * time.Second,
		Timeout:          20 * time.Second,
		FailureThreshold: 0.6,
		MinRequests:      5,
	}

	cb := New(cfg)

	result, err := cb.Execute(func() (interface{}, error) {
		return "success", nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if result != "success" {
		t.Errorf("expected result='success', got %v", result)
	}
	if cb.State() != gobreaker.StateClosed {
		t.Errorf("expected state=Closed after success, got %v", cb.State())
	}
}

func TestCircuitBreaker_Execute_Failure(t *testing.T) {
	cfg := Config{
		Name:             "test-circuit",
		MaxRequests:      3,
		Interval:         10 * time.Second,
		Timeout:          20 * time.Second,
		FailureThreshold: 0.6,
		MinRequests:      5,
	}

	cb := New(cfg)

	testErr := errors.New("test error")
	result, err := cb.Execute(func() (interface{}, error) {
		return nil, testErr
	})

	if err != testErr {
		t.Errorf("expected error=%v, got %v", testErr, err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestCircuitBreaker_TripsOpen(t *testing.T) {
	cfg := Config{
		Name:             "test-circuit",
		MaxRequests:      3,
		Interval:         10 * time.Second,
		Timeout:          1 * time.Second, // Short timeout for testing
		FailureThreshold: 0.6,             // 60% failure rate
		MinRequests:      5,               // Minimum 5 requests
	}

	cb := New(cfg)

	// Verify initial state
	if cb.State() != gobreaker.StateClosed {
		t.Fatalf("expected initial state=Closed, got %v", cb.State())
	}

	// Execute 5 requests: 4 failures + 1 success = 80% failure rate
	testErr := errors.New("test error")

	for i := 0; i < 4; i++ {
		_, err := cb.Execute(func() (interface{}, error) {
			return nil, testErr
		})
		if err != testErr {
			t.Errorf("request %d: expected test error, got %v", i, err)
		}
	}

	// One success
	_, err := cb.Execute(func() (interface{}, error) {
		return "success", nil
	})
	if err != nil {
		t.Errorf("success request failed: %v", err)
	}

	// Circuit should still be closed (80% failure rate >= 60% threshold)
	// But we need one more failure to trip it
	_, err = cb.Execute(func() (interface{}, error) {
		return nil, testErr
	})
	if err != testErr {
		t.Errorf("expected test error, got %v", err)
	}

	// Now circuit should be open
	if cb.State() != gobreaker.StateOpen {
		t.Errorf("expected state=Open after exceeding failure threshold, got %v", cb.State())
	}
	if !cb.IsOpen() {
		t.Error("expected IsOpen()=true")
	}

	// Next request should fail immediately with ErrOpenState
	_, err = cb.Execute(func() (interface{}, error) {
		t.Error("function should not be called when circuit is open")
		return nil, nil
	})

	if err == nil {
		t.Error("expected error when circuit is open, got nil")
	}
	if !errors.Is(err, gobreaker.ErrOpenState) {
		t.Errorf("expected ErrOpenState, got %v", err)
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cfg := Config{
		Name:             "test-circuit",
		MaxRequests:      2, // Allow 2 requests in half-open state
		Interval:         10 * time.Second,
		Timeout:          100 * time.Millisecond, // Very short timeout for testing
		FailureThreshold: 0.6,
		MinRequests:      5,
	}

	cb := New(cfg)

	// Trip the circuit open
	testErr := errors.New("test error")
	for i := 0; i < 6; i++ {
		_, _ = cb.Execute(func() (interface{}, error) {
			return nil, testErr
		})
	}

	if cb.State() != gobreaker.StateOpen {
		t.Fatalf("circuit should be open, got %v", cb.State())
	}

	// Wait for timeout to transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Next request should trigger half-open state
	_, err := cb.Execute(func() (interface{}, error) {
		return "success", nil
	})

	if err != nil {
		t.Errorf("expected success in half-open state, got %v", err)
	}

	// State should transition back to closed after success
	if cb.State() == gobreaker.StateOpen {
		t.Errorf("circuit should not be open after successful half-open request, got %v", cb.State())
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("test")

	if cfg.Name != "test" {
		t.Errorf("expected Name='test', got %q", cfg.Name)
	}
	if cfg.MaxRequests != 3 {
		t.Errorf("expected MaxRequests=3, got %d", cfg.MaxRequests)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("expected Interval=30s, got %v", cfg.Interval)
	}
	if cfg.Timeout != 60*time.Second {
		t.Errorf("expected Timeout=60s, got %v", cfg.Timeout)
	}
	if cfg.FailureThreshold != 0.6 {
		t.Errorf("expected FailureThreshold=0.6, got %f", cfg.FailureThreshold)
	}
	if cfg.MinRequests != 5 {
		t.Errorf("expected MinRequests=5, got %d", cfg.MinRequests)
	}
}

func TestClaudeAPIConfig(t *testing.T) {
	cfg := ClaudeAPIConfig()

	if cfg.Name != "claude-api" {
		t.Errorf("expected Name='claude-api', got %q", cfg.Name)
	}
	if cfg.MaxRequests != 3 {
		t.Errorf("expected MaxRequests=3, got %d", cfg.MaxRequests)
	}
}

func TestOpenAIAPIConfig(t *testing.T) {
	cfg := OpenAIAPIConfig()

	if cfg.Name != "openai-api" {
		t.Errorf("expected Name='openai-api', got %q", cfg.Name)
	}
}

func TestFeedFetchConfig(t *testing.T) {
	cfg := FeedFetchConfig()

	if cfg.Name != "feed-fetch" {
		t.Errorf("expected Name='feed-fetch', got %q", cfg.Name)
	}
	if cfg.MaxRequests != 5 {
		t.Errorf("expected MaxRequests=5, got %d", cfg.MaxRequests)
	}
	if cfg.FailureThreshold != 0.7 {
		t.Errorf("expected FailureThreshold=0.7, got %f", cfg.FailureThreshold)
	}
}

func TestCircuitBreaker_MinRequests(t *testing.T) {
	cfg := Config{
		Name:             "test-circuit",
		MaxRequests:      3,
		Interval:         10 * time.Second,
		Timeout:          1 * time.Second,
		FailureThreshold: 0.5, // 50% failure rate
		MinRequests:      10,  // Need at least 10 requests
	}

	cb := New(cfg)

	// Execute only 4 failures (less than MinRequests)
	testErr := errors.New("test error")
	for i := 0; i < 4; i++ {
		_, err := cb.Execute(func() (interface{}, error) {
			return nil, testErr
		})
		if err != testErr {
			t.Errorf("request %d: expected test error, got %v", i, err)
		}
	}

	// Circuit should still be closed (not enough requests)
	if cb.State() != gobreaker.StateClosed {
		t.Errorf("expected state=Closed (below MinRequests), got %v", cb.State())
	}
}
