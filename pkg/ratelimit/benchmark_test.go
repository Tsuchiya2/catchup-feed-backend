package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// BenchmarkInMemoryStore_AddRequest benchmarks the AddRequest operation.
//
// This benchmark tests the performance of adding request timestamps to the store.
// Target: <1ms per operation
func BenchmarkInMemoryStore_AddRequest(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("ip:%d", i%1000)
		store.AddRequest(ctx, key, time.Now())
	}
}

// BenchmarkInMemoryStore_AddRequest_SingleKey benchmarks AddRequest to a single key.
//
// This tests the performance when many requests come from the same IP/user.
func BenchmarkInMemoryStore_AddRequest_SingleKey(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.AddRequest(ctx, "ip:192.168.1.1", time.Now())
	}
}

// BenchmarkInMemoryStore_GetRequestCount benchmarks the GetRequestCount operation.
//
// This is the most critical operation as it's called on every rate limit check.
// Target: <1ms per operation
func BenchmarkInMemoryStore_GetRequestCount(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	ctx := context.Background()

	// Pre-populate the store with 1000 keys, each with 100 requests
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("ip:%d", i)
		for j := 0; j < 100; j++ {
			store.AddRequest(ctx, key, time.Now().Add(-time.Duration(j)*time.Second))
		}
	}

	cutoff := time.Now().Add(-1 * time.Minute)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("ip:%d", i%1000)
		store.GetRequestCount(ctx, key, cutoff)
	}
}

// BenchmarkInMemoryStore_Cleanup benchmarks the Cleanup operation.
//
// This operation runs periodically (every 5 minutes) to remove old timestamps.
// Target: <100ms for 10,000 keys
func BenchmarkInMemoryStore_Cleanup(b *testing.B) {
	ctx := context.Background()

	// Create a new store for each run
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
			MaxKeys: 10000,
			Clock:   &SystemClock{},
		})

		// Pre-populate the store
		now := time.Now()
		for j := 0; j < 1000; j++ {
			key := fmt.Sprintf("ip:%d", j)
			// Add old timestamps (should be cleaned up)
			for k := 0; k < 50; k++ {
				store.AddRequest(ctx, key, now.Add(-2*time.Hour))
			}
			// Add recent timestamps (should be kept)
			for k := 0; k < 50; k++ {
				store.AddRequest(ctx, key, now.Add(-30*time.Minute))
			}
		}
		b.StartTimer()

		// Cleanup timestamps older than 1 hour
		cutoff := now.Add(-1 * time.Hour)
		store.Cleanup(ctx, cutoff)
	}
}

// BenchmarkInMemoryStore_LRUEviction benchmarks LRU eviction performance.
//
// This tests the performance when the store reaches capacity and needs to evict keys.
// Target: <10ms for evicting 10% of keys
func BenchmarkInMemoryStore_LRUEviction(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
			MaxKeys: 1000, // Small max for faster benchmarking
			Clock:   &SystemClock{},
		})

		// Fill the store to capacity
		for j := 0; j < 1000; j++ {
			key := fmt.Sprintf("ip:%d", j)
			store.AddRequest(ctx, key, time.Now())
		}
		b.StartTimer()

		// Add a new key, triggering eviction
		store.AddRequest(ctx, "ip:new-key", time.Now())
	}
}

// BenchmarkSlidingWindow_IsAllowed benchmarks the core rate limiting algorithm.
//
// This is the most critical benchmark as it represents the full rate limit check.
// Target: <5ms p99 latency
func BenchmarkSlidingWindow_IsAllowed(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	algo := NewSlidingWindowAlgorithm(&SystemClock{})
	ctx := context.Background()

	limit := 100
	window := time.Minute

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("ip:%d", i%1000)
		algo.IsAllowed(ctx, key, store, limit, window)
	}
}

// BenchmarkSlidingWindow_IsAllowed_HighLoad benchmarks under high load.
//
// Simulates a scenario with many unique IPs making requests.
func BenchmarkSlidingWindow_IsAllowed_HighLoad(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	algo := NewSlidingWindowAlgorithm(&SystemClock{})
	ctx := context.Background()

	limit := 100
	window := time.Minute

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate 10,000 unique IPs
		key := fmt.Sprintf("ip:%d", i%10000)
		algo.IsAllowed(ctx, key, store, limit, window)
	}
}

// BenchmarkSlidingWindow_ConcurrentRequests benchmarks concurrent request handling.
//
// This tests the thread-safety and lock contention performance.
// Target: No significant performance degradation with concurrency
func BenchmarkSlidingWindow_ConcurrentRequests(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	algo := NewSlidingWindowAlgorithm(&SystemClock{})
	ctx := context.Background()

	limit := 100
	window := time.Minute

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("ip:%d", i%1000)
			algo.IsAllowed(ctx, key, store, limit, window)
			i++
		}
	})
}

