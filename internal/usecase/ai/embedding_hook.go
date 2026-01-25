package ai

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"catchup-feed/internal/domain/entity"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// embeddingTimeout is the maximum time to wait for embedding generation.
	// This prevents the embedding goroutine from running indefinitely.
	embeddingTimeout = 30 * time.Second

	// requestIDKey is the context key for request ID.
	requestIDKey contextKey = "request_id"
)

// Prometheus metrics for embedding hook
var (
	// embeddingPendingTotal tracks pending embedding operations.
	embeddingPendingTotal = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "ai_embedding_pending_total",
			Help: "Number of pending embedding operations",
		},
	)

	// embeddingProcessedTotal tracks processed embeddings.
	embeddingProcessedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_embedding_processed_total",
			Help: "Total embeddings processed",
		},
		[]string{"status"},
	)
)

// EmbeddingHook provides asynchronous article embedding functionality.
// It spawns a goroutine to generate embeddings without blocking the caller.
type EmbeddingHook struct {
	provider  AIProvider
	aiEnabled bool
}

// NewEmbeddingHook creates a new embedding hook with the given provider.
//
// Parameters:
//   - provider: AI provider implementation
//   - aiEnabled: Feature flag to enable/disable embedding generation
//
// Returns:
//   - *EmbeddingHook: Configured embedding hook ready to use
func NewEmbeddingHook(provider AIProvider, aiEnabled bool) *EmbeddingHook {
	return &EmbeddingHook{
		provider:  provider,
		aiEnabled: aiEnabled,
	}
}

// EmbedArticleAsync generates an article embedding asynchronously.
// This method is non-blocking and returns immediately.
// The embedding generation happens in a background goroutine.
//
// Behavior:
//   - Spawns a goroutine for embedding generation
//   - Returns immediately (does not block caller)
//   - Uses detached context with 30s timeout
//   - Gracefully handles failures (logs warnings, no error propagation)
//   - Skips execution if AI_ENABLED=false
//
// Parameters:
//   - ctx: Context from caller (used for logging only, not propagated)
//   - article: Article to embed (must not be nil)
//
// Example:
//
//	// In fetch service after article creation:
//	if err := articleRepo.Create(ctx, article); err != nil {
//	    return err
//	}
//	embeddingHook.EmbedArticleAsync(ctx, article) // Non-blocking
//	// Execution continues immediately
func (h *EmbeddingHook) EmbedArticleAsync(ctx context.Context, article *entity.Article) {
	// Check feature flag before spawning goroutine
	if !h.aiEnabled {
		// AI disabled, skip embedding
		return
	}

	// Validate input before spawning goroutine
	if article == nil {
		slog.Warn("Cannot embed nil article")
		return
	}

	// Extract request ID from parent context for tracing
	requestID, ok := ctx.Value(requestIDKey).(string)
	if !ok || requestID == "" {
		requestID = "unknown"
	}

	// Spawn goroutine for async execution
	go h.embedArticle(requestID, article)
}

// embedArticle performs the actual embedding generation in a goroutine.
// This method runs asynchronously and handles all errors gracefully.
func (h *EmbeddingHook) embedArticle(requestID string, article *entity.Article) {
	// Track pending operation - must be decremented on all exit paths including panic
	embeddingPendingTotal.Inc()
	completed := false
	defer func() {
		// Ensure gauge is decremented even on panic
		if !completed {
			embeddingPendingTotal.Dec()
			embeddingProcessedTotal.WithLabelValues("panic").Inc()
		}
		if r := recover(); r != nil {
			slog.Error("Panic in embedding hook",
				slog.String("request_id", requestID),
				slog.Int64("article_id", article.ID),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
		}
	}()

	// Create detached context with timeout
	// We use context.Background() instead of parent context to avoid cancellation
	// when the parent request completes
	ctx, cancel := context.WithTimeout(context.Background(), embeddingTimeout)
	defer cancel()

	// Add request ID to context for tracing
	ctx = context.WithValue(ctx, requestIDKey, requestID)

	slog.Info("Generating article embedding",
		slog.String("request_id", requestID),
		slog.Int64("article_id", article.ID),
		slog.String("url", article.URL),
		slog.String("title", article.Title))

	// Prepare embedding request
	req := EmbedRequest{
		ArticleID: article.ID,
		Title:     article.Title,
		Content:   article.Summary, // Use summary for embedding (already processed)
		URL:       article.URL,
	}

	// Call provider with metrics tracking
	startTime := time.Now()
	resp, err := h.provider.EmbedArticle(ctx, req)
	duration := time.Since(startTime)

	if err != nil {
		// Record failure and mark as completed
		completed = true
		recordEmbeddingComplete(false)

		// Embedding failed, log warning but do not propagate error
		// This is graceful degradation - article is saved, embedding can be retried later
		slog.Warn("Article embedding failed (non-blocking)",
			slog.String("request_id", requestID),
			slog.Int64("article_id", article.ID),
			slog.String("url", article.URL),
			slog.Duration("duration", duration),
			slog.Any("error", err))
		return
	}

	// Check response status
	if !resp.Success {
		// Record failure and mark as completed
		completed = true
		recordEmbeddingComplete(false)

		slog.Warn("Article embedding returned error",
			slog.String("request_id", requestID),
			slog.Int64("article_id", article.ID),
			slog.String("url", article.URL),
			slog.String("error_message", resp.ErrorMessage),
			slog.Duration("duration", duration))
		return
	}

	// Record success and mark as completed
	completed = true
	recordEmbeddingComplete(true)

	// Success
	slog.Info("Article embedding generated successfully",
		slog.String("request_id", requestID),
		slog.Int64("article_id", article.ID),
		slog.String("url", article.URL),
		slog.Int("dimension", int(resp.Dimension)),
		slog.Duration("duration", duration))
}

// recordEmbeddingComplete decrements the pending count and records the result.
func recordEmbeddingComplete(success bool) {
	embeddingPendingTotal.Dec()
	status := "success"
	if !success {
		status = "failure"
	}
	embeddingProcessedTotal.WithLabelValues(status).Inc()
}
