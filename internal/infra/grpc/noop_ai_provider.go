package grpc

import (
	"context"

	"catchup-feed/internal/usecase/ai"
)

// NoopAIProvider is a no-op implementation of AIProvider.
// Used for testing and when AI features are disabled.
type NoopAIProvider struct{}

// NewNoopAIProvider creates a new no-op AI provider.
func NewNoopAIProvider() *NoopAIProvider {
	return &NoopAIProvider{}
}

// EmbedArticle returns an error indicating AI is disabled.
func (p *NoopAIProvider) EmbedArticle(ctx context.Context, req ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, ai.ErrAIDisabled
}

// SearchSimilar returns an error indicating AI is disabled.
func (p *NoopAIProvider) SearchSimilar(ctx context.Context, req ai.SearchRequest) (*ai.SearchResponse, error) {
	return nil, ai.ErrAIDisabled
}

// QueryArticles returns an error indicating AI is disabled.
func (p *NoopAIProvider) QueryArticles(ctx context.Context, req ai.QueryRequest) (*ai.QueryResponse, error) {
	return nil, ai.ErrAIDisabled
}

// GenerateSummary returns an error indicating AI is disabled.
func (p *NoopAIProvider) GenerateSummary(ctx context.Context, req ai.SummaryRequest) (*ai.SummaryResponse, error) {
	return nil, ai.ErrAIDisabled
}

// Health returns unhealthy status with descriptive message.
func (p *NoopAIProvider) Health(ctx context.Context) (*ai.HealthStatus, error) {
	return &ai.HealthStatus{
		Healthy:     false,
		Latency:     0,
		Message:     "AI features are disabled",
		CircuitOpen: false,
	}, nil
}

// Close is a no-op for the noop provider.
func (p *NoopAIProvider) Close() error {
	return nil
}