// BenchmarkCircuitBreaker_Allow benchmarks the circuit breaker Allow check.
//
// This operation is called before every rate limit check.
// Target: <100μs per operation
func BenchmarkCircuitBreaker_Allow(b *testing.B) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 10,
		RecoveryTimeout:  30 * time.Second,
		Clock:            &SystemClock{},
		Metrics:          &NoOpMetrics{},
		LimiterType:      "ip",
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Allow()
	}
}

// BenchmarkCircuitBreaker_Execute benchmarks the circuit breaker Execute operation.
//
// This wraps the rate limit check with circuit breaker protection.
// Target: Minimal overhead (<1ms)
func BenchmarkCircuitBreaker_Execute(b *testing.B) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 10,
		RecoveryTimeout:  30 * time.Second,
		Clock:            &SystemClock{},
		Metrics:          &NoOpMetrics{},
		LimiterType:      "ip",
	})

	// Successful operation
	operation := func() error {
		// Simulate minimal work
		time.Sleep(100 * time.Microsecond)
		return nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Execute(operation)
	}
}

// BenchmarkCircuitBreaker_Execute_OpenCircuit benchmarks when circuit is open.
//
// When open, the circuit breaker should allow requests immediately (fail-open).
// Target: <10μs per operation (no actual rate limit check)
func BenchmarkCircuitBreaker_Execute_OpenCircuit(b *testing.B) {
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 10,
		RecoveryTimeout:  30 * time.Second,
		Clock:            &SystemClock{},
		Metrics:          &NoOpMetrics{},
		LimiterType:      "ip",
	})

	// Force the circuit open by recording failures
	for i := 0; i < 10; i++ {
		cb.RecordFailure()
	}

	operation := func() error {
		// This should never execute when circuit is open
		b.Fatal("operation executed when circuit is open")
		return nil
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cb.Execute(operation)
	}
}

// BenchmarkPrometheusMetrics_RecordRequest benchmarks metric recording.
//
// This operation happens on every request, so it should be very fast.
// Target: <100μs per operation
func BenchmarkPrometheusMetrics_RecordRequest(b *testing.B) {
	metrics := NewPrometheusMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordRequest("ip", "/api/articles")
	}
}

// BenchmarkPrometheusMetrics_RecordDenied benchmarks denied metric recording.
func BenchmarkPrometheusMetrics_RecordDenied(b *testing.B) {
	metrics := NewPrometheusMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordDenied("ip", "/api/articles")
	}
}

// BenchmarkPrometheusMetrics_RecordCheckDuration benchmarks duration recording.
func BenchmarkPrometheusMetrics_RecordCheckDuration(b *testing.B) {
	metrics := NewPrometheusMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.RecordCheckDuration("ip", 2*time.Millisecond)
	}
}

// BenchmarkPrometheusMetrics_SetActiveKeys benchmarks gauge updates.
func BenchmarkPrometheusMetrics_SetActiveKeys(b *testing.B) {
	metrics := NewPrometheusMetrics()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		metrics.SetActiveKeys("ip", i%10000)
	}
}

// BenchmarkPrometheusMetrics_ConcurrentWrites benchmarks concurrent metric writes.
//
// Prometheus client is thread-safe, so this tests contention.
func BenchmarkPrometheusMetrics_ConcurrentWrites(b *testing.B) {
	metrics := NewPrometheusMetrics()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			metrics.RecordRequest("ip", "/api/articles")
		}
	})
}

// BenchmarkFullRateLimitCheck benchmarks the complete rate limit flow.
//
// This includes:
// - Circuit breaker check
// - Algorithm IsAllowed check
// - Store operations
// - Metric recording
//
// This represents the actual overhead added to each HTTP request.
// Target: <5ms p99 latency
func BenchmarkFullRateLimitCheck(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	algo := NewSlidingWindowAlgorithm(&SystemClock{})
	metrics := NewPrometheusMetrics()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 10,
		RecoveryTimeout:  30 * time.Second,
		Clock:            &SystemClock{},
		Metrics:          metrics,
		LimiterType:      "ip",
	})

	ctx := context.Background()
	limit := 100
	window := time.Minute

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("ip:%d", i%1000)

		// Full rate limit check
		cb.Execute(func() error {
			start := time.Now()

			decision, err := algo.IsAllowed(ctx, key, store, limit, window)
			if err != nil {
				return err
			}

			duration := time.Since(start)
			metrics.RecordCheckDuration("ip", duration)

			if decision.Allowed {
				metrics.RecordRequest("ip", "/api/articles")
			} else {
				metrics.RecordDenied("ip", "/api/articles")
			}

			return nil
		})
	}
}

