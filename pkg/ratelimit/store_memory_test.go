package ratelimit

import (
	"context"
	"sync"
	"testing"
	"time"
)

// MockClock implements Clock interface for testing
type MockClock struct {
	mu  sync.RWMutex
	now time.Time
}

func NewMockClock(t time.Time) *MockClock {
	return &MockClock{now: t}
}

func (m *MockClock) Now() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.now
}

func (m *MockClock) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(d)
}

func (m *MockClock) Set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = t
}

func TestNewInMemoryRateLimitStore(t *testing.T) {
	tests := []struct {
		name        string
		config      InMemoryStoreConfig
		wantMaxKeys int
	}{
		{
			name: "with valid config",
			config: InMemoryStoreConfig{
				MaxKeys: 5000,
				Clock:   &SystemClock{},
			},
			wantMaxKeys: 5000,
		},
		{
			name: "with zero max keys should use default",
			config: InMemoryStoreConfig{
				MaxKeys: 0,
				Clock:   &SystemClock{},
			},
			wantMaxKeys: 10000,
		},
		{
			name: "with negative max keys should use default",
			config: InMemoryStoreConfig{
				MaxKeys: -1,
				Clock:   &SystemClock{},
			},
			wantMaxKeys: 10000,
		},
		{
			name: "with nil clock should use system clock",
			config: InMemoryStoreConfig{
				MaxKeys: 5000,
				Clock:   nil,
			},
			wantMaxKeys: 5000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryRateLimitStore(tt.config)
			if store == nil {
				t.Fatal("NewInMemoryRateLimitStore() returned nil")
			}
			if store.maxKeys != tt.wantMaxKeys {
				t.Errorf("maxKeys = %v, want %v", store.maxKeys, tt.wantMaxKeys)
			}
			if store.clock == nil {
				t.Error("clock should not be nil")
			}
		})
	}
}

func TestDefaultInMemoryStoreConfig(t *testing.T) {
	config := DefaultInMemoryStoreConfig()

	if config.MaxKeys != 10000 {
		t.Errorf("MaxKeys = %v, want 10000", config.MaxKeys)
	}
	if config.Clock == nil {
		t.Error("Clock should not be nil")
	}
}

func TestInMemoryRateLimitStore_AddRequest(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	// Add first request
	err := store.AddRequest(ctx, "user-123", now)
	if err != nil {
		t.Errorf("AddRequest() error = %v, want nil", err)
	}

	// Verify request was added
	count, err := store.KeyCount(ctx)
	if err != nil {
		t.Fatalf("KeyCount() error = %v", err)
	}
	if count != 1 {
		t.Errorf("KeyCount() = %v, want 1", count)
	}

	// Add another request for the same key
	err = store.AddRequest(ctx, "user-123", now.Add(1*time.Second))
	if err != nil {
		t.Errorf("AddRequest() error = %v, want nil", err)
	}

	// Key count should still be 1
	count, err = store.KeyCount(ctx)
	if err != nil {
		t.Fatalf("KeyCount() error = %v", err)
	}
	if count != 1 {
		t.Errorf("KeyCount() = %v, want 1 (same key)", count)
	}

	// Add request for different key
	err = store.AddRequest(ctx, "user-456", now)
	if err != nil {
		t.Errorf("AddRequest() error = %v, want nil", err)
	}

	// Key count should be 2
	count, err = store.KeyCount(ctx)
	if err != nil {
		t.Fatalf("KeyCount() error = %v", err)
	}
	if count != 2 {
		t.Errorf("KeyCount() = %v, want 2", count)
	}
}

func TestInMemoryRateLimitStore_GetRequests(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	key := "user-123"

	// Add requests at different times
	timestamps := []time.Time{
		now.Add(-10 * time.Minute),
		now.Add(-5 * time.Minute),
		now.Add(-2 * time.Minute),
		now.Add(-30 * time.Second),
	}

	for _, ts := range timestamps {
		err := store.AddRequest(ctx, key, ts)
		if err != nil {
			t.Fatalf("AddRequest() error = %v", err)
		}
	}

	tests := []struct {
		name      string
		cutoff    time.Time
		wantCount int
	}{
		{
			name:      "cutoff before all requests",
			cutoff:    now.Add(-15 * time.Minute),
			wantCount: 4,
		},
		{
			name:      "cutoff after some requests",
			cutoff:    now.Add(-3 * time.Minute),
			wantCount: 2,
		},
		{
			name:      "cutoff after all requests",
			cutoff:    now,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests, err := store.GetRequests(ctx, key, tt.cutoff)
			if err != nil {
				t.Errorf("GetRequests() error = %v", err)
			}
			if len(requests) != tt.wantCount {
				t.Errorf("GetRequests() count = %v, want %v", len(requests), tt.wantCount)
			}
		})
	}

	// Test non-existent key
	requests, err := store.GetRequests(ctx, "non-existent", now)
	if err != nil {
		t.Errorf("GetRequests() error = %v", err)
	}
	if len(requests) != 0 {
		t.Errorf("GetRequests() for non-existent key should return empty slice, got %v", len(requests))
	}
}

