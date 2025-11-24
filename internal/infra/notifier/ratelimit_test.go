package notifier

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	t.Run("TC-1: should allow request within rate limit", func(t *testing.T) {
		// Arrange
		limiter := NewRateLimiter(10.0, 5) // 10 req/s, burst of 5
		ctx := context.Background()

		// Act
		err := limiter.Allow(ctx)

		// Assert
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("TC-2: should block request exceeding rate limit", func(t *testing.T) {
		// Arrange
		limiter := NewRateLimiter(1.0, 1) // 1 req/s, burst of 1
		ctx := context.Background()

		// Consume the single token
		err := limiter.Allow(ctx)
		if err != nil {
			t.Fatalf("first request should succeed: %v", err)
		}

		// Act - Second request should be delayed
		start := time.Now()
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		err = limiter.Allow(ctxWithTimeout)

		// Assert
		elapsed := time.Since(start)
		if err == nil {
			t.Errorf("expected timeout error, but request succeeded")
		}
		if elapsed < 90*time.Millisecond {
			t.Logf("warning: expected request to be blocked for ~100ms, but elapsed time was %v (timing may vary)", elapsed)
		}
		// Check if error is related to context (various error message formats)
		if err != nil && !isContextError(err) && err.Error() != "rate: Wait(n=1) would exceed context deadline" {
			t.Errorf("expected context-related error, got %v", err)
		}
	})

	t.Run("TC-3: should handle burst requests immediately", func(t *testing.T) {
		// Arrange
		limiter := NewRateLimiter(2.0, 5) // 2 req/s, burst of 5
		ctx := context.Background()

		// Act - First 5 requests should succeed immediately
		start := time.Now()
		for i := 0; i < 5; i++ {
			err := limiter.Allow(ctx)
			if err != nil {
				t.Fatalf("burst request %d should succeed: %v", i+1, err)
			}
		}
		elapsed := time.Since(start)

		// Assert - All 5 requests should complete very quickly (< 100ms)
		if elapsed > 100*time.Millisecond {
			t.Errorf("expected burst requests to complete quickly, but took %v", elapsed)
		}

		// Act - 6th request should be rate limited
		ctxWithTimeout, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		err := limiter.Allow(ctxWithTimeout)

		// Assert - 6th request should be blocked
		if err == nil {
			t.Errorf("expected 6th request to be rate limited")
		}
		// Check if error is related to context (various error message formats)
		if err != nil && !isContextError(err) && err.Error() != "rate: Wait(n=1) would exceed context deadline" {
			t.Errorf("expected context-related error, got %v", err)
		}
	})

	t.Run("TC-4: should respect context cancellation during rate limiting", func(t *testing.T) {
		// Arrange
		limiter := NewRateLimiter(1.0, 1) // 1 req/s, burst of 1
		ctx := context.Background()

		// Consume the single token
		err := limiter.Allow(ctx)
		if err != nil {
			t.Fatalf("first request should succeed: %v", err)
		}

		// Create a context that will be canceled
		ctxWithCancel, cancel := context.WithCancel(ctx)

		// Act - Start rate limited request in goroutine
		errChan := make(chan error, 1)
		go func() {
			errChan <- limiter.Allow(ctxWithCancel)
		}()

		// Cancel context after 50ms
		time.Sleep(50 * time.Millisecond)
		cancel()

		// Wait for request to complete
		err = <-errChan

		// Assert
		if err == nil {
			t.Errorf("expected cancellation error, but request succeeded")
		}
		if !isContextError(err) {
			t.Errorf("expected context canceled error, got %v", err)
		}
	})
}

func TestNewRateLimiter(t *testing.T) {
	t.Run("should create rate limiter with correct configuration", func(t *testing.T) {
		// Arrange
		requestsPerSecond := 2.0
		burst := 5

		// Act
		limiter := NewRateLimiter(requestsPerSecond, burst)

		// Assert
		if limiter == nil {
			t.Fatal("expected non-nil limiter")
		}
		if limiter.limiter == nil {
			t.Error("expected internal limiter to be initialized")
		}
		if limiter.burst != burst {
			t.Errorf("expected burst=%d, got %d", burst, limiter.burst)
		}
		if float64(limiter.rate) != requestsPerSecond {
			t.Errorf("expected rate=%f, got %f", requestsPerSecond, float64(limiter.rate))
		}
	})
}

// isContextError checks if the error is a context error (Canceled or DeadlineExceeded)
func isContextError(err error) bool {
	return err == context.Canceled || err == context.DeadlineExceeded
}
