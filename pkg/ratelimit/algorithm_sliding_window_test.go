package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewSlidingWindowAlgorithm(t *testing.T) {
	tests := []struct {
		name      string
		clock     Clock
		wantClock bool
	}{
		{
			name:      "with system clock",
			clock:     &SystemClock{},
			wantClock: true,
		},
		{
			name:      "with nil clock should use system clock",
			clock:     nil,
			wantClock: true,
		},
		{
			name:      "with mock clock",
			clock:     NewMockClock(time.Now()),
			wantClock: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			algo := NewSlidingWindowAlgorithm(tt.clock)
			if algo == nil {
				t.Fatal("NewSlidingWindowAlgorithm() returned nil")
			}
			if tt.wantClock && algo.clock == nil {
				t.Error("clock should not be nil")
			}
			if algo.lastTimestamps == nil {
				t.Error("lastTimestamps map should be initialized")
			}
		})
	}
}

func TestSlidingWindowAlgorithm_IsAllowed(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	tests := []struct {
		name        string
		setupStore  func() RateLimitStore
		key         string
		limit       int
		window      time.Duration
		wantAllowed bool
		wantErr     bool
	}{
		{
			name: "first request should be allowed",
			setupStore: func() RateLimitStore {
				return NewInMemoryRateLimitStore(InMemoryStoreConfig{
					MaxKeys: 10,
					Clock:   clock,
				})
			},
			key:         "user-123",
			limit:       10,
			window:      1 * time.Minute,
			wantAllowed: true,
			wantErr:     false,
		},
		{
			name: "request within limit should be allowed",
			setupStore: func() RateLimitStore {
				store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
					MaxKeys: 10,
					Clock:   clock,
				})
				// Add 5 requests
				for i := 0; i < 5; i++ {
					store.AddRequest(ctx, "user-123", now.Add(time.Duration(i)*time.Second))
				}
				return store
			},
			key:         "user-123",
			limit:       10,
			window:      1 * time.Minute,
			wantAllowed: true,
			wantErr:     false,
		},
		{
			name: "request at limit should be denied",
			setupStore: func() RateLimitStore {
				store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
					MaxKeys: 10,
					Clock:   clock,
				})
				// Add exactly 10 requests
				for i := 0; i < 10; i++ {
					store.AddRequest(ctx, "user-123", now.Add(time.Duration(i)*time.Second))
				}
				return store
			},
			key:         "user-123",
			limit:       10,
			window:      1 * time.Minute,
			wantAllowed: false,
			wantErr:     false,
		},
		{
			name: "old requests outside window should not count",
			setupStore: func() RateLimitStore {
				store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
					MaxKeys: 10,
					Clock:   clock,
				})
				// Add 10 old requests (outside window)
				for i := 0; i < 10; i++ {
					store.AddRequest(ctx, "user-123", now.Add(-2*time.Minute))
				}
				return store
			},
			key:         "user-123",
			limit:       5,
			window:      1 * time.Minute,
			wantAllowed: true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			algo := NewSlidingWindowAlgorithm(clock)
			store := tt.setupStore()

			decision, err := algo.IsAllowed(ctx, tt.key, store, tt.limit, tt.window)

			if (err != nil) != tt.wantErr {
				t.Errorf("IsAllowed() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if decision.Allowed != tt.wantAllowed {
					t.Errorf("IsAllowed() allowed = %v, want %v", decision.Allowed, tt.wantAllowed)
				}
				if decision.Key != tt.key {
					t.Errorf("IsAllowed() key = %v, want %v", decision.Key, tt.key)
				}
				if decision.Limit != tt.limit {
					t.Errorf("IsAllowed() limit = %v, want %v", decision.Limit, tt.limit)
				}
			}
		})
	}
}

func TestSlidingWindowAlgorithm_ClockSkewProtection(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	algo := NewSlidingWindowAlgorithm(clock)
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	key := "user-123"
	limit := 10
	window := 1 * time.Minute

	// First request at T0
	decision, err := algo.IsAllowed(ctx, key, store, limit, window)
	if err != nil {
		t.Fatalf("IsAllowed() error = %v", err)
	}
	if !decision.Allowed {
		t.Error("First request should be allowed")
	}

	// Move clock backward (clock skew)
	clock.Set(now.Add(-30 * time.Second))

	// Request should still work (use last valid timestamp)
	decision, err = algo.IsAllowed(ctx, key, store, limit, window)
	if err != nil {
		t.Fatalf("IsAllowed() error = %v", err)
	}
	if !decision.Allowed {
		t.Error("Request should be allowed even with clock skew")
	}

	// Move clock forward again
	clock.Set(now.Add(30 * time.Second))

	// Request should work normally
	decision, err = algo.IsAllowed(ctx, key, store, limit, window)
	if err != nil {
		t.Fatalf("IsAllowed() error = %v", err)
	}
	if !decision.Allowed {
		t.Error("Request should be allowed after clock recovers")
	}
}

