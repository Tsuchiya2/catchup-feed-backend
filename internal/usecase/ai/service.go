package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
)

var (
	// ErrAIDisabled is returned when AI features are disabled.
	ErrAIDisabled = errors.New("AI features are disabled")
	// ErrInvalidQuery is returned when the search query is empty or invalid.
	ErrInvalidQuery = errors.New("search query cannot be empty")
	// ErrInvalidQuestion is returned when the question is empty or invalid.
	ErrInvalidQuestion = errors.New("question cannot be empty")
	// ErrInvalidPeriod is returned when the summary period is invalid.
	ErrInvalidPeriod = errors.New("invalid summary period")
)

// Service provides AI-powered operations for articles.
// It orchestrates AI provider calls with logging, validation, and error handling.
type Service struct {
	provider  AIProvider
	aiEnabled bool
}

// NewService creates a new AI service with the given provider.
//
// Parameters:
//   - provider: AI provider implementation (GRPCAIProvider or NoopAIProvider)
//   - aiEnabled: Feature flag to enable/disable AI operations
//
// Returns:
//   - *Service: Configured AI service ready to use
func NewService(provider AIProvider, aiEnabled bool) *Service {
	return &Service{
		provider:  provider,
		aiEnabled: aiEnabled,
	}
}

// Search performs semantic search to find similar articles.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - query: Search query string
//   - limit: Maximum number of results to return
//   - minSimilarity: Minimum similarity score threshold (0.0 to 1.0)
//
// Returns:
//   - *SearchResponse: Search results with similarity scores
//   - error: ErrAIDisabled if disabled, ErrInvalidQuery if query is empty, or provider errors
//
// Example:
//
//	resp, err := service.Search(ctx, "Go concurrency patterns", 10, 0.7)
//	if err != nil {
//	    log.Error("search failed", "error", err)
//	    return err
//	}
//	for _, article := range resp.Articles {
//	    fmt.Printf("%s (%.2f%%)\n", article.Title, article.Similarity*100)
//	}
func (s *Service) Search(ctx context.Context, query string, limit int32, minSimilarity float32) (*SearchResponse, error) {
	// Generate request ID for tracing
	requestID := s.getOrCreateRequestID(ctx)

	// Check feature flag
	if !s.aiEnabled {
		slog.Warn("AI search requested but feature is disabled",
			slog.String("request_id", requestID),
			slog.String("query", query))
		return nil, ErrAIDisabled
	}

	// Validate input
	if query == "" {
		slog.Warn("Empty search query provided",
			slog.String("request_id", requestID))
		return nil, ErrInvalidQuery
	}

	// Set default limit if not specified
	if limit <= 0 {
		limit = 10
	}

	// Set default similarity threshold if not specified
	if minSimilarity <= 0 {
		minSimilarity = 0.7
	}

	slog.Info("Performing semantic search",
		slog.String("request_id", requestID),
		slog.String("query", query),
		slog.Int("limit", int(limit)),
		slog.Float64("min_similarity", float64(minSimilarity)))

	// Call provider
	req := SearchRequest{
		Query:         query,
		Limit:         limit,
		MinSimilarity: minSimilarity,
	}

	resp, err := s.provider.SearchSimilar(ctx, req)
	if err != nil {
		slog.Error("Search failed",
			slog.String("request_id", requestID),
			slog.String("query", query),
			slog.Any("error", err))
		return nil, fmt.Errorf("AI search failed: %w", err)
	}

	slog.Info("Search completed successfully",
		slog.String("request_id", requestID),
		slog.Int("results", len(resp.Articles)),
		slog.Int64("total_searched", resp.TotalSearched))

	return resp, nil
}

// Ask performs RAG-based Q&A to answer questions using article context.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - question: Question to answer
//   - maxContext: Maximum number of articles to use as context
//
// Returns:
//   - *QueryResponse: Answer with source citations and confidence score
//   - error: ErrAIDisabled if disabled, ErrInvalidQuestion if question is empty, or provider errors
//
// Example:
//
//	resp, err := service.Ask(ctx, "What are the benefits of Go?", 5)
//	if err != nil {
//	    log.Error("ask failed", "error", err)
//	    return err
//	}
//	fmt.Printf("Answer: %s (Confidence: %.2f%%)\n", resp.Answer, resp.Confidence*100)
//	for _, src := range resp.Sources {
//	    fmt.Printf("  - %s (Relevance: %.2f%%)\n", src.Title, src.Relevance*100)
//	}
func (s *Service) Ask(ctx context.Context, question string, maxContext int32) (*QueryResponse, error) {
	// Generate request ID for tracing
	requestID := s.getOrCreateRequestID(ctx)

	// Check feature flag
	if !s.aiEnabled {
		slog.Warn("AI ask requested but feature is disabled",
			slog.String("request_id", requestID),
			slog.String("question", question))
		return nil, ErrAIDisabled
	}

	// Validate input
	if question == "" {
		slog.Warn("Empty question provided",
			slog.String("request_id", requestID))
		return nil, ErrInvalidQuestion
	}

	// Set default context size if not specified
	if maxContext <= 0 {
		maxContext = 5
	}

	slog.Info("Answering question with RAG",
		slog.String("request_id", requestID),
		slog.String("question", question),
		slog.Int("max_context", int(maxContext)))

	// Call provider
	req := QueryRequest{
		Question:   question,
		MaxContext: maxContext,
	}

	resp, err := s.provider.QueryArticles(ctx, req)
	if err != nil {
		slog.Error("Ask failed",
			slog.String("request_id", requestID),
			slog.String("question", question),
			slog.Any("error", err))
		return nil, fmt.Errorf("AI ask failed: %w", err)
	}

	slog.Info("Question answered successfully",
		slog.String("request_id", requestID),
		slog.Int("sources", len(resp.Sources)),
		slog.Float64("confidence", float64(resp.Confidence)))

	return resp, nil
}