func TestInMemoryRateLimitStore_GetRequestCount(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	key := "user-123"

	// Add 5 requests
	for i := 0; i < 5; i++ {
		err := store.AddRequest(ctx, key, now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatalf("AddRequest() error = %v", err)
		}
	}

	// Count all requests
	count, err := store.GetRequestCount(ctx, key, now.Add(-1*time.Minute))
	if err != nil {
		t.Errorf("GetRequestCount() error = %v", err)
	}
	if count != 5 {
		t.Errorf("GetRequestCount() = %v, want 5", count)
	}

	// Count only recent requests (after 2 seconds means timestamps at 2, 3, 4 seconds)
	count, err = store.GetRequestCount(ctx, key, now.Add(2*time.Second))
	if err != nil {
		t.Errorf("GetRequestCount() error = %v", err)
	}
	// Timestamps at index 2, 3, 4 are at 2s, 3s, 4s which are NOT after 2s
	// Only timestamps strictly After the cutoff are counted
	if count != 2 {
		t.Errorf("GetRequestCount() = %v, want 2", count)
	}

	// Count non-existent key
	count, err = store.GetRequestCount(ctx, "non-existent", now)
	if err != nil {
		t.Errorf("GetRequestCount() error = %v", err)
	}
	if count != 0 {
		t.Errorf("GetRequestCount() for non-existent key = %v, want 0", count)
	}
}

func TestInMemoryRateLimitStore_Cleanup(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	// Add old and recent requests for multiple keys
	err := store.AddRequest(ctx, "user-1", now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("AddRequest() error = %v", err)
	}
	err = store.AddRequest(ctx, "user-2", now.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("AddRequest() error = %v", err)
	}
	err = store.AddRequest(ctx, "user-3", now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("AddRequest() error = %v", err)
	}

	// Cleanup old requests (older than 1 hour)
	err = store.Cleanup(ctx, now.Add(-1*time.Hour))
	if err != nil {
		t.Errorf("Cleanup() error = %v", err)
	}

	// user-1 should be removed (all timestamps expired)
	count, err := store.GetRequestCount(ctx, "user-1", time.Time{})
	if err != nil {
		t.Fatalf("GetRequestCount() error = %v", err)
	}
	if count != 0 {
		t.Errorf("user-1 should have 0 requests after cleanup, got %v", count)
	}

	// user-2 and user-3 should still exist
	keyCount, err := store.KeyCount(ctx)
	if err != nil {
		t.Fatalf("KeyCount() error = %v", err)
	}
	if keyCount != 2 {
		t.Errorf("KeyCount() = %v, want 2 after cleanup", keyCount)
	}
}

func TestInMemoryRateLimitStore_LRUEviction(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	// Create store with small capacity
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	// Fill up to max capacity
	for i := 0; i < 10; i++ {
		key := "user-" + string(rune('A'+i))
		err := store.AddRequest(ctx, key, now)
		if err != nil {
			t.Fatalf("AddRequest() error = %v", err)
		}
	}

	// Verify we have 10 keys
	count, err := store.KeyCount(ctx)
	if err != nil {
		t.Fatalf("KeyCount() error = %v", err)
	}
	if count != 10 {
		t.Errorf("KeyCount() = %v, want 10", count)
	}

	// Add one more key, should trigger eviction
	err = store.AddRequest(ctx, "user-NEW", now)
	if err != nil {
		t.Fatalf("AddRequest() error = %v", err)
	}

	// Should still have 10 keys (one was evicted)
	count, err = store.KeyCount(ctx)
	if err != nil {
		t.Fatalf("KeyCount() error = %v", err)
	}
	if count != 10 {
		t.Errorf("KeyCount() = %v, want 10 after eviction", count)
	}

	// New key should exist
	newKeyCount, err := store.GetRequestCount(ctx, "user-NEW", time.Time{})
	if err != nil {
		t.Fatalf("GetRequestCount() error = %v", err)
	}
	if newKeyCount == 0 {
		t.Error("New key should exist after eviction")
	}
}

