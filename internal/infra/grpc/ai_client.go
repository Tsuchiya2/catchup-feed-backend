package grpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"catchup-feed/internal/config"
	pb "catchup-feed/internal/interface/grpc/pb/ai"
	"catchup-feed/internal/usecase/ai"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sony/gobreaker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// Prometheus metrics for AI client
var (
	// aiClientRequestsTotal tracks the total number of AI client requests.
	aiClientRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_client_requests_total",
			Help: "Total number of AI client requests",
		},
		[]string{"method", "status"},
	)

	// aiClientRequestDuration tracks AI client request latency.
	// Buckets are optimized for AI operation response times:
	// - Fast: 0.1s, 0.5s, 1s
	// - Normal: 2s, 5s, 10s
	// - Slow: 30s, 60s, 120s
	aiClientRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ai_client_request_duration_seconds",
			Help:    "AI client request duration in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60, 120},
		},
		[]string{"method"},
	)

	// aiClientCircuitBreakerState tracks circuit breaker state.
	// 0 = closed, 1 = open, 2 = half-open
	aiClientCircuitBreakerState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "ai_client_circuit_breaker_state",
			Help: "AI circuit breaker state (0=closed, 1=open, 2=half-open)",
		},
		[]string{"name"},
	)
)

// Common errors
var (
	// ErrAIServiceUnavailable indicates the AI service is not reachable.
	ErrAIServiceUnavailable = errors.New("AI service unavailable")

	// ErrCircuitBreakerOpen indicates too many failures, circuit is open.
	ErrCircuitBreakerOpen = errors.New("AI service temporarily disabled (circuit breaker open)")

	// ErrInvalidQuery indicates the query failed validation.
	ErrInvalidQuery = errors.New("invalid query")

	// ErrTimeout indicates the operation exceeded its deadline.
	ErrTimeout = errors.New("operation timed out")

	// ErrAIDisabled indicates AI features are disabled by configuration.
	ErrAIDisabled = errors.New("AI features are disabled")
)

// GRPCAIProvider implements AIProvider interface using gRPC client.
type GRPCAIProvider struct {
	conn           *grpc.ClientConn
	client         pb.ArticleAIClient
	config         *config.AIConfig
	circuitBreaker *gobreaker.CircuitBreaker
	logger         *slog.Logger
}

// NewGRPCAIProvider creates a new gRPC AI provider.
func NewGRPCAIProvider(cfg *config.AIConfig) (*GRPCAIProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("AI config is required")
	}

	if !cfg.Enabled {
		return nil, ErrAIDisabled
	}

	// Create gRPC connection
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectionTimeout)
	defer cancel()

	conn, err := grpc.NewClient(
		cfg.GRPCAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection: %w", err)
	}

	// Initiate connection (non-blocking)
	conn.Connect()

	// Wait for connection to be ready
	if !waitForConnection(ctx, conn) {
		if closeErr := conn.Close(); closeErr != nil {
			slog.Error("failed to close gRPC connection", slog.Any("error", closeErr))
		}
		return nil, fmt.Errorf("AI service connection timeout")
	}

	client := pb.NewArticleAIClient(conn)

	// Configure circuit breaker
	cbSettings := gobreaker.Settings{
		Name:        "ai-service",
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
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			slog.Info("circuit breaker state changed",
				slog.String("name", name),
				slog.String("from", from.String()),
				slog.String("to", to.String()))
			// Update circuit breaker state metric
			updateCircuitBreakerMetric(name, to)
		},
	}

	cb := gobreaker.NewCircuitBreaker(cbSettings)

	provider := &GRPCAIProvider{
		conn:           conn,
		client:         client,
		config:         cfg,
		circuitBreaker: cb,
		logger:         slog.Default(),
	}

	return provider, nil
}

