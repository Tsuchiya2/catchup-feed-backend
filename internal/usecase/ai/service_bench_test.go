package ai_test

import (
	"context"
	"testing"
	"time"

	"catchup-feed/internal/usecase/ai"
)

// mockBenchProvider is a fast mock for benchmarking.
type mockBenchProvider struct{}

func (m *mockBenchProvider) EmbedArticle(ctx context.Context, req ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return &ai.EmbedResponse{Success: true, Dimension: 768}, nil
}

func (m *mockBenchProvider) SearchSimilar(ctx context.Context, req ai.SearchRequest) (*ai.SearchResponse, error) {
	articles := make([]ai.SimilarArticle, req.Limit)
	for i := int32(0); i < req.Limit; i++ {
		articles[i] = ai.SimilarArticle{
			ArticleID:  int64(i + 1),
			Title:      "Benchmark Article",
			Similarity: 0.9 - float32(i)*0.01,
		}
	}
	return &ai.SearchResponse{Articles: articles, TotalSearched: 1000}, nil
}

func (m *mockBenchProvider) QueryArticles(ctx context.Context, req ai.QueryRequest) (*ai.QueryResponse, error) {
	sources := make([]ai.SourceArticle, req.MaxContext)
	for i := int32(0); i < req.MaxContext; i++ {
		sources[i] = ai.SourceArticle{
			ArticleID: int64(i + 1),
			Title:     "Source Article",
			Relevance: 0.95 - float32(i)*0.05,
		}
	}
	return &ai.QueryResponse{
		Answer:     "This is a benchmark answer for the question.",
		Sources:    sources,
		Confidence: 0.85,
	}, nil
}

func (m *mockBenchProvider) GenerateSummary(ctx context.Context, req ai.SummaryRequest) (*ai.SummaryResponse, error) {
	highlights := make([]ai.Highlight, req.MaxHighlights)
	for i := int32(0); i < req.MaxHighlights; i++ {
		highlights[i] = ai.Highlight{
			Topic:        "Topic",
			Description:  "Highlight description",
			ArticleCount: int32(i+1) * 5,
		}
	}
	return &ai.SummaryResponse{
		Summary:      "This is a benchmark summary of articles.",
		StartDate:    "2024-01-01",
		EndDate:      "2024-01-07",
		ArticleCount: 100,
		Highlights:   highlights,
	}, nil
}

func (m *mockBenchProvider) Health(ctx context.Context) (*ai.HealthStatus, error) {
	return &ai.HealthStatus{
		Healthy: true,
		Latency: 5 * time.Millisecond,
	}, nil
}

func (m *mockBenchProvider) Close() error {
	return nil
}

// BenchmarkService_Search measures search operation performance.
func BenchmarkService_Search(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.Search(ctx, "benchmark query", 10, 0.7)
	}
}

// BenchmarkService_Search_LargeResults measures search with larger result sets.
func BenchmarkService_Search_LargeResults(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.Search(ctx, "benchmark query with more keywords", 100, 0.5)
	}
}

// BenchmarkService_Ask measures RAG query performance.
func BenchmarkService_Ask(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.Ask(ctx, "What are the main trends in technology?", 5)
	}
}

// BenchmarkService_Ask_LargeContext measures RAG with larger context.
func BenchmarkService_Ask_LargeContext(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.Ask(ctx, "Provide a detailed analysis of recent developments", 20)
	}
}

// BenchmarkService_Summarize_Weekly measures weekly summary generation.
func BenchmarkService_Summarize_Weekly(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.Summarize(ctx, ai.SummaryPeriodWeek, 5)
	}
}

// BenchmarkService_Summarize_Monthly measures monthly summary generation.
func BenchmarkService_Summarize_Monthly(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.Summarize(ctx, ai.SummaryPeriodMonth, 10)
	}
}

// BenchmarkService_Health measures health check performance.
func BenchmarkService_Health(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.Health(ctx)
	}
}

// BenchmarkService_Search_WithContextValues measures search with context values.
func BenchmarkService_Search_WithContextValues(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)
	//nolint:staticcheck // SA1029: intentionally using string key to match production code
	ctx := context.WithValue(context.Background(), "request_id", "bench-request-123")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = service.Search(ctx, "benchmark query", 10, 0.7)
	}
}

// BenchmarkService_Parallel_Search measures concurrent search operations.
func BenchmarkService_Parallel_Search(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)

	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_, _ = service.Search(ctx, "concurrent benchmark query", 10, 0.7)
		}
	})
}

// BenchmarkService_Parallel_Ask measures concurrent RAG operations.
func BenchmarkService_Parallel_Ask(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)

	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			_, _ = service.Ask(ctx, "concurrent question about trends?", 5)
		}
	})
}

// BenchmarkService_Parallel_Mixed measures mixed concurrent operations.
func BenchmarkService_Parallel_Mixed(b *testing.B) {
	provider := &mockBenchProvider{}
	service := ai.NewService(provider, true)

	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		i := 0
		for pb.Next() {
			switch i % 3 {
			case 0:
				_, _ = service.Search(ctx, "search query", 10, 0.7)
			case 1:
				_, _ = service.Ask(ctx, "question about topic?", 5)
			case 2:
				_, _ = service.Summarize(ctx, ai.SummaryPeriodWeek, 5)
			}
			i++
		}
	})
}
