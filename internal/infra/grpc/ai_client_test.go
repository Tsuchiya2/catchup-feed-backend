package grpc

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"catchup-feed/internal/config"
	pb "catchup-feed/internal/interface/grpc/pb/ai"
	"catchup-feed/internal/usecase/ai"

	"github.com/sony/gobreaker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// mockArticleAIServer implements ArticleAIServer for testing.
type mockArticleAIServer struct {
	pb.UnimplementedArticleAIServer

	embedFn   func(ctx context.Context, req *pb.EmbedArticleRequest) (*pb.EmbedArticleResponse, error)
	searchFn  func(ctx context.Context, req *pb.SearchSimilarRequest) (*pb.SearchSimilarResponse, error)
	queryFn   func(ctx context.Context, req *pb.QueryArticlesRequest) (*pb.QueryArticlesResponse, error)
	summaryFn func(ctx context.Context, req *pb.GenerateWeeklySummaryRequest) (*pb.GenerateWeeklySummaryResponse, error)
}

func (m *mockArticleAIServer) EmbedArticle(ctx context.Context, req *pb.EmbedArticleRequest) (*pb.EmbedArticleResponse, error) {
	if m.embedFn != nil {
		return m.embedFn(ctx, req)
	}
	return &pb.EmbedArticleResponse{Success: true, Dimension: 768}, nil
}

func (m *mockArticleAIServer) SearchSimilar(ctx context.Context, req *pb.SearchSimilarRequest) (*pb.SearchSimilarResponse, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, req)
	}
	return &pb.SearchSimilarResponse{
		Articles:      []*pb.SimilarArticle{{ArticleId: 1, Title: "Test", Similarity: 0.95}},
		TotalSearched: 100,
	}, nil
}

func (m *mockArticleAIServer) QueryArticles(ctx context.Context, req *pb.QueryArticlesRequest) (*pb.QueryArticlesResponse, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, req)
	}
	return &pb.QueryArticlesResponse{
		Answer:     "Test answer",
		Sources:    []*pb.SourceArticle{{ArticleId: 1, Title: "Source", Relevance: 0.9}},
		Confidence: 0.85,
	}, nil
}

func (m *mockArticleAIServer) GenerateWeeklySummary(ctx context.Context, req *pb.GenerateWeeklySummaryRequest) (*pb.GenerateWeeklySummaryResponse, error) {
	if m.summaryFn != nil {
		return m.summaryFn(ctx, req)
	}
	return &pb.GenerateWeeklySummaryResponse{
		Summary:      "Weekly summary",
		StartDate:    "2024-01-15",
		EndDate:      "2024-01-21",
		ArticleCount: 50,
		Highlights:   []*pb.Highlight{{Topic: "AI", Description: "AI news", ArticleCount: 10}},
	}, nil
}

// setupTestServer creates a bufconn-based gRPC server for testing.
func setupTestServer(t *testing.T, server *mockArticleAIServer) (*grpc.ClientConn, func()) {
	lis := bufconn.Listen(bufSize)
	s := grpc.NewServer()
	pb.RegisterArticleAIServer(s, server)

	go func() {
		if err := s.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Logf("Server error: %v", err)
		}
	}()

	dialer := func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}

	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	cleanup := func() {
		_ = conn.Close()
		s.Stop()
		_ = lis.Close()
	}

	return conn, cleanup
}

// createTestProvider creates a GRPCAIProvider with a mock server.
func createTestProvider(t *testing.T, server *mockArticleAIServer) (*GRPCAIProvider, func()) {
	conn, cleanup := setupTestServer(t, server)
	cfg := validTestConfig()

	cbSettings := gobreaker.Settings{
		Name:        "ai-service-test",
		MaxRequests: cfg.CircuitBreaker.MaxRequests,
		Interval:    cfg.CircuitBreaker.Interval,
		Timeout:     cfg.CircuitBreaker.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			if counts.Requests < cfg.CircuitBreaker.MinRequests {
				return false
			}
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return failureRatio >= cfg.CircuitBreaker.FailureThreshold
		},
	}

	provider := &GRPCAIProvider{
		conn:           conn,
		client:         pb.NewArticleAIClient(conn),
		config:         cfg,
		circuitBreaker: gobreaker.NewCircuitBreaker(cbSettings),
	}

	return provider, cleanup
}

