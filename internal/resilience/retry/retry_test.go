package retry

import (
	"context"
	"errors"
	"syscall"
	"testing"
	"time"
)

func TestWithBackoff_Success(t *testing.T) {
	cfg := Config{
		MaxAttempts:    3,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}

	attempts := 0
	fn := func() error {
		attempts++
		return nil // Success on first attempt
	}

	err := WithBackoff(context.Background(), cfg, fn)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", attempts)
	}
}

func TestWithBackoff_SuccessAfterRetry(t *testing.T) {
	cfg := Config{
		MaxAttempts:    3,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}

	attempts := 0
	fn := func() error {
		attempts++
		if attempts < 3 {
			return &HTTPError{StatusCode: 500, Message: "Server Error"}
		}
		return nil // Success on 3rd attempt
	}

	err := WithBackoff(context.Background(), cfg, fn)

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestWithBackoff_MaxAttemptsExceeded(t *testing.T) {
	cfg := Config{
		MaxAttempts:    3,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}

	attempts := 0
	testErr := &HTTPError{StatusCode: 500, Message: "Server Error"}
	fn := func() error {
		attempts++
		return testErr // Always fail
	}

	err := WithBackoff(context.Background(), cfg, fn)

	if err == nil {
		t.Error("expected error, got nil")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
	if !errors.Is(err, testErr) {
		t.Errorf("expected wrapped error to contain original error")
	}
}

func TestWithBackoff_NonRetryableError(t *testing.T) {
	cfg := Config{
		MaxAttempts:    3,
		InitialDelay:   10 * time.Millisecond,
		MaxDelay:       100 * time.Millisecond,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}

	attempts := 0
	testErr := &HTTPError{StatusCode: 400, Message: "Bad Request"}
	fn := func() error {
		attempts++
		return testErr // Non-retryable error
	}

	err := WithBackoff(context.Background(), cfg, fn)

	if err == nil {
		t.Error("expected error, got nil")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (non-retryable), got %d", attempts)
	}
	if err != testErr {
		t.Errorf("expected same error, got different error")
	}
}

func TestWithBackoff_ContextCanceled(t *testing.T) {
	cfg := Config{
		MaxAttempts:    5,
		InitialDelay:   50 * time.Millisecond,
		MaxDelay:       200 * time.Millisecond,
		Multiplier:     2.0,
		JitterFraction: 0.1,
	}

	ctx, cancel := context.WithCancel(context.Background())

	attempts := 0
	fn := func() error {
		attempts++
		if attempts == 2 {
			cancel() // Cancel context after 2nd attempt
		}
		return &HTTPError{StatusCode: 500, Message: "Server Error"}
	}

	err := WithBackoff(ctx, cfg, fn)

	if err == nil {
		t.Error("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
	// Should have attempted at least 2 times before cancel
	if attempts < 2 {
		t.Errorf("expected at least 2 attempts, got %d", attempts)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "nil error",
			err:       nil,
			retryable: false,
		},
		{
			name:      "context canceled",
			err:       context.Canceled,
			retryable: false,
		},
		{
			name:      "context deadline exceeded",
			err:       context.DeadlineExceeded,
			retryable: false,
		},
		{
			name:      "HTTP 500 error",
			err:       &HTTPError{StatusCode: 500, Message: "Internal Server Error"},
			retryable: true,
		},
		{
			name:      "HTTP 502 error",
			err:       &HTTPError{StatusCode: 502, Message: "Bad Gateway"},
			retryable: true,
		},
		{
			name:      "HTTP 503 error",
			err:       &HTTPError{StatusCode: 503, Message: "Service Unavailable"},
			retryable: true,
		},
		{
			name:      "HTTP 429 error",
			err:       &HTTPError{StatusCode: 429, Message: "Too Many Requests"},
			retryable: true,
		},
		{
			name:      "HTTP 408 error",
			err:       &HTTPError{StatusCode: 408, Message: "Request Timeout"},
			retryable: true,
		},
		{
			name:      "HTTP 400 error",
			err:       &HTTPError{StatusCode: 400, Message: "Bad Request"},
			retryable: false,
		},
		{
			name:      "HTTP 404 error",
			err:       &HTTPError{StatusCode: 404, Message: "Not Found"},
			retryable: false,
		},
		{
			name:      "ECONNREFUSED",
			err:       syscall.ECONNREFUSED,
			retryable: true,
		},
		{
			name:      "ECONNRESET",
			err:       syscall.ECONNRESET,
			retryable: true,
		},
		{
			name:      "ETIMEDOUT",
			err:       syscall.ETIMEDOUT,
			retryable: true,
		},
		{
			name:      "ENETUNREACH",
			err:       syscall.ENETUNREACH,
			retryable: true,
		},
		{
			name:      "generic error",
			err:       errors.New("some error"),
			retryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsRetryable(tt.err)
			if result != tt.retryable {
				t.Errorf("IsRetryable() = %v, want %v", result, tt.retryable)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 1*time.Second {
		t.Errorf("expected InitialDelay=1s, got %v", cfg.InitialDelay)
	}
	if cfg.MaxDelay != 30*time.Second {
		t.Errorf("expected MaxDelay=30s, got %v", cfg.MaxDelay)
	}
	if cfg.Multiplier != 2.0 {
		t.Errorf("expected Multiplier=2.0, got %f", cfg.Multiplier)
	}
	if cfg.JitterFraction != 0.1 {
		t.Errorf("expected JitterFraction=0.1, got %f", cfg.JitterFraction)
	}
}

func TestFeedFetchConfig(t *testing.T) {
	cfg := FeedFetchConfig()

	if cfg.MaxAttempts != 5 {
		t.Errorf("expected MaxAttempts=5, got %d", cfg.MaxAttempts)
	}
}

func TestAIAPIConfig(t *testing.T) {
	cfg := AIAPIConfig()

	if cfg.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 2*time.Second {
		t.Errorf("expected InitialDelay=2s, got %v", cfg.InitialDelay)
	}
}

func TestDBConfig(t *testing.T) {
	cfg := DBConfig()

	if cfg.MaxAttempts != 3 {
		t.Errorf("expected MaxAttempts=3, got %d", cfg.MaxAttempts)
	}
	if cfg.InitialDelay != 100*time.Millisecond {
		t.Errorf("expected InitialDelay=100ms, got %v", cfg.InitialDelay)
	}
}

func TestHTTPError_Error(t *testing.T) {
	err := &HTTPError{StatusCode: 500, Message: "Internal Server Error"}
	expected := "HTTP 500: Internal Server Error"

	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestAddJitter(t *testing.T) {
	duration := 100 * time.Millisecond
	jitterFraction := 0.2

	// Run multiple times to check jitter is random
	results := make(map[time.Duration]bool)
	for i := 0; i < 10; i++ {
		result := addJitter(duration, jitterFraction)

		// Result should be between duration and duration*(1+jitterFraction)
		minDuration := duration
		maxDuration := time.Duration(float64(duration) * 1.2)

		if result < minDuration || result > maxDuration {
			t.Errorf("expected result between %v and %v, got %v", minDuration, maxDuration, result)
		}

		results[result] = true
	}

	// Should have some variation (not all the same)
	if len(results) < 2 {
		t.Error("expected jitter to produce varied results")
	}
}

func TestAddJitter_ZeroFraction(t *testing.T) {
	duration := 100 * time.Millisecond
	result := addJitter(duration, 0.0)

	if result != duration {
		t.Errorf("expected no jitter with fraction=0, got %v instead of %v", result, duration)
	}
}