// Summarize generates a summary of articles for the specified period.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - period: Time period to summarize (week or month)
//   - maxHighlights: Maximum number of highlights to include
//
// Returns:
//   - *SummaryResponse: Summary with highlights, date range, and article count
//   - error: ErrAIDisabled if disabled, ErrInvalidPeriod if period is invalid, or provider errors
//
// Example:
//
//	resp, err := service.Summarize(ctx, SummaryPeriodWeek, 5)
//	if err != nil {
//	    log.Error("summarize failed", "error", err)
//	    return err
//	}
//	fmt.Printf("Summary (%s to %s, %d articles):\n%s\n",
//	    resp.StartDate, resp.EndDate, resp.ArticleCount, resp.Summary)
//	for _, h := range resp.Highlights {
//	    fmt.Printf("  - %s: %s (%d articles)\n", h.Topic, h.Description, h.ArticleCount)
//	}
func (s *Service) Summarize(ctx context.Context, period SummaryPeriod, maxHighlights int32) (*SummaryResponse, error) {
	// Generate request ID for tracing
	requestID := s.getOrCreateRequestID(ctx)

	// Check feature flag
	if !s.aiEnabled {
		slog.Warn("AI summarize requested but feature is disabled",
			slog.String("request_id", requestID),
			slog.Any("period", period))
		return nil, ErrAIDisabled
	}

	// Validate input
	if period != SummaryPeriodWeek && period != SummaryPeriodMonth {
		slog.Warn("Invalid summary period",
			slog.String("request_id", requestID),
			slog.Any("period", period))
		return nil, ErrInvalidPeriod
	}

	// Set default highlights if not specified
	if maxHighlights <= 0 {
		maxHighlights = 5
	}

	periodStr := "week"
	if period == SummaryPeriodMonth {
		periodStr = "month"
	}

	slog.Info("Generating summary",
		slog.String("request_id", requestID),
		slog.String("period", periodStr),
		slog.Int("max_highlights", int(maxHighlights)))

	// Call provider
	req := SummaryRequest{
		Period:        period,
		MaxHighlights: maxHighlights,
	}

	resp, err := s.provider.GenerateSummary(ctx, req)
	if err != nil {
		slog.Error("Summarize failed",
			slog.String("request_id", requestID),
			slog.String("period", periodStr),
			slog.Any("error", err))
		return nil, fmt.Errorf("AI summarize failed: %w", err)
	}

	slog.Info("Summary generated successfully",
		slog.String("request_id", requestID),
		slog.Int("highlights", len(resp.Highlights)),
		slog.Int("article_count", int(resp.ArticleCount)),
		slog.String("date_range", fmt.Sprintf("%s to %s", resp.StartDate, resp.EndDate)))

	return resp, nil
}

// Health checks the health of the AI provider.
//
// Parameters:
//   - ctx: Context for cancellation and timeout (recommended: 5s)
//
// Returns:
//   - *HealthStatus: Health status with latency and circuit breaker state
//   - error: Provider errors if health check fails
//
// Example:
//
//	health, err := service.Health(ctx)
//	if err != nil {
//	    log.Error("health check failed", "error", err)
//	    return err
//	}
//	if !health.Healthy {
//	    log.Warn("AI service is unhealthy", "message", health.Message)
//	}
func (s *Service) Health(ctx context.Context) (*HealthStatus, error) {
	return s.provider.Health(ctx)
}

// Close releases resources held by the AI service.
// This method should be called when the service is no longer needed.
//
// Returns:
//   - error: Provider errors if cleanup fails
func (s *Service) Close() error {
	return s.provider.Close()
}

// getOrCreateRequestID extracts request ID from context or creates a new one.
func (s *Service) getOrCreateRequestID(ctx context.Context) string {
	// Try to get request ID from context
	if requestID, ok := ctx.Value("request_id").(string); ok && requestID != "" {
		return requestID
	}

	// Generate new request ID
	return uuid.New().String()
}