// Integration tests using bufconn

func TestGRPCAIProvider_EmbedArticle_Success(t *testing.T) {
	server := &mockArticleAIServer{
		embedFn: func(ctx context.Context, req *pb.EmbedArticleRequest) (*pb.EmbedArticleResponse, error) {
			assert.Equal(t, int64(123), req.ArticleId)
			assert.Equal(t, "Test Title", req.Title)
			return &pb.EmbedArticleResponse{Success: true, Dimension: 768}, nil
		},
	}

	provider, cleanup := createTestProvider(t, server)
	defer cleanup()

	resp, err := provider.EmbedArticle(context.Background(), ai.EmbedRequest{
		ArticleID: 123,
		Title:     "Test Title",
		Content:   "Test Content",
		URL:       "https://example.com",
	})

	require.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, int32(768), resp.Dimension)
}

func TestGRPCAIProvider_EmbedArticle_ServerError(t *testing.T) {
	server := &mockArticleAIServer{
		embedFn: func(ctx context.Context, req *pb.EmbedArticleRequest) (*pb.EmbedArticleResponse, error) {
			return nil, status.Error(codes.Internal, "internal error")
		},
	}

	provider, cleanup := createTestProvider(t, server)
	defer cleanup()

	resp, err := provider.EmbedArticle(context.Background(), ai.EmbedRequest{
		ArticleID: 123,
		Title:     "Test Title",
		Content:   "Test Content",
		URL:       "https://example.com",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestGRPCAIProvider_SearchSimilar_Success(t *testing.T) {
	server := &mockArticleAIServer{
		searchFn: func(ctx context.Context, req *pb.SearchSimilarRequest) (*pb.SearchSimilarResponse, error) {
			assert.Equal(t, "test query", req.Query)
			return &pb.SearchSimilarResponse{
				Articles:      []*pb.SimilarArticle{{ArticleId: 1, Title: "Article 1", Similarity: 0.95}},
				TotalSearched: 100,
			}, nil
		},
	}

	provider, cleanup := createTestProvider(t, server)
	defer cleanup()

	resp, err := provider.SearchSimilar(context.Background(), ai.SearchRequest{
		Query:         "test query",
		Limit:         10,
		MinSimilarity: 0.7,
	})

	require.NoError(t, err)
	assert.Len(t, resp.Articles, 1)
	assert.Equal(t, int64(100), resp.TotalSearched)
}

func TestGRPCAIProvider_SearchSimilar_Unavailable(t *testing.T) {
	server := &mockArticleAIServer{
		searchFn: func(ctx context.Context, req *pb.SearchSimilarRequest) (*pb.SearchSimilarResponse, error) {
			return nil, status.Error(codes.Unavailable, "service unavailable")
		},
	}

	provider, cleanup := createTestProvider(t, server)
	defer cleanup()

	resp, err := provider.SearchSimilar(context.Background(), ai.SearchRequest{
		Query: "test query",
		Limit: 10,
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestGRPCAIProvider_QueryArticles_Success(t *testing.T) {
	server := &mockArticleAIServer{
		queryFn: func(ctx context.Context, req *pb.QueryArticlesRequest) (*pb.QueryArticlesResponse, error) {
			assert.Equal(t, "What is AI?", req.Question)
			return &pb.QueryArticlesResponse{
				Answer:     "AI is artificial intelligence",
				Sources:    []*pb.SourceArticle{{ArticleId: 1, Title: "AI Overview", Relevance: 0.95}},
				Confidence: 0.9,
			}, nil
		},
	}

	provider, cleanup := createTestProvider(t, server)
	defer cleanup()

	resp, err := provider.QueryArticles(context.Background(), ai.QueryRequest{
		Question:   "What is AI?",
		MaxContext: 5,
	})

	require.NoError(t, err)
	assert.Equal(t, "AI is artificial intelligence", resp.Answer)
	assert.Equal(t, float32(0.9), resp.Confidence)
}

func TestGRPCAIProvider_QueryArticles_DeadlineExceeded(t *testing.T) {
	server := &mockArticleAIServer{
		queryFn: func(ctx context.Context, req *pb.QueryArticlesRequest) (*pb.QueryArticlesResponse, error) {
			return nil, status.Error(codes.DeadlineExceeded, "timeout")
		},
	}

	provider, cleanup := createTestProvider(t, server)
	defer cleanup()

	resp, err := provider.QueryArticles(context.Background(), ai.QueryRequest{
		Question:   "What is AI?",
		MaxContext: 5,
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestGRPCAIProvider_GenerateSummary_Weekly(t *testing.T) {
	server := &mockArticleAIServer{
		summaryFn: func(ctx context.Context, req *pb.GenerateWeeklySummaryRequest) (*pb.GenerateWeeklySummaryResponse, error) {
			assert.Equal(t, pb.SummaryPeriod_SUMMARY_PERIOD_WEEK, req.Period)
			return &pb.GenerateWeeklySummaryResponse{
				Summary:      "Weekly tech summary",
				ArticleCount: 50,
				Highlights:   []*pb.Highlight{{Topic: "AI", Description: "AI news", ArticleCount: 10}},
			}, nil
		},
	}

	provider, cleanup := createTestProvider(t, server)
	defer cleanup()

	resp, err := provider.GenerateSummary(context.Background(), ai.SummaryRequest{
		Period:        ai.SummaryPeriodWeek,
		MaxHighlights: 5,
	})

	require.NoError(t, err)
	assert.Equal(t, "Weekly tech summary", resp.Summary)
	assert.Equal(t, int32(50), resp.ArticleCount)
}

func TestGRPCAIProvider_GenerateSummary_Monthly(t *testing.T) {
	server := &mockArticleAIServer{
		summaryFn: func(ctx context.Context, req *pb.GenerateWeeklySummaryRequest) (*pb.GenerateWeeklySummaryResponse, error) {
			assert.Equal(t, pb.SummaryPeriod_SUMMARY_PERIOD_MONTH, req.Period)
			return &pb.GenerateWeeklySummaryResponse{Summary: "Monthly summary", ArticleCount: 200}, nil
		},
	}

	provider, cleanup := createTestProvider(t, server)
	defer cleanup()

	resp, err := provider.GenerateSummary(context.Background(), ai.SummaryRequest{
		Period:        ai.SummaryPeriodMonth,
		MaxHighlights: 10,
	})

	require.NoError(t, err)
	assert.Equal(t, "Monthly summary", resp.Summary)
}

func TestGRPCAIProvider_Health_Healthy(t *testing.T) {
	provider, cleanup := createTestProvider(t, &mockArticleAIServer{})
	defer cleanup()

	// Make an RPC call first to establish connection (bufconn needs this)
	_, _ = provider.SearchSimilar(context.Background(), ai.SearchRequest{
		Query: "test",
		Limit: 1,
	})

	status, err := provider.Health(context.Background())

	require.NoError(t, err)
	assert.True(t, status.Healthy)
	assert.False(t, status.CircuitOpen)
}

func TestGRPCAIProvider_Close(t *testing.T) {
	provider, cleanup := createTestProvider(t, &mockArticleAIServer{})
	defer cleanup()

	err := provider.Close()
	assert.NoError(t, err)
}

func TestGRPCAIProvider_ContextCancellation(t *testing.T) {
	server := &mockArticleAIServer{
		searchFn: func(ctx context.Context, req *pb.SearchSimilarRequest) (*pb.SearchSimilarResponse, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return &pb.SearchSimilarResponse{}, nil
			}
		},
	}

	provider, cleanup := createTestProvider(t, server)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	resp, err := provider.SearchSimilar(ctx, ai.SearchRequest{Query: "test", Limit: 10})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestGRPCAIProvider_InvalidArgument(t *testing.T) {
	server := &mockArticleAIServer{
		searchFn: func(ctx context.Context, req *pb.SearchSimilarRequest) (*pb.SearchSimilarResponse, error) {
			return nil, status.Error(codes.InvalidArgument, "invalid query")
		},
	}

	provider, cleanup := createTestProvider(t, server)
	defer cleanup()

	resp, err := provider.SearchSimilar(context.Background(), ai.SearchRequest{Query: "test", Limit: 10})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

// Unit tests for validation functions

func TestValidateEmbedRequest(t *testing.T) {
	tests := []struct {
		name      string
		req       ai.EmbedRequest
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid request",
			req: ai.EmbedRequest{
				ArticleID: 1,
				Title:     "Test Title",
				Content:   "Test content",
				URL:       "https://example.com",
			},
			expectErr: false,
		},
		{
			name: "invalid article ID zero",
			req: ai.EmbedRequest{
				ArticleID: 0,
				Title:     "Test Title",
				Content:   "Test content",
			},
			expectErr: true,
			errMsg:    "article_id must be positive",
		},
		{
			name: "invalid article ID negative",
			req: ai.EmbedRequest{
				ArticleID: -1,
				Title:     "Test Title",
				Content:   "Test content",
			},
			expectErr: true,
			errMsg:    "article_id must be positive",
		},
		{
			name: "empty title",
			req: ai.EmbedRequest{
				ArticleID: 1,
				Title:     "",
				Content:   "Test content",
			},
			expectErr: true,
			errMsg:    "title cannot be empty",
		},
		{
			name: "whitespace only title",
			req: ai.EmbedRequest{
				ArticleID: 1,
				Title:     "   ",
				Content:   "Test content",
			},
			expectErr: true,
			errMsg:    "title cannot be empty",
		},
		{
			name: "empty content",
			req: ai.EmbedRequest{
				ArticleID: 1,
				Title:     "Test Title",
				Content:   "",
			},
			expectErr: true,
			errMsg:    "content cannot be empty",
		},
		{
			name: "title too long",
			req: ai.EmbedRequest{
				ArticleID: 1,
				Title:     strings.Repeat("a", 501),
				Content:   "Test content",
			},
			expectErr: true,
			errMsg:    "title exceeds maximum length",
		},
		{
			name: "content too long",
			req: ai.EmbedRequest{
				ArticleID: 1,
				Title:     "Test Title",
				Content:   strings.Repeat("a", 100001),
			},
			expectErr: true,
			errMsg:    "content exceeds maximum length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEmbedRequest(tt.req)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateSearchRequest(t *testing.T) {
	tests := []struct {
		name      string
		req       ai.SearchRequest
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid request",
			req: ai.SearchRequest{
				Query:         "test query",
				Limit:         10,
				MinSimilarity: 0.7,
			},
			expectErr: false,
		},
		{
			name: "valid request with zero limit",
			req: ai.SearchRequest{
				Query:         "test query",
				Limit:         0,
				MinSimilarity: 0.7,
			},
			expectErr: false,
		},
		{
			name: "empty query",
			req: ai.SearchRequest{
				Query: "",
				Limit: 10,
			},
			expectErr: true,
			errMsg:    "query cannot be empty",
		},
		{
			name: "whitespace only query",
			req: ai.SearchRequest{
				Query: "   ",
				Limit: 10,
			},
			expectErr: true,
			errMsg:    "query cannot be empty",
		},
		{
			name: "query too long",
			req: ai.SearchRequest{
				Query: strings.Repeat("a", 1001),
				Limit: 10,
			},
			expectErr: true,
			errMsg:    "query exceeds maximum length",
		},
		{
			name: "negative limit",
			req: ai.SearchRequest{
				Query: "test",
				Limit: -1,
			},
			expectErr: true,
			errMsg:    "limit must be non-negative",
		},
		{
			name: "negative min similarity",
			req: ai.SearchRequest{
				Query:         "test",
				Limit:         10,
				MinSimilarity: -0.1,
			},
			expectErr: true,
			errMsg:    "min_similarity must be between",
		},
		{
			name: "min similarity above 1",
			req: ai.SearchRequest{
				Query:         "test",
				Limit:         10,
				MinSimilarity: 1.5,
			},
			expectErr: true,
			errMsg:    "min_similarity must be between",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSearchRequest(tt.req)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateQueryRequest(t *testing.T) {
	tests := []struct {
		name      string
		req       ai.QueryRequest
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid request",
			req: ai.QueryRequest{
				Question:   "What is AI?",
				MaxContext: 5,
			},
			expectErr: false,
		},
		{
			name: "valid request with zero max context",
			req: ai.QueryRequest{
				Question:   "What is AI?",
				MaxContext: 0,
			},
			expectErr: false,
		},
		{
			name: "empty question",
			req: ai.QueryRequest{
				Question:   "",
				MaxContext: 5,
			},
			expectErr: true,
			errMsg:    "question cannot be empty",
		},
		{
			name: "whitespace only question",
			req: ai.QueryRequest{
				Question:   "   ",
				MaxContext: 5,
			},
			expectErr: true,
			errMsg:    "question cannot be empty",
		},
		{
			name: "question too long",
			req: ai.QueryRequest{
				Question:   strings.Repeat("a", 2001),
				MaxContext: 5,
			},
			expectErr: true,
			errMsg:    "question exceeds maximum length",
		},
		{
			name: "negative max context",
			req: ai.QueryRequest{
				Question:   "What is AI?",
				MaxContext: -1,
			},
			expectErr: true,
			errMsg:    "max_context must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateQueryRequest(tt.req)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateSummaryRequest(t *testing.T) {
	tests := []struct {
		name      string
		req       ai.SummaryRequest
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid week request",
			req: ai.SummaryRequest{
				Period:        ai.SummaryPeriodWeek,
				MaxHighlights: 5,
			},
			expectErr: false,
		},
		{
			name: "valid month request",
			req: ai.SummaryRequest{
				Period:        ai.SummaryPeriodMonth,
				MaxHighlights: 10,
			},
			expectErr: false,
		},
		{
			name: "invalid period unspecified",
			req: ai.SummaryRequest{
				Period:        ai.SummaryPeriodUnspecified,
				MaxHighlights: 5,
			},
			expectErr: true,
			errMsg:    "period must be WEEK or MONTH",
		},
		{
			name: "invalid period out of range",
			req: ai.SummaryRequest{
				Period:        ai.SummaryPeriod(99),
				MaxHighlights: 5,
			},
			expectErr: true,
			errMsg:    "period must be WEEK or MONTH",
		},
		{
			name: "negative max highlights",
			req: ai.SummaryRequest{
				Period:        ai.SummaryPeriodWeek,
				MaxHighlights: -1,
			},
			expectErr: true,
			errMsg:    "max_highlights must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSummaryRequest(tt.req)
			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewGRPCAIProvider_NilConfig(t *testing.T) {
	provider, err := NewGRPCAIProvider(nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI config is required")
	assert.Nil(t, provider)
}

func TestNewGRPCAIProvider_AIDisabled(t *testing.T) {
	cfg := &config.AIConfig{
		Enabled: false,
	}

	provider, err := NewGRPCAIProvider(cfg)

	assert.ErrorIs(t, err, ai.ErrAIDisabled)
	assert.Nil(t, provider)
}

func TestUpdateCircuitBreakerMetric(t *testing.T) {
	// Test each circuit breaker state
	tests := []struct {
		name  string
		state gobreaker.State
	}{
		{"closed state", gobreaker.StateClosed},
		{"open state", gobreaker.StateOpen},
		{"half-open state", gobreaker.StateHalfOpen},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This function doesn't return anything, just updates metrics
			// We verify it doesn't panic
			updateCircuitBreakerMetric("test", tt.state)
		})
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := validTestConfig()

	assert.Equal(t, "localhost:50051", cfg.GRPCAddress)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 10*time.Second, cfg.ConnectionTimeout)
}

func validTestConfig() *config.AIConfig {
	return &config.AIConfig{
		GRPCAddress:       "localhost:50051",
		Enabled:           true,
		ConnectionTimeout: 10 * time.Second,
		Timeouts: config.TimeoutConfig{
			EmbedArticle:    30 * time.Second,
			SearchSimilar:   30 * time.Second,
			QueryArticles:   60 * time.Second,
			GenerateSummary: 120 * time.Second,
		},
		Search: config.SearchConfig{
			DefaultLimit:         10,
			MaxLimit:             50,
			DefaultMinSimilarity: 0.7,
			DefaultMaxContext:    5,
			MaxContext:           20,
		},
		CircuitBreaker: config.CircuitBreakerConfig{
			MaxRequests:      3,
			Interval:         10 * time.Second,
			Timeout:          30 * time.Second,
			FailureThreshold: 0.6,
			MinRequests:      5,
		},
		Observability: config.ObservabilityConfig{
			EnableTracing:   false,
			TracingEndpoint: "localhost:4317",
			LogLevel:        "info",
			EnableMetrics:   true,
		},
	}
}
