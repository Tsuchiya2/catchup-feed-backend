package grpc

import (
	"context"
	"testing"

	"catchup-feed/internal/usecase/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewNoopAIProvider(t *testing.T) {
	provider := NewNoopAIProvider()

	assert.NotNil(t, provider)
}

func TestNoopAIProvider_EmbedArticle(t *testing.T) {
	provider := NewNoopAIProvider()
	ctx := context.Background()

	req := ai.EmbedRequest{
		ArticleID: 1,
		Title:     "Test Article",
		Content:   "Test content",
		URL:       "https://example.com",
	}

	resp, err := provider.EmbedArticle(ctx, req)

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, ai.ErrAIDisabled)
}

func TestNoopAIProvider_SearchSimilar(t *testing.T) {
	provider := NewNoopAIProvider()
	ctx := context.Background()

	req := ai.SearchRequest{
		Query: "test query",
		Limit: 10,
	}

	resp, err := provider.SearchSimilar(ctx, req)

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, ai.ErrAIDisabled)
}

func TestNoopAIProvider_QueryArticles(t *testing.T) {
	provider := NewNoopAIProvider()
	ctx := context.Background()

	req := ai.QueryRequest{
		Question:   "What is AI?",
		MaxContext: 5,
	}

	resp, err := provider.QueryArticles(ctx, req)

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, ai.ErrAIDisabled)
}

func TestNoopAIProvider_GenerateSummary(t *testing.T) {
	provider := NewNoopAIProvider()
	ctx := context.Background()

	req := ai.SummaryRequest{
		Period:        ai.SummaryPeriodWeek,
		MaxHighlights: 5,
	}

	resp, err := provider.GenerateSummary(ctx, req)

	assert.Nil(t, resp)
	assert.ErrorIs(t, err, ai.ErrAIDisabled)
}

func TestNoopAIProvider_Health(t *testing.T) {
	provider := NewNoopAIProvider()
	ctx := context.Background()

	status, err := provider.Health(ctx)

	require.NoError(t, err)
	assert.NotNil(t, status)
	assert.False(t, status.Healthy)
	assert.Equal(t, "AI features are disabled", status.Message)
	assert.False(t, status.CircuitOpen)
	assert.Zero(t, status.Latency)
}

func TestNoopAIProvider_Close(t *testing.T) {
	provider := NewNoopAIProvider()

	err := provider.Close()

	assert.NoError(t, err)
}

func TestNoopAIProvider_ImplementsInterface(t *testing.T) {
	// Verify NoopAIProvider implements AIProvider interface
	var _ ai.AIProvider = (*NoopAIProvider)(nil)
}
