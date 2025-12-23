package middleware

import (
	"context"
	"sync"
	"time"

	"catchup-feed/pkg/ratelimit"
)

// mockRateLimitStore is a mock implementation of RateLimitStore for testing.
type mockRateLimitStore struct {
	requests     map[string][]time.Time
	mu           sync.RWMutex
	addErr       error
	getErr       error
	getCountErr  error
	cleanupErr   error
	keyCountErr  error
	memoryErr    error
	keyCount     int
	memoryUsage  int64
}

func newMockRateLimitStore() *mockRateLimitStore {
	return &mockRateLimitStore{
		requests: make(map[string][]time.Time),
	}
}

func (m *mockRateLimitStore) AddRequest(ctx context.Context, key string, timestamp time.Time) error {
	if m.addErr != nil {
		return m.addErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests[key] = append(m.requests[key], timestamp)
	return nil
}

func (m *mockRateLimitStore) GetRequests(ctx context.Context, key string, cutoff time.Time) ([]time.Time, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	timestamps, exists := m.requests[key]
	if !exists {
		return []time.Time{}, nil
	}

	var validTimestamps []time.Time
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			validTimestamps = append(validTimestamps, ts)
		}
	}

	return validTimestamps, nil
}

func (m *mockRateLimitStore) GetRequestCount(ctx context.Context, key string, cutoff time.Time) (int, error) {
	if m.getCountErr != nil {
		return 0, m.getCountErr
	}

	timestamps, err := m.GetRequests(ctx, key, cutoff)
	if err != nil {
		return 0, err
	}

	return len(timestamps), nil
}

func (m *mockRateLimitStore) Cleanup(ctx context.Context, cutoff time.Time) error {
	if m.cleanupErr != nil {
		return m.cleanupErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for key, timestamps := range m.requests {
		var validTimestamps []time.Time
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				validTimestamps = append(validTimestamps, ts)
			}
		}

		if len(validTimestamps) > 0 {
			m.requests[key] = validTimestamps
		} else {
			delete(m.requests, key)
		}
	}

	return nil
}

func (m *mockRateLimitStore) KeyCount(ctx context.Context) (int, error) {
	if m.keyCountErr != nil {
		return 0, m.keyCountErr
	}

	if m.keyCount > 0 {
		return m.keyCount, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.requests), nil
}

func (m *mockRateLimitStore) MemoryUsage(ctx context.Context) (int64, error) {
	if m.memoryErr != nil {
		return 0, m.memoryErr
	}

	if m.memoryUsage > 0 {
		return m.memoryUsage, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Rough estimate: 100 bytes per key + 8 bytes per timestamp
	var usage int64
	for key, timestamps := range m.requests {
		usage += int64(len(key)) + int64(len(timestamps)*8)
	}

	return usage, nil
}

// CheckAndAddRequest implements AtomicRateLimitStore interface for thread-safe rate limiting.
// This atomically checks if a request is within the limit and adds it if allowed.
func (m *mockRateLimitStore) CheckAndAddRequest(ctx context.Context, key string, timestamp time.Time, cutoff time.Time, limit int) (allowed bool, count int, err error) {
	if m.addErr != nil {
		return false, 0, m.addErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Count current requests within the window
	timestamps, exists := m.requests[key]
	currentCount := 0
	if exists {
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				currentCount++
			}
		}
	}

	// Check if request is allowed
	if currentCount >= limit {
		return false, currentCount, nil
	}

	// Add the request
	m.requests[key] = append(m.requests[key], timestamp)

	return true, currentCount + 1, nil
}

// mockRateLimitAlgorithm is a mock implementation of RateLimitAlgorithm for testing.
type mockRateLimitAlgorithm struct {
	decision *ratelimit.RateLimitDecision
	err      error
	window   time.Duration
}

func (m *mockRateLimitAlgorithm) IsAllowed(ctx context.Context, key string, store ratelimit.RateLimitStore, limit int, window time.Duration) (*ratelimit.RateLimitDecision, error) {
	if m.err != nil {
		return nil, m.err
	}

	if m.decision != nil {
		return m.decision, nil
	}

	now := time.Now()
	cutoff := now.Add(-window)
	resetAt := now.Add(window)

	// Use atomic operation if available to prevent TOCTOU race conditions
	if atomicStore, ok := store.(ratelimit.AtomicRateLimitStore); ok {
		allowed, count, err := atomicStore.CheckAndAddRequest(ctx, key, now, cutoff, limit)
		if err != nil {
			return nil, err
		}

		if allowed {
			remaining := limit - count
			return ratelimit.NewAllowedDecision(key, "test", limit, remaining, resetAt), nil
		}

		return ratelimit.NewDeniedDecision(key, "test", limit, resetAt), nil
	}

	// Fallback: non-atomic behavior (has TOCTOU race condition)
	count, err := store.GetRequestCount(ctx, key, cutoff)
	if err != nil {
		return nil, err
	}

	allowed := count < limit
	remaining := limit - count - 1
	if remaining < 0 {
		remaining = 0
	}

	if allowed {
		// Add the request
		store.AddRequest(ctx, key, now)
		return ratelimit.NewAllowedDecision(key, "test", limit, remaining, resetAt), nil
	}

	return ratelimit.NewDeniedDecision(key, "test", limit, resetAt), nil
}

func (m *mockRateLimitAlgorithm) GetWindowDuration() time.Duration {
	if m.window > 0 {
		return m.window
	}
	return 1 * time.Minute
}

// mockRateLimitMetrics is a mock implementation of RateLimitMetrics for testing.
type mockRateLimitMetrics struct {
	requests           int
	denied             int
	allowed            int
	checkDurations     []time.Duration
	activeKeys         int
	circuitStates      []string
	degradationLevels  []int
	evictions          int
	mu                 sync.Mutex
}

func newMockRateLimitMetrics() *mockRateLimitMetrics {
	return &mockRateLimitMetrics{}
}

func (m *mockRateLimitMetrics) RecordRequest(limiterType, endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests++
}

func (m *mockRateLimitMetrics) RecordDenied(limiterType, endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.denied++
}

func (m *mockRateLimitMetrics) RecordAllowed(limiterType, endpoint string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowed++
}

func (m *mockRateLimitMetrics) RecordCheckDuration(limiterType string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkDurations = append(m.checkDurations, duration)
}

func (m *mockRateLimitMetrics) SetActiveKeys(limiterType string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeKeys = count
}

func (m *mockRateLimitMetrics) RecordCircuitState(limiterType, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.circuitStates = append(m.circuitStates, state)
}

func (m *mockRateLimitMetrics) RecordDegradationLevel(limiterType string, level int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.degradationLevels = append(m.degradationLevels, level)
}

func (m *mockRateLimitMetrics) RecordEviction(limiterType string, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evictions += count
}

// mockClock is a mock implementation of Clock for testing time-dependent behavior.
type mockClock struct {
	mu      sync.Mutex
	current time.Time
}

func newMockClock(start time.Time) *mockClock {
	return &mockClock{
		current: start,
	}
}

func (c *mockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *mockClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}