func TestInMemoryRateLimitStore_MemoryUsage(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	// Empty store should have minimal memory
	usage, err := store.MemoryUsage(ctx)
	if err != nil {
		t.Fatalf("MemoryUsage() error = %v", err)
	}
	if usage != 0 {
		t.Errorf("MemoryUsage() for empty store = %v, want 0", usage)
	}

	// Add some requests
	for i := 0; i < 5; i++ {
		key := "user-" + string(rune('A'+i))
		for j := 0; j < 10; j++ {
			err := store.AddRequest(ctx, key, now.Add(time.Duration(j)*time.Second))
			if err != nil {
				t.Fatalf("AddRequest() error = %v", err)
			}
		}
	}

	// Memory usage should increase
	usage, err = store.MemoryUsage(ctx)
	if err != nil {
		t.Fatalf("MemoryUsage() error = %v", err)
	}
	if usage == 0 {
		t.Error("MemoryUsage() should be > 0 after adding requests")
	}
}

func TestInMemoryRateLimitStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 1000,
		Clock:   clock,
	})

	// Run concurrent operations
	var wg sync.WaitGroup
	numGoroutines := 10
	requestsPerGoroutine := 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "user-" + string(rune('A'+id))
			for j := 0; j < requestsPerGoroutine; j++ {
				err := store.AddRequest(ctx, key, now.Add(time.Duration(j)*time.Millisecond))
				if err != nil {
					t.Errorf("AddRequest() error = %v", err)
				}
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := "user-" + string(rune('A'+id))
			for j := 0; j < requestsPerGoroutine; j++ {
				_, err := store.GetRequestCount(ctx, key, now)
				if err != nil {
					t.Errorf("GetRequestCount() error = %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify final state
	count, err := store.KeyCount(ctx)
	if err != nil {
		t.Fatalf("KeyCount() error = %v", err)
	}
	if count != numGoroutines {
		t.Errorf("KeyCount() = %v, want %v", count, numGoroutines)
	}

	// Verify each key has correct number of requests
	for i := 0; i < numGoroutines; i++ {
		key := "user-" + string(rune('A'+i))
		requestCount, err := store.GetRequestCount(ctx, key, time.Time{})
		if err != nil {
			t.Fatalf("GetRequestCount() error = %v", err)
		}
		if requestCount != requestsPerGoroutine {
			t.Errorf("key %v has %v requests, want %v", key, requestCount, requestsPerGoroutine)
		}
	}
}

func TestInMemoryRateLimitStore_KeyCount(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	// Initial count should be 0
	count, err := store.KeyCount(ctx)
	if err != nil {
		t.Fatalf("KeyCount() error = %v", err)
	}
	if count != 0 {
		t.Errorf("initial KeyCount() = %v, want 0", count)
	}

	// Add some keys
	for i := 0; i < 5; i++ {
		key := "user-" + string(rune('A'+i))
		err := store.AddRequest(ctx, key, now)
		if err != nil {
			t.Fatalf("AddRequest() error = %v", err)
		}
	}

	// Count should be 5
	count, err = store.KeyCount(ctx)
	if err != nil {
		t.Fatalf("KeyCount() error = %v", err)
	}
	if count != 5 {
		t.Errorf("KeyCount() = %v, want 5", count)
	}
}

func TestInMemoryRateLimitStore_EdgeCases(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	clock := NewMockClock(now)

	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10,
		Clock:   clock,
	})

	t.Run("empty key", func(t *testing.T) {
		err := store.AddRequest(ctx, "", now)
		if err != nil {
			t.Errorf("AddRequest() with empty key error = %v, want nil", err)
		}

		count, err := store.GetRequestCount(ctx, "", time.Time{})
		if err != nil {
			t.Errorf("GetRequestCount() with empty key error = %v", err)
		}
		if count != 1 {
			t.Errorf("GetRequestCount() = %v, want 1", count)
		}
	})

	t.Run("very long key", func(t *testing.T) {
		longKey := string(make([]byte, 10000))
		err := store.AddRequest(ctx, longKey, now)
		if err != nil {
			t.Errorf("AddRequest() with long key error = %v, want nil", err)
		}
	})

	t.Run("zero timestamp", func(t *testing.T) {
		err := store.AddRequest(ctx, "user-zero", time.Time{})
		if err != nil {
			t.Errorf("AddRequest() with zero timestamp error = %v, want nil", err)
		}

		// Use a cutoff before zero time to count the zero timestamp
		count, err := store.GetRequestCount(ctx, "user-zero", time.Time{}.Add(-1*time.Second))
		if err != nil {
			t.Errorf("GetRequestCount() error = %v", err)
		}
		if count != 1 {
			t.Errorf("GetRequestCount() = %v, want 1", count)
		}
	})
}
