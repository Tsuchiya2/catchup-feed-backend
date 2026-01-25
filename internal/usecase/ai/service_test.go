package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockAIProvider implements AIProvider for testing.
type MockAIProvider struct {
	embedFn   func(ctx context.Context, req EmbedRequest) (*EmbedResponse, error)
	searchFn  func(ctx context.Context, req SearchRequest) (*SearchResponse, error)
	queryFn   func(ctx context.Context, req QueryRequest) (*QueryResponse, error)
	summaryFn func(ctx context.Context, req SummaryRequest) (*SummaryResponse, error)
	healthFn  func(ctx context.Context) (*HealthStatus, error)
	closeFn   func() error
}

func (m *MockAIProvider) EmbedArticle(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
	if m.embedFn != nil {
		return m.embedFn(ctx, req)
	}
	return &EmbedResponse{Success: true, Dimension: 768}, nil
}

func (m *MockAIProvider) SearchSimilar(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, req)
	}
	return &SearchResponse{Articles: []SimilarArticle{}}, nil
}

func (m *MockAIProvider) QueryArticles(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, req)
	}
	return &QueryResponse{Answer: "test answer", Sources: []SourceArticle{}}, nil
}

func (m *MockAIProvider) GenerateSummary(ctx context.Context, req SummaryRequest) (*SummaryResponse, error) {
	if m.summaryFn != nil {
		return m.summaryFn(ctx, req)
	}
	return &SummaryResponse{Summary: "test summary", Highlights: []Highlight{}}, nil
}

func (m *MockAIProvider) Health(ctx context.Context) (*HealthStatus, error) {
	if m.healthFn != nil {
		return m.healthFn(ctx)
	}
	return &HealthStatus{Healthy: true, Latency: 10 * time.Millisecond}, nil
}

func (m *MockAIProvider) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

func TestNewService(t *testing.T) {
	provider := &MockAIProvider{}
	service := NewService(provider, true)

	assert.NotNil(t, service)
	assert.True(t, service.aiEnabled)
}

func TestNewService_AIDisabled(t *testing.T) {
	provider := &MockAIProvider{}
	service := NewService(provider, false)

	assert.NotNil(t, service)
	assert.False(t, service.aiEnabled)
}

