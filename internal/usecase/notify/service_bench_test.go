package notify

import (
	"context"
	"sync"
	"testing"

	"catchup-feed/internal/domain/entity"
)

// BenchmarkNotifyNewArticle_SingleChannel measures throughput of single notification to one channel
func BenchmarkNotifyNewArticle_SingleChannel(b *testing.B) {
	// Setup - fast mock channel with no delay
	channel := &mockChannel{
		name:    "discord",
		enabled: true,
	}
	svc := NewService([]Channel{channel}, 10)

	article := &entity.Article{
		ID:    1,
		Title: "Benchmark Article",
		URL:   "https://example.com/article",
	}
	source := &entity.Source{
		ID:   1,
		Name: "Benchmark Source",
	}
	ctx := context.Background()

	// Enable allocation reporting
	b.ReportAllocs()

	// Reset timer before benchmark loop
	b.ResetTimer()

	// Run benchmark
	for i := 0; i < b.N; i++ {
		_ = svc.NotifyNewArticle(ctx, article, source)
	}

	// Stop timer before cleanup
	b.StopTimer()

	// Wait for all goroutines to complete
	shutdownCtx := context.Background()
	_ = svc.Shutdown(shutdownCtx)
}

// BenchmarkNotifyNewArticle_MultipleChannels measures throughput with 3 channels enabled
func BenchmarkNotifyNewArticle_MultipleChannels(b *testing.B) {
	// Setup - 3 fast mock channels
	channels := []Channel{
		&mockChannel{name: "discord", enabled: true},
		&mockChannel{name: "slack", enabled: true},
		&mockChannel{name: "email", enabled: true},
	}
	svc := NewService(channels, 10)

	article := &entity.Article{
		ID:    1,
		Title: "Benchmark Article",
		URL:   "https://example.com/article",
	}
	source := &entity.Source{
		ID:   1,
		Name: "Benchmark Source",
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = svc.NotifyNewArticle(ctx, article, source)
	}

	b.StopTimer()
	shutdownCtx := context.Background()
	_ = svc.Shutdown(shutdownCtx)
}

// BenchmarkCircuitBreakerCheck measures circuit breaker check overhead
func BenchmarkCircuitBreakerCheck(b *testing.B) {
	// Setup service with one channel
	channel := &mockChannel{name: "discord", enabled: true}
	svc := NewService([]Channel{channel}, 10)

	b.ReportAllocs()

	b.Run("CircuitClosed", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Access circuit breaker state (simulates check in notifyChannel)
			_ = svc.GetChannelHealth()
		}
	})

	b.Run("CircuitOpen", func(b *testing.B) {
		// Trigger circuit breaker to open
		implSvc := svc.(*service)
		health := implSvc.getChannelHealth("discord")
		health.mu.Lock()
		health.consecutiveFailures = circuitBreakerThreshold
		health.mu.Unlock()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = svc.GetChannelHealth()
		}
	})
}

// BenchmarkWorkerPoolAcquisition measures time to acquire worker pool slot
func BenchmarkWorkerPoolAcquisition(b *testing.B) {
	// Setup service with larger worker pool
	channel := &mockChannel{name: "discord", enabled: true}
	svc := NewService([]Channel{channel}, 100)

	article := &entity.Article{
		ID:    1,
		Title: "Benchmark Article",
		URL:   "https://example.com/article",
	}
	source := &entity.Source{
		ID:   1,
		Name: "Benchmark Source",
	}
	ctx := context.Background()

	b.ReportAllocs()

	b.Run("PoolEmpty", func(b *testing.B) {
		// Pool is empty - immediate acquisition
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = svc.NotifyNewArticle(ctx, article, source)
		}

		b.StopTimer()
		shutdownCtx := context.Background()
		_ = svc.Shutdown(shutdownCtx)
	})

	b.Run("Pool50PercentFull", func(b *testing.B) {
		// Setup - new service for this sub-benchmark
		svc2 := NewService([]Channel{channel}, 10)

		// Fill 50% of pool (5 out of 10 slots)
		implSvc := svc2.(*service)
		for i := 0; i < 5; i++ {
			implSvc.workerPool <- struct{}{}
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = svc2.NotifyNewArticle(ctx, article, source)
		}

		b.StopTimer()

		// Release pool slots
		for i := 0; i < 5; i++ {
			<-implSvc.workerPool
		}

		shutdownCtx := context.Background()
		_ = svc2.Shutdown(shutdownCtx)
	})
}

