// Package ratelimit provides framework-agnostic rate limiting functionality.
package ratelimit

import (
	"context"
	"sync"
	"time"
)

// InMemoryRateLimitStore is a thread-safe in-memory implementation of RateLimitStore.
//
// This store uses a map to track request timestamps for each key (e.g., IP address, user ID).
// It includes memory management features such as:
//   - Maximum key limit to prevent unbounded memory growth
//   - LRU (Least Recently Used) eviction policy when capacity is reached
//   - Automatic cleanup of expired entries
//
// The store is optimized for read-heavy workloads using sync.RWMutex.
type InMemoryRateLimitStore struct {
	mu       sync.RWMutex
	requests map[string]*timestampList
	maxKeys  int
	clock    Clock

	// LRU tracking
	lruList *lruList
}

// timestampList holds timestamps for a single key.
type timestampList struct {
	timestamps []time.Time
	lastAccess time.Time
}

// lruList maintains a doubly-linked list of keys ordered by last access time.
type lruList struct {
	head *lruNode
	tail *lruNode
	keys map[string]*lruNode
}

// lruNode represents a node in the LRU list.
type lruNode struct {
	key  string
	prev *lruNode
	next *lruNode
}

// InMemoryStoreConfig holds configuration for InMemoryRateLimitStore.
type InMemoryStoreConfig struct {
	// MaxKeys is the maximum number of keys to store in memory.
	// When this limit is reached, the least recently used keys are evicted.
	// Default: 10000
	MaxKeys int

	// Clock provides time operations for testing.
	// Default: SystemClock
	Clock Clock
}

// DefaultInMemoryStoreConfig returns the default configuration.
func DefaultInMemoryStoreConfig() InMemoryStoreConfig {
	return InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	}
}

// NewInMemoryRateLimitStore creates a new in-memory rate limit store with the given configuration.
func NewInMemoryRateLimitStore(config InMemoryStoreConfig) *InMemoryRateLimitStore {
	if config.MaxKeys <= 0 {
		config.MaxKeys = 10000
	}
	if config.Clock == nil {
		config.Clock = &SystemClock{}
	}

	return &InMemoryRateLimitStore{
		requests: make(map[string]*timestampList),
		maxKeys:  config.MaxKeys,
		clock:    config.Clock,
		lruList:  newLRUList(),
	}
}

// newLRUList creates a new LRU list.
func newLRUList() *lruList {
	return &lruList{
		keys: make(map[string]*lruNode),
	}
}

// AddRequest records a new request timestamp for the given key.
//
// If the key does not exist, it is created. If the store has reached
// its maximum capacity, the least recently used keys are evicted.
//
// This method is thread-safe.
func (s *InMemoryRateLimitStore) AddRequest(ctx context.Context, key string, timestamp time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if we need to evict before adding
	if len(s.requests) >= s.maxKeys {
		// Check if this is a new key (not just adding to existing key)
		if _, exists := s.requests[key]; !exists {
			s.evictLRU()
		}
	}

	// Get or create timestamp list for this key
	tsList, exists := s.requests[key]
	if !exists {
		tsList = &timestampList{
			timestamps: make([]time.Time, 0, 100), // Pre-allocate for efficiency
			lastAccess: timestamp,
		}
		s.requests[key] = tsList
	} else {
		tsList.lastAccess = timestamp
	}

	// Add the timestamp
	tsList.timestamps = append(tsList.timestamps, timestamp)

	// Update LRU list
	s.lruList.touch(key)

	return nil
}

// GetRequests retrieves all request timestamps for the given key
// that occurred after the cutoff time.
//
// This method is thread-safe and uses a read lock for better concurrency.
func (s *InMemoryRateLimitStore) GetRequests(ctx context.Context, key string, cutoff time.Time) ([]time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tsList, exists := s.requests[key]
	if !exists {
		return []time.Time{}, nil
	}

	// Filter timestamps that are after cutoff
	result := make([]time.Time, 0, len(tsList.timestamps))
	for _, ts := range tsList.timestamps {
		if ts.After(cutoff) {
			result = append(result, ts)
		}
	}

	return result, nil
}

// GetRequestCount returns the number of requests for the given key
// that occurred after the cutoff time.
//
// This is more efficient than GetRequests when only the count is needed.
//
// This method is thread-safe and uses a read lock for better concurrency.
func (s *InMemoryRateLimitStore) GetRequestCount(ctx context.Context, key string, cutoff time.Time) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tsList, exists := s.requests[key]
	if !exists {
		return 0, nil
	}

	// Count timestamps that are after cutoff
	count := 0
	for _, ts := range tsList.timestamps {
		if ts.After(cutoff) {
			count++
		}
	}

	return count, nil
}

// Cleanup removes expired request timestamps from storage.
//
// This method removes timestamps older than the cutoff time from all keys.
// If a key has no remaining timestamps, the key is removed entirely.
//
// This method is thread-safe.
func (s *InMemoryRateLimitStore) Cleanup(ctx context.Context, cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	keysToRemove := make([]string, 0)

	for key, tsList := range s.requests {
		// Filter timestamps that are after cutoff
		validTimestamps := make([]time.Time, 0, len(tsList.timestamps))
		for _, ts := range tsList.timestamps {
			if ts.After(cutoff) {
				validTimestamps = append(validTimestamps, ts)
			}
		}

		// If no valid timestamps remain, mark key for removal
		if len(validTimestamps) == 0 {
			keysToRemove = append(keysToRemove, key)
		} else {
			// Update the timestamp list
			tsList.timestamps = validTimestamps
		}
	}

	// Remove keys with no valid timestamps
	for _, key := range keysToRemove {
		delete(s.requests, key)
		s.lruList.remove(key)
	}

	return nil
}