func TestService_Search_Success(t *testing.T) {
	expectedArticles := []SimilarArticle{
		{ArticleID: 1, Title: "Article 1", Similarity: 0.95},
		{ArticleID: 2, Title: "Article 2", Similarity: 0.90},
	}

	provider := &MockAIProvider{
		searchFn: func(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
			assert.Equal(t, "test query", req.Query)
			assert.Equal(t, int32(10), req.Limit)
			assert.Equal(t, float32(0.7), req.MinSimilarity)
			return &SearchResponse{Articles: expectedArticles, TotalSearched: 100}, nil
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	resp, err := service.Search(ctx, "test query", 10, 0.7)

	require.NoError(t, err)
	assert.Equal(t, expectedArticles, resp.Articles)
	assert.Equal(t, int64(100), resp.TotalSearched)
}

func TestService_Search_AIDisabled(t *testing.T) {
	provider := &MockAIProvider{}
	service := NewService(provider, false)
	ctx := context.Background()

	resp, err := service.Search(ctx, "test query", 10, 0.7)

	assert.ErrorIs(t, err, ErrAIDisabled)
	assert.Nil(t, resp)
}

func TestService_Search_EmptyQuery(t *testing.T) {
	provider := &MockAIProvider{}
	service := NewService(provider, true)
	ctx := context.Background()

	resp, err := service.Search(ctx, "", 10, 0.7)

	assert.ErrorIs(t, err, ErrInvalidQuery)
	assert.Nil(t, resp)
}

func TestService_Search_DefaultValues(t *testing.T) {
	provider := &MockAIProvider{
		searchFn: func(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
			// When limit <= 0, default is 10
			assert.Equal(t, int32(10), req.Limit)
			// When minSimilarity <= 0, default is 0.7
			assert.Equal(t, float32(0.7), req.MinSimilarity)
			return &SearchResponse{Articles: []SimilarArticle{}}, nil
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	resp, err := service.Search(ctx, "test query", 0, 0)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestService_Search_ProviderError(t *testing.T) {
	expectedErr := errors.New("provider error")
	provider := &MockAIProvider{
		searchFn: func(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
			return nil, expectedErr
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	resp, err := service.Search(ctx, "test query", 10, 0.7)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI search failed")
	assert.Nil(t, resp)
}

func TestService_Ask_Success(t *testing.T) {
	expectedResponse := &QueryResponse{
		Answer: "This is the answer",
		Sources: []SourceArticle{
			{ArticleID: 1, Title: "Source 1", Relevance: 0.95},
		},
		Confidence: 0.85,
	}

	provider := &MockAIProvider{
		queryFn: func(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
			assert.Equal(t, "What is AI?", req.Question)
			assert.Equal(t, int32(5), req.MaxContext)
			return expectedResponse, nil
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	response, err := service.Ask(ctx, "What is AI?", 5)

	require.NoError(t, err)
	assert.Equal(t, expectedResponse, response)
}

func TestService_Ask_AIDisabled(t *testing.T) {
	provider := &MockAIProvider{}
	service := NewService(provider, false)
	ctx := context.Background()

	response, err := service.Ask(ctx, "test query", 5)

	assert.ErrorIs(t, err, ErrAIDisabled)
	assert.Nil(t, response)
}

func TestService_Ask_EmptyQuestion(t *testing.T) {
	provider := &MockAIProvider{}
	service := NewService(provider, true)
	ctx := context.Background()

	response, err := service.Ask(ctx, "", 5)

	assert.ErrorIs(t, err, ErrInvalidQuestion)
	assert.Nil(t, response)
}

func TestService_Ask_DefaultMaxContext(t *testing.T) {
	provider := &MockAIProvider{
		queryFn: func(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
			// When maxContext <= 0, default is 5
			assert.Equal(t, int32(5), req.MaxContext)
			return &QueryResponse{Answer: "test"}, nil
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	response, err := service.Ask(ctx, "test question", 0)

	require.NoError(t, err)
	assert.NotNil(t, response)
}

func TestService_Ask_ProviderError(t *testing.T) {
	expectedErr := errors.New("provider error")
	provider := &MockAIProvider{
		queryFn: func(ctx context.Context, req QueryRequest) (*QueryResponse, error) {
			return nil, expectedErr
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	response, err := service.Ask(ctx, "test question", 5)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI ask failed")
	assert.Nil(t, response)
}

func TestService_Summarize_Success(t *testing.T) {
	expectedResponse := &SummaryResponse{
		Summary:      "Weekly summary",
		StartDate:    "2024-01-15",
		EndDate:      "2024-01-21",
		ArticleCount: 50,
		Highlights: []Highlight{
			{Topic: "AI", Description: "AI developments", ArticleCount: 10},
		},
	}

	provider := &MockAIProvider{
		summaryFn: func(ctx context.Context, req SummaryRequest) (*SummaryResponse, error) {
			assert.Equal(t, SummaryPeriodWeek, req.Period)
			assert.Equal(t, int32(5), req.MaxHighlights)
			return expectedResponse, nil
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	response, err := service.Summarize(ctx, SummaryPeriodWeek, 5)

	require.NoError(t, err)
	assert.Equal(t, expectedResponse, response)
}

func TestService_Summarize_AIDisabled(t *testing.T) {
	provider := &MockAIProvider{}
	service := NewService(provider, false)
	ctx := context.Background()

	response, err := service.Summarize(ctx, SummaryPeriodWeek, 5)

	assert.ErrorIs(t, err, ErrAIDisabled)
	assert.Nil(t, response)
}

func TestService_Summarize_MonthPeriod(t *testing.T) {
	provider := &MockAIProvider{
		summaryFn: func(ctx context.Context, req SummaryRequest) (*SummaryResponse, error) {
			assert.Equal(t, SummaryPeriodMonth, req.Period)
			return &SummaryResponse{Summary: "Monthly summary"}, nil
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	response, err := service.Summarize(ctx, SummaryPeriodMonth, 5)

	require.NoError(t, err)
	assert.Equal(t, "Monthly summary", response.Summary)
}

func TestService_Summarize_InvalidPeriod(t *testing.T) {
	provider := &MockAIProvider{}
	service := NewService(provider, true)
	ctx := context.Background()

	// Use unspecified period
	response, err := service.Summarize(ctx, SummaryPeriodUnspecified, 5)

	assert.ErrorIs(t, err, ErrInvalidPeriod)
	assert.Nil(t, response)
}

func TestService_Summarize_DefaultMaxHighlights(t *testing.T) {
	provider := &MockAIProvider{
		summaryFn: func(ctx context.Context, req SummaryRequest) (*SummaryResponse, error) {
			// When maxHighlights <= 0, default is 5
			assert.Equal(t, int32(5), req.MaxHighlights)
			return &SummaryResponse{Summary: "test"}, nil
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	response, err := service.Summarize(ctx, SummaryPeriodWeek, 0)

	require.NoError(t, err)
	assert.NotNil(t, response)
}

func TestService_Summarize_ProviderError(t *testing.T) {
	expectedErr := errors.New("provider error")
	provider := &MockAIProvider{
		summaryFn: func(ctx context.Context, req SummaryRequest) (*SummaryResponse, error) {
			return nil, expectedErr
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	response, err := service.Summarize(ctx, SummaryPeriodWeek, 5)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI summarize failed")
	assert.Nil(t, response)
}

func TestService_Health_Success(t *testing.T) {
	expectedStatus := &HealthStatus{
		Healthy: true,
		Latency: 10 * time.Millisecond,
		Message: "",
	}

	provider := &MockAIProvider{
		healthFn: func(ctx context.Context) (*HealthStatus, error) {
			return expectedStatus, nil
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	status, err := service.Health(ctx)

	require.NoError(t, err)
	assert.Equal(t, expectedStatus, status)
}

func TestService_Health_Unhealthy(t *testing.T) {
	expectedStatus := &HealthStatus{
		Healthy:     false,
		Message:     "connection refused",
		CircuitOpen: true,
	}

	provider := &MockAIProvider{
		healthFn: func(ctx context.Context) (*HealthStatus, error) {
			return expectedStatus, nil
		},
	}

	service := NewService(provider, true)
	ctx := context.Background()

	status, err := service.Health(ctx)

	require.NoError(t, err)
	assert.False(t, status.Healthy)
	assert.True(t, status.CircuitOpen)
}

func TestService_Close_Success(t *testing.T) {
	closeCalled := false
	provider := &MockAIProvider{
		closeFn: func() error {
			closeCalled = true
			return nil
		},
	}

	service := NewService(provider, true)

	err := service.Close()

	require.NoError(t, err)
	assert.True(t, closeCalled)
}

func TestService_Close_Error(t *testing.T) {
	expectedErr := errors.New("close error")
	provider := &MockAIProvider{
		closeFn: func() error {
			return expectedErr
		},
	}

	service := NewService(provider, true)

	err := service.Close()

	assert.ErrorIs(t, err, expectedErr)
}

func TestService_ContextCancellation(t *testing.T) {
	provider := &MockAIProvider{
		searchFn: func(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	service := NewService(provider, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	resp, err := service.Search(ctx, "test query", 10, 0.7)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestService_ContextWithRequestID(t *testing.T) {
	provider := &MockAIProvider{
		searchFn: func(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
			return &SearchResponse{Articles: []SimilarArticle{}}, nil
		},
	}

	service := NewService(provider, true)
	// Create context with request ID
	// Note: Using string key to match service implementation (see service.go:317)
	//nolint:staticcheck // SA1029: intentionally using string key to match production code
	ctx := context.WithValue(context.Background(), "request_id", "test-request-123")

	resp, err := service.Search(ctx, "test query", 10, 0.7)

	require.NoError(t, err)
	assert.NotNil(t, resp)
}
