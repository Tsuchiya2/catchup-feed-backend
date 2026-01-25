package ai

import (
	"context"
	"time"
)

// AIProvider defines the interface for AI service operations.
// This abstraction allows switching between different AI backends
// (e.g., catchup-ai gRPC, OpenAI API, local models) without changing business logic.
type AIProvider interface {
	// EmbedArticle generates an embedding for the given article.
	EmbedArticle(ctx context.Context, req EmbedRequest) (*EmbedResponse, error)

	// SearchSimilar finds semantically similar articles.
	SearchSimilar(ctx context.Context, req SearchRequest) (*SearchResponse, error)

	// QueryArticles performs RAG-based Q&A.
	QueryArticles(ctx context.Context, req QueryRequest) (*QueryResponse, error)

	// GenerateSummary generates a summary for the specified period.
	GenerateSummary(ctx context.Context, req SummaryRequest) (*SummaryResponse, error)

	// Health returns the health status of the AI provider.
	Health(ctx context.Context) (*HealthStatus, error)

	// Close releases resources held by the provider.
	Close() error
}

// EmbedRequest contains article data for embedding generation.
type EmbedRequest struct {
	ArticleID int64
	Title     string
	Content   string
	URL       string
}

// EmbedResponse contains the result of embedding generation.
type EmbedResponse struct {
	Success      bool
	ErrorMessage string
	Dimension    int32
}

// SearchRequest contains the query for semantic search.
type SearchRequest struct {
	Query         string
	Limit         int32
	MinSimilarity float32
}

// SearchResponse contains search results.
type SearchResponse struct {
	Articles      []SimilarArticle
	TotalSearched int64
}

// SimilarArticle represents an article with similarity score.
type SimilarArticle struct {
	ArticleID  int64
	Title      string
	URL        string
	Similarity float32
	Excerpt    string
}

// QueryRequest contains the question for RAG-based Q&A.
type QueryRequest struct {
	Question   string
	MaxContext int32
}

// QueryResponse contains the AI-generated answer.
type QueryResponse struct {
	Answer     string
	Sources    []SourceArticle
	Confidence float32
}

// SourceArticle represents an article cited in the answer.
type SourceArticle struct {
	ArticleID int64
	Title     string
	URL       string
	Relevance float32
}

// SummaryRequest contains parameters for summary generation.
type SummaryRequest struct {
	Period        SummaryPeriod
	MaxHighlights int32
}

// SummaryPeriod defines the time range for summarization.
type SummaryPeriod int

const (
	// SummaryPeriodUnspecified is the default unspecified period.
	SummaryPeriodUnspecified SummaryPeriod = 0
	// SummaryPeriodWeek represents a weekly summary.
	SummaryPeriodWeek SummaryPeriod = 1
	// SummaryPeriodMonth represents a monthly summary.
	SummaryPeriodMonth SummaryPeriod = 2
)

// SummaryResponse contains the generated summary.
type SummaryResponse struct {
	Summary      string
	Highlights   []Highlight
	ArticleCount int32
	StartDate    string
	EndDate      string
}

// Highlight represents a key topic or trend.
type Highlight struct {
	Topic        string
	Description  string
	ArticleCount int32
}

// HealthStatus represents the health of an AI provider.
type HealthStatus struct {
	Healthy     bool
	Latency     time.Duration
	Message     string
	CircuitOpen bool
}