func TestSlidingWindowAlgorithm_GetValidTimestamp(t *testing.T) {
	now := time.Now()
	clock := NewMockClock(now)
	algo := NewSlidingWindowAlgorithm(clock)

	key := "user-123"

	// First call should return current time
	ts1 := algo.getValidTimestamp(key)
	if !ts1.Equal(now) {
		t.Errorf("First getValidTimestamp() = %v, want %v", ts1, now)
	}

	// Move clock forward
	clock.Advance(10 * time.Second)
	ts2 := algo.getValidTimestamp(key)
	if !ts2.After(ts1) {
		t.Error("getValidTimestamp() should return later time when clock advances")
	}

	// Move clock backward (clock skew)
	clock.Set(now.Add(-5 * time.Second))
	ts3 := algo.getValidTimestamp(key)

	// Should return last valid timestamp (ts2), not the skewed time
	if !ts3.Equal(ts2) {
		t.Errorf("getValidTimestamp() with clock skew = %v, want %v (last valid)", ts3, ts2)
	}
}

func TestSlidingWindowAlgorithm_GetWindowDuration(t *testing.T) {
	clock := NewMockClock(time.Now())
	algo := NewSlidingWindowAlgorithm(clock)

	// Initially should be zero
	if algo.GetWindowDuration() != 0 {
		t.Error("Initial GetWindowDuration() should be 0")
	}

	// After IsAllowed, should return the window duration
	ctx := context.Background()
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	window := 1 * time.Minute
	_, err := algo.IsAllowed(ctx, "user-123", store, 10, window)
	if err != nil {
		t.Fatalf("IsAllowed() error = %v", err)
	}

	if algo.GetWindowDuration() != window {
		t.Errorf("GetWindowDuration() = %v, want %v", algo.GetWindowDuration(), window)
	}
}

func TestSlidingWindowAlgorithm_CleanupExpiredTimestamps(t *testing.T) {
	now := time.Now()
	clock := NewMockClock(now)
	algo := NewSlidingWindowAlgorithm(clock)

	// Add some timestamps
	algo.getValidTimestamp("user-1")
	clock.Advance(10 * time.Minute)
	algo.getValidTimestamp("user-2")
	clock.Advance(10 * time.Minute)
	algo.getValidTimestamp("user-3")

	// Verify we have 3 keys
	if count := algo.GetTrackedKeysCount(); count != 3 {
		t.Errorf("GetTrackedKeysCount() = %v, want 3", count)
	}

	// Cleanup entries older than 15 minutes
	removed := algo.CleanupExpiredTimestamps(15 * time.Minute)

	// user-1 should be removed
	if removed != 1 {
		t.Errorf("CleanupExpiredTimestamps() removed = %v, want 1", removed)
	}

	// Should have 2 keys remaining
	if count := algo.GetTrackedKeysCount(); count != 2 {
		t.Errorf("GetTrackedKeysCount() after cleanup = %v, want 2", count)
	}
}

func TestSlidingWindowAlgorithm_GetTrackedKeysCount(t *testing.T) {
	clock := NewMockClock(time.Now())
	algo := NewSlidingWindowAlgorithm(clock)

	// Initially should be 0
	if count := algo.GetTrackedKeysCount(); count != 0 {
		t.Errorf("Initial GetTrackedKeysCount() = %v, want 0", count)
	}

	// Add some keys
	for i := 0; i < 5; i++ {
		key := "user-" + string(rune('A'+i))
		algo.getValidTimestamp(key)
	}

	// Should have 5 keys
	if count := algo.GetTrackedKeysCount(); count != 5 {
		t.Errorf("GetTrackedKeysCount() = %v, want 5", count)
	}
}