// KeyCount returns the number of active keys currently in storage.
//
// This method is thread-safe and uses a read lock for better concurrency.
func (s *InMemoryRateLimitStore) KeyCount(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.requests), nil
}

// MemoryUsage returns the estimated memory usage in bytes.
//
// This calculation includes:
//   - Map overhead (approximately 48 bytes per entry)
//   - Timestamp slice overhead
//   - Each timestamp (24 bytes)
//   - LRU list overhead
//
// This method is thread-safe and uses a read lock for better concurrency.
func (s *InMemoryRateLimitStore) MemoryUsage(ctx context.Context) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	const (
		mapEntryOverhead       = 48 // Approximate bytes per map entry
		timestampSize          = 24 // Size of time.Time
		timestampListOverhead  = 32 // Overhead for timestampList struct
		lruNodeSize            = 48 // Size of lruNode
	)

	var totalBytes int64

	// Calculate memory for requests map
	for _, tsList := range s.requests {
		// Map entry overhead
		totalBytes += mapEntryOverhead

		// timestampList struct overhead
		totalBytes += timestampListOverhead

		// Timestamps
		totalBytes += int64(len(tsList.timestamps) * timestampSize)
	}

	// Calculate memory for LRU list
	totalBytes += int64(len(s.lruList.keys) * lruNodeSize)

	return totalBytes, nil
}

// evictLRU evicts the least recently used keys when memory limit is reached.
//
// This method evicts 10% of the keys to avoid frequent evictions.
//
// This method must be called while holding the write lock.
func (s *InMemoryRateLimitStore) evictLRU() {
	evictCount := s.maxKeys / 10
	if evictCount < 1 {
		evictCount = 1
	}

	evicted := 0
	for evicted < evictCount && s.lruList.tail != nil {
		// Remove the least recently used key
		key := s.lruList.tail.key
		delete(s.requests, key)
		s.lruList.remove(key)
		evicted++
	}
}

// touch updates the last access time for a key in the LRU list.
//
// If the key is not in the list, it is added. If it is already in the list,
// it is moved to the front (most recently used position).
//
// This method must be called while holding the write lock.
func (l *lruList) touch(key string) {
	_, exists := l.keys[key]
	if exists {
		// Move to front
		l.remove(key)
	}

	// Add to front
	newNode := &lruNode{
		key:  key,
		next: l.head,
	}

	if l.head != nil {
		l.head.prev = newNode
	}
	l.head = newNode

	if l.tail == nil {
		l.tail = newNode
	}

	l.keys[key] = newNode
}

// remove removes a key from the LRU list.
//
// This method must be called while holding the write lock.
func (l *lruList) remove(key string) {
	node, exists := l.keys[key]
	if !exists {
		return
	}

	// Update previous node
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		// This was the head
		l.head = node.next
	}

	// Update next node
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		// This was the tail
		l.tail = node.prev
	}

	delete(l.keys, key)
}

// CheckAndAddRequest atomically checks if a request is within the rate limit
// and adds it to the store if allowed.
//
// This method prevents TOCTOU (Time-of-Check to Time-of-Use) race conditions
// by performing the check and add within a single lock acquisition.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - key: Unique identifier for the rate limit subject
//   - timestamp: Time when the request occurred
//   - cutoff: Only count timestamps after this time
//   - limit: Maximum number of requests allowed
//
// Returns:
//   - allowed: true if the request was within limit and added
//   - count: Current count of requests in the window (after adding if allowed)
//   - err: Error if the operation fails
func (s *InMemoryRateLimitStore) CheckAndAddRequest(ctx context.Context, key string, timestamp time.Time, cutoff time.Time, limit int) (allowed bool, count int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Count current requests within the window
	tsList, exists := s.requests[key]
	currentCount := 0

	if exists {
		for _, ts := range tsList.timestamps {
			if ts.After(cutoff) {
				currentCount++
			}
		}
	}

	// Check if request is allowed
	if currentCount >= limit {
		// Request denied - return current count without adding
		return false, currentCount, nil
	}

	// Request allowed - add it to the store

	// Check if we need to evict before adding
	if len(s.requests) >= s.maxKeys {
		// Check if this is a new key (not just adding to existing key)
		if !exists {
			s.evictLRU()
		}
	}

	// Get or create timestamp list for this key
	if !exists {
		tsList = &timestampList{
			timestamps: make([]time.Time, 0, 100), // Pre-allocate for efficiency
			lastAccess: timestamp,
		}
		s.requests[key] = tsList
	} else {
		tsList.lastAccess = timestamp
	}

	// Add the timestamp
	tsList.timestamps = append(tsList.timestamps, timestamp)

	// Update LRU list
	s.lruList.touch(key)

	// Return the count after adding (+1 for the current request)
	return true, currentCount + 1, nil
}