// BenchmarkNotifyNewArticle_100Concurrent measures stress test with 100 concurrent notifications
func BenchmarkNotifyNewArticle_100Concurrent(b *testing.B) {
	// Setup service
	channel := &mockChannel{name: "discord", enabled: true}
	svc := NewService([]Channel{channel}, 50) // Large worker pool for concurrency

	article := &entity.Article{
		ID:    1,
		Title: "Benchmark Article",
		URL:   "https://example.com/article",
	}
	source := &entity.Source{
		ID:   1,
		Name: "Benchmark Source",
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		numConcurrent := 100

		wg.Add(numConcurrent)
		for j := 0; j < numConcurrent; j++ {
			go func() {
				defer wg.Done()
				_ = svc.NotifyNewArticle(ctx, article, source)
			}()
		}

		wg.Wait()
	}

	b.StopTimer()
	shutdownCtx := context.Background()
	_ = svc.Shutdown(shutdownCtx)
}

// BenchmarkGetChannelHealth measures health status retrieval overhead
func BenchmarkGetChannelHealth(b *testing.B) {
	// Setup service with 3 channels
	channels := []Channel{
		&mockChannel{name: "discord", enabled: true},
		&mockChannel{name: "slack", enabled: true},
		&mockChannel{name: "email", enabled: false},
	}
	svc := NewService(channels, 10)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = svc.GetChannelHealth()
	}
}

// BenchmarkNotifyNewArticle_Parallel measures parallel notification throughput
func BenchmarkNotifyNewArticle_Parallel(b *testing.B) {
	// Setup service
	channel := &mockChannel{name: "discord", enabled: true}
	svc := NewService([]Channel{channel}, 50)

	article := &entity.Article{
		ID:    1,
		Title: "Benchmark Article",
		URL:   "https://example.com/article",
	}
	source := &entity.Source{
		ID:   1,
		Name: "Benchmark Source",
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = svc.NotifyNewArticle(ctx, article, source)
		}
	})

	b.StopTimer()
	shutdownCtx := context.Background()
	_ = svc.Shutdown(shutdownCtx)
}

// BenchmarkNotifyChannel_WithCircuitBreaker measures overhead of circuit breaker in notifyChannel
func BenchmarkNotifyChannel_WithCircuitBreaker(b *testing.B) {
	// Setup service
	channel := &mockChannel{name: "discord", enabled: true}
	svc := NewService([]Channel{channel}, 100)

	article := &entity.Article{
		ID:    1,
		Title: "Benchmark Article",
		URL:   "https://example.com/article",
	}
	source := &entity.Source{
		ID:   1,
		Name: "Benchmark Source",
	}

	implSvc := svc.(*service)

	b.ReportAllocs()

	b.Run("CircuitClosed", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// Directly call notifyChannel to measure its performance
			implSvc.wg.Add(1)
			implSvc.notifyChannel("bench-request-id", channel, article, source)
		}

		b.StopTimer()
		shutdownCtx := context.Background()
		_ = svc.Shutdown(shutdownCtx)
	})
}

// BenchmarkMemoryAllocation_NotifyNewArticle measures memory allocations per notification
func BenchmarkMemoryAllocation_NotifyNewArticle(b *testing.B) {
	channel := &mockChannel{name: "discord", enabled: true}
	svc := NewService([]Channel{channel}, 10)

	article := &entity.Article{
		ID:    1,
		Title: "Benchmark Article",
		URL:   "https://example.com/article",
	}
	source := &entity.Source{
		ID:   1,
		Name: "Benchmark Source",
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = svc.NotifyNewArticle(ctx, article, source)
	}

	b.StopTimer()
	shutdownCtx := context.Background()
	_ = svc.Shutdown(shutdownCtx)
}