// EmbedArticle generates an embedding for the given article.
func (p *GRPCAIProvider) EmbedArticle(ctx context.Context, req ai.EmbedRequest) (*ai.EmbedResponse, error) {
	// Validate input
	if err := validateEmbedRequest(req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, p.config.Timeouts.EmbedArticle)
	defer cancel()

	// Track request duration
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		aiClientRequestDuration.WithLabelValues("EmbedArticle").Observe(duration)
	}()

	// Execute through circuit breaker
	result, err := p.circuitBreaker.Execute(func() (any, error) {
		pbReq := &pb.EmbedArticleRequest{
			ArticleId: req.ArticleID,
			Title:     req.Title,
			Content:   req.Content,
			Url:       req.URL,
		}

		pbResp, err := p.client.EmbedArticle(ctx, pbReq)
		if err != nil {
			return nil, p.mapGRPCError(err)
		}

		return &ai.EmbedResponse{
			Success:      pbResp.Success,
			ErrorMessage: pbResp.ErrorMessage,
			Dimension:    pbResp.Dimension,
		}, nil
	})

	// Record request metrics
	status := "success"
	if err != nil {
		status = "error"
		if errors.Is(err, gobreaker.ErrOpenState) {
			aiClientRequestsTotal.WithLabelValues("EmbedArticle", "circuit_breaker_open").Inc()
			return nil, ErrCircuitBreakerOpen
		}
	}
	aiClientRequestsTotal.WithLabelValues("EmbedArticle", status).Inc()

	if err != nil {
		return nil, err
	}

	return result.(*ai.EmbedResponse), nil
}

// SearchSimilar finds semantically similar articles.
func (p *GRPCAIProvider) SearchSimilar(ctx context.Context, req ai.SearchRequest) (*ai.SearchResponse, error) {
	// Validate input
	if err := validateSearchRequest(req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
	}

	// Apply defaults and limits
	limit := req.Limit
	if limit <= 0 {
		limit = p.config.Search.DefaultLimit
	}
	if limit > p.config.Search.MaxLimit {
		limit = p.config.Search.MaxLimit
	}

	minSimilarity := req.MinSimilarity
	if minSimilarity <= 0 {
		minSimilarity = p.config.Search.DefaultMinSimilarity
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, p.config.Timeouts.SearchSimilar)
	defer cancel()

	// Track request duration
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		aiClientRequestDuration.WithLabelValues("SearchSimilar").Observe(duration)
	}()

	// Execute through circuit breaker
	result, err := p.circuitBreaker.Execute(func() (any, error) {
		pbReq := &pb.SearchSimilarRequest{
			Query:         req.Query,
			Limit:         limit,
			MinSimilarity: minSimilarity,
		}

		pbResp, err := p.client.SearchSimilar(ctx, pbReq)
		if err != nil {
			return nil, p.mapGRPCError(err)
		}

		articles := make([]ai.SimilarArticle, 0, len(pbResp.Articles))
		for _, pbArt := range pbResp.Articles {
			articles = append(articles, ai.SimilarArticle{
				ArticleID:  pbArt.ArticleId,
				Title:      pbArt.Title,
				URL:        pbArt.Url,
				Similarity: pbArt.Similarity,
				Excerpt:    pbArt.Excerpt,
			})
		}

		return &ai.SearchResponse{
			Articles:      articles,
			TotalSearched: pbResp.TotalSearched,
		}, nil
	})

	// Record request metrics
	status := "success"
	if err != nil {
		status = "error"
		if errors.Is(err, gobreaker.ErrOpenState) {
			aiClientRequestsTotal.WithLabelValues("SearchSimilar", "circuit_breaker_open").Inc()
			return nil, ErrCircuitBreakerOpen
		}
	}
	aiClientRequestsTotal.WithLabelValues("SearchSimilar", status).Inc()

	if err != nil {
		return nil, err
	}

	return result.(*ai.SearchResponse), nil
}

