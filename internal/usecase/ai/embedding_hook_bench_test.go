package ai_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/usecase/ai"
)

// mockEmbedProvider is a mock for embedding benchmarks.
type mockEmbedProvider struct {
	delay time.Duration
}

func (m *mockEmbedProvider) EmbedArticle(ctx context.Context, req ai.EmbedRequest) (*ai.EmbedResponse, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return &ai.EmbedResponse{Success: true, Dimension: 768}, nil
}

func (m *mockEmbedProvider) SearchSimilar(ctx context.Context, req ai.SearchRequest) (*ai.SearchResponse, error) {
	return &ai.SearchResponse{Articles: []ai.SimilarArticle{}}, nil
}

func (m *mockEmbedProvider) QueryArticles(ctx context.Context, req ai.QueryRequest) (*ai.QueryResponse, error) {
	return &ai.QueryResponse{Answer: "test"}, nil
}

func (m *mockEmbedProvider) GenerateSummary(ctx context.Context, req ai.SummaryRequest) (*ai.SummaryResponse, error) {
	return &ai.SummaryResponse{Summary: "test"}, nil
}

func (m *mockEmbedProvider) Health(ctx context.Context) (*ai.HealthStatus, error) {
	return &ai.HealthStatus{Healthy: true}, nil
}

func (m *mockEmbedProvider) Close() error {
	return nil
}

// BenchmarkEmbeddingHook_EmbedArticleAsync_Dispatch measures goroutine dispatch overhead.
func BenchmarkEmbeddingHook_EmbedArticleAsync_Dispatch(b *testing.B) {
	provider := &mockEmbedProvider{delay: 0}
	hook := ai.NewEmbeddingHook(provider, true)
	ctx := context.Background()

	article := &entity.Article{
		ID:      1,
		Title:   "Benchmark Article",
		Summary: "This is the content for benchmarking embedding dispatch.",
		URL:     "https://example.com/benchmark",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hook.EmbedArticleAsync(ctx, article)
	}

	// Wait a bit for goroutines to complete
	time.Sleep(10 * time.Millisecond)
}

// BenchmarkEmbeddingHook_EmbedArticleAsync_WithDelay measures with simulated network delay.
func BenchmarkEmbeddingHook_EmbedArticleAsync_WithDelay(b *testing.B) {
	provider := &mockEmbedProvider{delay: 1 * time.Millisecond}
	hook := ai.NewEmbeddingHook(provider, true)
	ctx := context.Background()

	article := &entity.Article{
		ID:      1,
		Title:   "Benchmark Article",
		Summary: "This is the content for benchmarking embedding with delay.",
		URL:     "https://example.com/benchmark",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hook.EmbedArticleAsync(ctx, article)
	}

	// Wait for goroutines to complete
	time.Sleep(100 * time.Millisecond)
}

// BenchmarkEmbeddingHook_EmbedArticleAsync_Concurrent measures concurrent embedding dispatches.
func BenchmarkEmbeddingHook_EmbedArticleAsync_Concurrent(b *testing.B) {
	provider := &mockEmbedProvider{delay: 0}
	hook := ai.NewEmbeddingHook(provider, true)

	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		id := int64(0)
		for pb.Next() {
			article := &entity.Article{
				ID:      id,
				Title:   "Concurrent Benchmark Article",
				Summary: "Content for concurrent embedding benchmark.",
				URL:     "https://example.com/concurrent",
			}
			hook.EmbedArticleAsync(ctx, article)
			id++
		}
	})

	// Wait for goroutines to complete
	time.Sleep(50 * time.Millisecond)
}

// BenchmarkEmbeddingHook_EmbedArticleAsync_BatchSimulation simulates batch article processing.
func BenchmarkEmbeddingHook_EmbedArticleAsync_BatchSimulation(b *testing.B) {
	provider := &mockEmbedProvider{delay: 0}
	hook := ai.NewEmbeddingHook(provider, true)
	ctx := context.Background()

	// Create batch of 100 articles
	articles := make([]*entity.Article, 100)
	for i := range articles {
		articles[i] = &entity.Article{
			ID:      int64(i),
			Title:   "Batch Article",
			Summary: "Content for batch embedding benchmark.",
			URL:     "https://example.com/batch",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, article := range articles {
			hook.EmbedArticleAsync(ctx, article)
		}
	}

	// Wait for goroutines to complete
	time.Sleep(100 * time.Millisecond)
}

// BenchmarkEmbeddingHook_Disabled measures overhead when AI is disabled.
func BenchmarkEmbeddingHook_Disabled(b *testing.B) {
	provider := &mockEmbedProvider{delay: 0}
	hook := ai.NewEmbeddingHook(provider, false) // AI disabled
	ctx := context.Background()

	article := &entity.Article{
		ID:      1,
		Title:   "Benchmark Article",
		Summary: "Content for disabled AI benchmark.",
		URL:     "https://example.com/benchmark",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hook.EmbedArticleAsync(ctx, article)
	}
}

// BenchmarkEmbeddingHook_NilArticle measures overhead with nil article.
func BenchmarkEmbeddingHook_NilArticle(b *testing.B) {
	provider := &mockEmbedProvider{delay: 0}
	hook := ai.NewEmbeddingHook(provider, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hook.EmbedArticleAsync(ctx, nil)
	}
}

// BenchmarkEmbeddingHook_WithContextRequestID measures with request ID context.
func BenchmarkEmbeddingHook_WithContextRequestID(b *testing.B) {
	provider := &mockEmbedProvider{delay: 0}
	hook := ai.NewEmbeddingHook(provider, true)
	//nolint:staticcheck // SA1029: intentionally using string key to match production code
	ctx := context.WithValue(context.Background(), "request_id", "bench-req-123")

	article := &entity.Article{
		ID:      1,
		Title:   "Benchmark Article",
		Summary: "Content for context benchmark.",
		URL:     "https://example.com/benchmark",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hook.EmbedArticleAsync(ctx, article)
	}

	// Wait for goroutines to complete
	time.Sleep(10 * time.Millisecond)
}

// BenchmarkEmbeddingHook_HighConcurrency measures high concurrency scenario.
func BenchmarkEmbeddingHook_HighConcurrency(b *testing.B) {
	provider := &mockEmbedProvider{delay: 0}
	hook := ai.NewEmbeddingHook(provider, true)
	ctx := context.Background()

	// Simulate high concurrency with wait group
	var wg sync.WaitGroup

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Spawn 50 concurrent embeddings
		for j := 0; j < 50; j++ {
			wg.Add(1)
			go func(id int64) {
				defer wg.Done()
				article := &entity.Article{
					ID:      id,
					Title:   "High Concurrency Article",
					Summary: "Content for high concurrency benchmark.",
					URL:     "https://example.com/concurrent",
				}
				hook.EmbedArticleAsync(ctx, article)
			}(int64(j))
		}
		wg.Wait()
	}

	// Wait for internal goroutines to complete
	time.Sleep(50 * time.Millisecond)
}