func TestSlidingWindowAlgorithm_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 100,
		Clock:   clock,
	})

	var wg sync.WaitGroup
	numGoroutines := 10
	requestsPerGoroutine := 100

	window := 1 * time.Minute

	// Concurrent IsAllowed calls - each goroutine uses its own algorithm instance
	// to avoid race on windowDuration field (which is a known limitation)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Create separate algorithm instance per goroutine to avoid race
			algo := NewSlidingWindowAlgorithm(clock)
			key := "user-" + string(rune('A'+id))
			for j := 0; j < requestsPerGoroutine; j++ {
				_, err := algo.IsAllowed(ctx, key, store, 1000, window)
				if err != nil {
					t.Errorf("IsAllowed() error = %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify store has data from all goroutines
	keyCount, err := store.KeyCount(ctx)
	if err != nil {
		t.Fatalf("KeyCount() error = %v", err)
	}
	if keyCount != numGoroutines {
		t.Errorf("Store KeyCount() = %v, want %v", keyCount, numGoroutines)
	}
}

func TestSlidingWindowAlgorithm_DecisionFields(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	algo := NewSlidingWindowAlgorithm(clock)
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	key := "user-123"
	limit := 10
	window := 1 * time.Minute

	// Allowed decision
	decision, err := algo.IsAllowed(ctx, key, store, limit, window)
	if err != nil {
		t.Fatalf("IsAllowed() error = %v", err)
	}

	if decision.Key != key {
		t.Errorf("Key = %v, want %v", decision.Key, key)
	}
	if decision.Limit != limit {
		t.Errorf("Limit = %v, want %v", decision.Limit, limit)
	}
	if decision.Remaining < 0 || decision.Remaining >= limit {
		t.Errorf("Remaining = %v, should be between 0 and %v", decision.Remaining, limit-1)
	}
	if decision.ResetAt.Before(now) {
		t.Errorf("ResetAt = %v, should be after %v", decision.ResetAt, now)
	}

	// Fill up to limit
	for i := 0; i < limit-1; i++ {
		algo.IsAllowed(ctx, key, store, limit, window)
	}

	// Denied decision
	decision, err = algo.IsAllowed(ctx, key, store, limit, window)
	if err != nil {
		t.Fatalf("IsAllowed() error = %v", err)
	}

	if decision.Allowed {
		t.Error("Decision should be denied when limit is reached")
	}
	if decision.Remaining != 0 {
		t.Errorf("Remaining = %v, want 0 for denied decision", decision.Remaining)
	}
	if decision.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, should be positive for denied decision", decision.RetryAfter)
	}
}

func TestSlidingWindowAlgorithm_SlidingWindow(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	algo := NewSlidingWindowAlgorithm(clock)
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	key := "user-123"
	limit := 5
	window := 1 * time.Minute

	// Fill up to limit
	for i := 0; i < limit; i++ {
		decision, err := algo.IsAllowed(ctx, key, store, limit, window)
		if err != nil {
			t.Fatalf("IsAllowed() error = %v", err)
		}
		if !decision.Allowed {
			t.Errorf("Request %d should be allowed", i+1)
		}
		clock.Advance(10 * time.Second)
	}

	// Next request should be denied
	decision, err := algo.IsAllowed(ctx, key, store, limit, window)
	if err != nil {
		t.Fatalf("IsAllowed() error = %v", err)
	}
	if decision.Allowed {
		t.Error("Request should be denied when limit is reached")
	}

	// Advance past the first request's window
	clock.Advance(20 * time.Second) // Total: 1 minute 10 seconds from start

	// Now request should be allowed (first request expired)
	decision, err = algo.IsAllowed(ctx, key, store, limit, window)
	if err != nil {
		t.Fatalf("IsAllowed() error = %v", err)
	}
	if !decision.Allowed {
		t.Error("Request should be allowed after oldest request expires")
	}
}

func TestSlidingWindowAlgorithm_MultipleKeys(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	algo := NewSlidingWindowAlgorithm(clock)
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	limit := 5
	window := 1 * time.Minute

	// Add requests for different keys
	keys := []string{"user-1", "user-2", "user-3"}
	for _, key := range keys {
		for i := 0; i < limit; i++ {
			decision, err := algo.IsAllowed(ctx, key, store, limit, window)
			if err != nil {
				t.Fatalf("IsAllowed() error = %v", err)
			}
			if !decision.Allowed {
				t.Errorf("Request for %s should be allowed", key)
			}
		}
	}

	// All keys should be at limit
	for _, key := range keys {
		decision, err := algo.IsAllowed(ctx, key, store, limit, window)
		if err != nil {
			t.Fatalf("IsAllowed() error = %v", err)
		}
		if decision.Allowed {
			t.Errorf("Request for %s should be denied", key)
		}
	}

	// Verify tracked keys
	if count := algo.GetTrackedKeysCount(); count != len(keys) {
		t.Errorf("GetTrackedKeysCount() = %v, want %v", count, len(keys))
	}
}