// QueryArticles performs RAG-based Q&A.
func (p *GRPCAIProvider) QueryArticles(ctx context.Context, req ai.QueryRequest) (*ai.QueryResponse, error) {
	// Validate input
	if err := validateQueryRequest(req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
	}

	// Apply defaults and limits
	maxContext := req.MaxContext
	if maxContext <= 0 {
		maxContext = p.config.Search.DefaultMaxContext
	}
	if maxContext > p.config.Search.MaxContext {
		maxContext = p.config.Search.MaxContext
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, p.config.Timeouts.QueryArticles)
	defer cancel()

	// Track request duration
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		aiClientRequestDuration.WithLabelValues("QueryArticles").Observe(duration)
	}()

	// Execute through circuit breaker
	result, err := p.circuitBreaker.Execute(func() (any, error) {
		pbReq := &pb.QueryArticlesRequest{
			Question:   req.Question,
			MaxContext: maxContext,
		}

		pbResp, err := p.client.QueryArticles(ctx, pbReq)
		if err != nil {
			return nil, p.mapGRPCError(err)
		}

		sources := make([]ai.SourceArticle, 0, len(pbResp.Sources))
		for _, pbSrc := range pbResp.Sources {
			sources = append(sources, ai.SourceArticle{
				ArticleID: pbSrc.ArticleId,
				Title:     pbSrc.Title,
				URL:       pbSrc.Url,
				Relevance: pbSrc.Relevance,
			})
		}

		return &ai.QueryResponse{
			Answer:     pbResp.Answer,
			Sources:    sources,
			Confidence: pbResp.Confidence,
		}, nil
	})

	// Record request metrics
	status := "success"
	if err != nil {
		status = "error"
		if errors.Is(err, gobreaker.ErrOpenState) {
			aiClientRequestsTotal.WithLabelValues("QueryArticles", "circuit_breaker_open").Inc()
			return nil, ErrCircuitBreakerOpen
		}
	}
	aiClientRequestsTotal.WithLabelValues("QueryArticles", status).Inc()

	if err != nil {
		return nil, err
	}

	return result.(*ai.QueryResponse), nil
}

// GenerateSummary generates a summary for the specified period.
func (p *GRPCAIProvider) GenerateSummary(ctx context.Context, req ai.SummaryRequest) (*ai.SummaryResponse, error) {
	// Validate input
	if err := validateSummaryRequest(req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidQuery, err)
	}

	// Apply defaults
	maxHighlights := req.MaxHighlights
	if maxHighlights <= 0 {
		maxHighlights = 5
	}
	if maxHighlights > 10 {
		maxHighlights = 10
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, p.config.Timeouts.GenerateSummary)
	defer cancel()

	// Track request duration
	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		aiClientRequestDuration.WithLabelValues("GenerateSummary").Observe(duration)
	}()

	// Map period to proto enum
	var pbPeriod pb.SummaryPeriod
	switch req.Period {
	case ai.SummaryPeriodWeek:
		pbPeriod = pb.SummaryPeriod_SUMMARY_PERIOD_WEEK
	case ai.SummaryPeriodMonth:
		pbPeriod = pb.SummaryPeriod_SUMMARY_PERIOD_MONTH
	default:
		pbPeriod = pb.SummaryPeriod_SUMMARY_PERIOD_UNSPECIFIED
	}

	// Execute through circuit breaker
	result, err := p.circuitBreaker.Execute(func() (any, error) {
		pbReq := &pb.GenerateWeeklySummaryRequest{
			Period:        pbPeriod,
			MaxHighlights: maxHighlights,
		}

		pbResp, err := p.client.GenerateWeeklySummary(ctx, pbReq)
		if err != nil {
			return nil, p.mapGRPCError(err)
		}

		highlights := make([]ai.Highlight, 0, len(pbResp.Highlights))
		for _, pbHL := range pbResp.Highlights {
			highlights = append(highlights, ai.Highlight{
				Topic:        pbHL.Topic,
				Description:  pbHL.Description,
				ArticleCount: pbHL.ArticleCount,
			})
		}

		return &ai.SummaryResponse{
			Summary:      pbResp.Summary,
			Highlights:   highlights,
			ArticleCount: pbResp.ArticleCount,
			StartDate:    pbResp.StartDate,
			EndDate:      pbResp.EndDate,
		}, nil
	})

	// Record request metrics
	status := "success"
	if err != nil {
		status = "error"
		if errors.Is(err, gobreaker.ErrOpenState) {
			aiClientRequestsTotal.WithLabelValues("GenerateSummary", "circuit_breaker_open").Inc()
			return nil, ErrCircuitBreakerOpen
		}
	}
	aiClientRequestsTotal.WithLabelValues("GenerateSummary", status).Inc()

	if err != nil {
		return nil, err
	}

	return result.(*ai.SummaryResponse), nil
}