// BenchmarkFullRateLimitCheck_Concurrent benchmarks the full flow with concurrency.
//
// This simulates real-world HTTP request handling.
// Target: Linear scaling with CPU cores, no lock contention
func BenchmarkFullRateLimitCheck_Concurrent(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	algo := NewSlidingWindowAlgorithm(&SystemClock{})
	metrics := NewPrometheusMetrics()
	cb := NewCircuitBreaker(CircuitBreakerConfig{
		FailureThreshold: 10,
		RecoveryTimeout:  30 * time.Second,
		Clock:            &SystemClock{},
		Metrics:          metrics,
		LimiterType:      "ip",
	})

	ctx := context.Background()
	limit := 100
	window := time.Minute

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("ip:%d", i%1000)

			cb.Execute(func() error {
				start := time.Now()

				decision, err := algo.IsAllowed(ctx, key, store, limit, window)
				if err != nil {
					return err
				}

				duration := time.Since(start)
				metrics.RecordCheckDuration("ip", duration)

				if decision.Allowed {
					metrics.RecordRequest("ip", "/api/articles")
				} else {
					metrics.RecordDenied("ip", "/api/articles")
				}

				return nil
			})

			i++
		}
	})
}

// BenchmarkMemoryPerKey measures memory usage per key.
//
// This helps verify that we meet the <1KB per key target.
func BenchmarkMemoryPerKey(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	ctx := context.Background()

	// Add 100 requests per key (typical for 100 req/min limit)
	numKeys := 1000
	requestsPerKey := 100

	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("ip:%d", i)
		for j := 0; j < requestsPerKey; j++ {
			store.AddRequest(ctx, key, time.Now().Add(-time.Duration(j)*time.Second))
		}
	}

	// Measure memory
	memUsage, _ := store.MemoryUsage(ctx)
	avgPerKey := memUsage / int64(numKeys)

	b.ReportMetric(float64(avgPerKey), "bytes/key")
	b.ReportMetric(float64(memUsage)/(1024*1024), "total_MB")
}

// BenchmarkStoreThroughput measures maximum throughput.
//
// This tests how many rate limit checks per second the system can handle.
func BenchmarkStoreThroughput(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	algo := NewSlidingWindowAlgorithm(&SystemClock{})
	ctx := context.Background()

	limit := 100
	window := time.Minute

	var wg sync.WaitGroup
	numWorkers := 10
	requestsPerWorker := b.N / numWorkers

	start := time.Now()

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < requestsPerWorker; j++ {
				key := fmt.Sprintf("ip:%d", (workerID*requestsPerWorker+j)%1000)
				algo.IsAllowed(ctx, key, store, limit, window)
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	throughput := float64(b.N) / elapsed.Seconds()
	b.ReportMetric(throughput, "requests/sec")
}

// BenchmarkCleanupWithDifferentSizes benchmarks cleanup with various store sizes.
func BenchmarkCleanupWithDifferentSizes(b *testing.B) {
	sizes := []int{100, 1000, 10000}
	ctx := context.Background()

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
					MaxKeys: size * 2,
					Clock:   &SystemClock{},
				})

				// Populate
				now := time.Now()
				for j := 0; j < size; j++ {
					key := fmt.Sprintf("ip:%d", j)
					store.AddRequest(ctx, key, now.Add(-2*time.Hour))
					store.AddRequest(ctx, key, now.Add(-30*time.Minute))
				}
				b.StartTimer()

				store.Cleanup(ctx, now.Add(-1*time.Hour))
			}
		})
	}
}

// BenchmarkConcurrentReadWrite benchmarks concurrent reads and writes.
//
// This simulates real-world scenario where rate limit checks (reads) and
// request recording (writes) happen concurrently.
func BenchmarkConcurrentReadWrite(b *testing.B) {
	store := NewInMemoryRateLimitStore(InMemoryStoreConfig{
		MaxKeys: 10000,
		Clock:   &SystemClock{},
	})
	ctx := context.Background()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		key := fmt.Sprintf("ip:%d", i)
		for j := 0; j < 50; j++ {
			store.AddRequest(ctx, key, time.Now().Add(-time.Duration(j)*time.Second))
		}
	}

	cutoff := time.Now().Add(-1 * time.Minute)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := fmt.Sprintf("ip:%d", i%1000)
			if i%2 == 0 {
				// Read operation
				store.GetRequestCount(ctx, key, cutoff)
			} else {
				// Write operation
				store.AddRequest(ctx, key, time.Now())
			}
			i++
		}
	})
}