// Health returns the health status of the AI provider.
func (p *GRPCAIProvider) Health(ctx context.Context) (*ai.HealthStatus, error) {
	start := time.Now()

	// Check circuit breaker state
	cbState := p.circuitBreaker.State()
	if cbState == gobreaker.StateOpen {
		return &ai.HealthStatus{
			Healthy:     false,
			Latency:     0,
			Message:     "circuit breaker is open",
			CircuitOpen: true,
		}, nil
	}

	// Check connection state using typed constant
	state := p.conn.GetState()
	healthy := state == connectivity.Ready

	return &ai.HealthStatus{
		Healthy:     healthy,
		Latency:     time.Since(start),
		Message:     fmt.Sprintf("connection state: %s", state),
		CircuitOpen: cbState == gobreaker.StateOpen,
	}, nil
}

// Close releases resources held by the provider.
func (p *GRPCAIProvider) Close() error {
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}

// Validation functions

func validateEmbedRequest(req ai.EmbedRequest) error {
	if req.ArticleID <= 0 {
		return fmt.Errorf("article_id must be positive")
	}
	if strings.TrimSpace(req.Title) == "" {
		return fmt.Errorf("title cannot be empty")
	}
	if strings.TrimSpace(req.Content) == "" {
		return fmt.Errorf("content cannot be empty")
	}
	if len(req.Title) > 500 {
		return fmt.Errorf("title exceeds maximum length of 500 characters")
	}
	if len(req.Content) > 100000 {
		return fmt.Errorf("content exceeds maximum length of 100000 characters")
	}
	return nil
}

func validateSearchRequest(req ai.SearchRequest) error {
	if strings.TrimSpace(req.Query) == "" {
		return fmt.Errorf("query cannot be empty")
	}
	if len(req.Query) > 1000 {
		return fmt.Errorf("query exceeds maximum length of 1000 characters")
	}
	if req.Limit < 0 {
		return fmt.Errorf("limit must be non-negative")
	}
	if req.MinSimilarity < 0 || req.MinSimilarity > 1 {
		return fmt.Errorf("min_similarity must be between 0.0 and 1.0")
	}
	return nil
}

func validateQueryRequest(req ai.QueryRequest) error {
	if strings.TrimSpace(req.Question) == "" {
		return fmt.Errorf("question cannot be empty")
	}
	if len(req.Question) > 2000 {
		return fmt.Errorf("question exceeds maximum length of 2000 characters")
	}
	if req.MaxContext < 0 {
		return fmt.Errorf("max_context must be non-negative")
	}
	return nil
}

func validateSummaryRequest(req ai.SummaryRequest) error {
	if req.Period != ai.SummaryPeriodWeek && req.Period != ai.SummaryPeriodMonth {
		return fmt.Errorf("period must be WEEK or MONTH")
	}
	if req.MaxHighlights < 0 {
		return fmt.Errorf("max_highlights must be non-negative")
	}
	return nil
}

// mapGRPCError maps gRPC errors to domain errors.
func (p *GRPCAIProvider) mapGRPCError(err error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("%w: %v", ErrAIServiceUnavailable, err)
	}

	switch st.Code() {
	case codes.DeadlineExceeded:
		return ErrTimeout
	case codes.Unavailable:
		return ErrAIServiceUnavailable
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", ErrInvalidQuery, st.Message())
	default:
		return fmt.Errorf("AI service error: %s", st.Message())
	}
}

// waitForConnection waits for the gRPC connection to be ready.
func waitForConnection(ctx context.Context, conn *grpc.ClientConn) bool {
	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return true
		}
		if !conn.WaitForStateChange(ctx, state) {
			return false
		}
	}
}

// updateCircuitBreakerMetric updates the circuit breaker state metric.
func updateCircuitBreakerMetric(name string, state gobreaker.State) {
	var value float64
	switch state {
	case gobreaker.StateClosed:
		value = 0
	case gobreaker.StateOpen:
		value = 1
	case gobreaker.StateHalfOpen:
		value = 2
	}
	aiClientCircuitBreakerState.WithLabelValues(name).Set(value)
}
