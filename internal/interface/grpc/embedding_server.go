// Package grpc provides gRPC server implementations for the application.
package grpc

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"catchup-feed/internal/domain/entity"
	pb "catchup-feed/internal/interface/grpc/pb/embedding"
	"catchup-feed/internal/repository"
)

// EmbeddingServer implements the gRPC EmbeddingService.
// It provides endpoints for storing, retrieving, and searching article embeddings.
type EmbeddingServer struct {
	pb.UnimplementedEmbeddingServiceServer
	repo repository.ArticleEmbeddingRepository
}

// NewEmbeddingServer creates a new EmbeddingServer with the given repository.
func NewEmbeddingServer(repo repository.ArticleEmbeddingRepository) *EmbeddingServer {
	return &EmbeddingServer{repo: repo}
}

// StoreEmbedding stores or updates an embedding for an article.
// It validates the request, converts it to a domain entity, and persists it.
// Returns success=false with an error message for validation or persistence failures.
func (s *EmbeddingServer) StoreEmbedding(
	ctx context.Context,
	req *pb.StoreEmbeddingRequest,
) (*pb.StoreEmbeddingResponse, error) {
	// Validate article_id
	if req.ArticleId <= 0 {
		return &pb.StoreEmbeddingResponse{
			Success:      false,
			ErrorMessage: "article_id must be positive",
		}, nil
	}

	// Validate embedding is not empty
	if len(req.Embedding) == 0 {
		return &pb.StoreEmbeddingResponse{
			Success:      false,
			ErrorMessage: "embedding cannot be empty",
		}, nil
	}

	// Validate dimension matches embedding length
	if int(req.Dimension) != len(req.Embedding) {
		return &pb.StoreEmbeddingResponse{
			Success:      false,
			ErrorMessage: "dimension does not match embedding length",
		}, nil
	}

	// Validate embedding_type
	embeddingType := entity.EmbeddingType(req.EmbeddingType)
	if !embeddingType.IsValid() {
		return &pb.StoreEmbeddingResponse{
			Success:      false,
			ErrorMessage: "invalid embedding_type: must be one of 'title', 'content', 'summary'",
		}, nil
	}

	// Validate provider
	provider := entity.EmbeddingProvider(req.Provider)
	if !provider.IsValid() {
		return &pb.StoreEmbeddingResponse{
			Success:      false,
			ErrorMessage: "invalid provider: must be one of 'openai', 'voyage'",
		}, nil
	}

	// Validate model is not empty
	if req.Model == "" {
		return &pb.StoreEmbeddingResponse{
			Success:      false,
			ErrorMessage: "model cannot be empty",
		}, nil
	}

	// Convert to domain entity
	embedding := &entity.ArticleEmbedding{
		ArticleID:     req.ArticleId,
		EmbeddingType: embeddingType,
		Provider:      provider,
		Model:         req.Model,
		Dimension:     req.Dimension,
		Embedding:     req.Embedding,
	}

	// Store embedding via repository
	if err := s.repo.Upsert(ctx, embedding); err != nil {
		slog.Error("failed to store embedding",
			slog.Int64("article_id", req.ArticleId),
			slog.String("embedding_type", req.EmbeddingType),
			slog.String("provider", req.Provider),
			slog.String("error", err.Error()),
		)
		return &pb.StoreEmbeddingResponse{
			Success:      false,
			ErrorMessage: "failed to store embedding",
		}, nil
	}

	slog.Info("embedding stored successfully",
		slog.Int64("article_id", req.ArticleId),
		slog.Int64("embedding_id", embedding.ID),
		slog.String("embedding_type", req.EmbeddingType),
		slog.String("provider", req.Provider),
		slog.Int("dimension", int(req.Dimension)),
	)

	return &pb.StoreEmbeddingResponse{
		Success:     true,
		EmbeddingId: embedding.ID,
	}, nil
}

// GetEmbeddings retrieves all embeddings for an article.
// Returns an empty list if no embeddings exist for the article.
// Returns a gRPC error for invalid input or internal errors.
func (s *EmbeddingServer) GetEmbeddings(
	ctx context.Context,
	req *pb.GetEmbeddingsRequest,
) (*pb.GetEmbeddingsResponse, error) {
	// Validate article_id
	if req.ArticleId <= 0 {
		return nil, status.Error(codes.InvalidArgument, "article_id must be positive")
	}

	// Retrieve embeddings from repository
	embeddings, err := s.repo.FindByArticleID(ctx, req.ArticleId)
	if err != nil {
		slog.Error("failed to get embeddings",
			slog.Int64("article_id", req.ArticleId),
			slog.String("error", err.Error()),
		)
		return nil, status.Error(codes.Internal, "failed to retrieve embeddings")
	}

	slog.Debug("embeddings retrieved",
		slog.Int64("article_id", req.ArticleId),
		slog.Int("count", len(embeddings)),
	)

	// Convert domain entities to proto messages
	pbEmbeddings := make([]*pb.ArticleEmbedding, 0, len(embeddings))
	for _, e := range embeddings {
		pbEmbeddings = append(pbEmbeddings, entityToProto(e))
	}

	return &pb.GetEmbeddingsResponse{
		Embeddings: pbEmbeddings,
	}, nil
}

// SearchSimilar finds articles similar to a query vector using cosine similarity.
// Returns results sorted by similarity score in descending order.
// Uses default limit of 10 if not specified, maximum is 100.
func (s *EmbeddingServer) SearchSimilar(
	ctx context.Context,
	req *pb.SearchSimilarRequest,
) (*pb.SearchSimilarResponse, error) {
	// Validate embedding is not empty
	if len(req.Embedding) == 0 {
		return nil, status.Error(codes.InvalidArgument, "embedding cannot be empty")
	}

	// Validate embedding_type
	embeddingType := entity.EmbeddingType(req.EmbeddingType)
	if !embeddingType.IsValid() {
		return nil, status.Errorf(codes.InvalidArgument, "invalid embedding_type: %s", req.EmbeddingType)
	}

	// Apply default and max limit
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	// Search similar articles via repository
	results, err := s.repo.SearchSimilar(ctx, req.Embedding, embeddingType, limit)
	if err != nil {
		slog.Error("similarity search failed",
			slog.String("embedding_type", req.EmbeddingType),
			slog.Int("limit", limit),
			slog.Int("vector_dimension", len(req.Embedding)),
			slog.String("error", err.Error()),
		)
		return nil, status.Error(codes.Internal, "similarity search failed")
	}

	slog.Info("similarity search completed",
		slog.String("embedding_type", req.EmbeddingType),
		slog.Int("limit", limit),
		slog.Int("results_count", len(results)),
	)

	// Convert results to proto messages
	pbArticles := make([]*pb.SimilarArticle, 0, len(results))
	for _, r := range results {
		pbArticles = append(pbArticles, &pb.SimilarArticle{
			ArticleId:  r.ArticleID,
			Similarity: float32(r.Similarity),
		})
	}

	return &pb.SearchSimilarResponse{
		Articles: pbArticles,
	}, nil
}

// DeleteEmbedding removes all embeddings associated with an article.
// Returns the number of deleted embeddings.
func (s *EmbeddingServer) DeleteEmbedding(
	ctx context.Context,
	req *pb.DeleteEmbeddingRequest,
) (*pb.DeleteEmbeddingResponse, error) {
	// Validate article_id
	if req.ArticleId <= 0 {
		return &pb.DeleteEmbeddingResponse{
			Success:      false,
			ErrorMessage: "article_id must be positive",
		}, nil
	}

	// Delete embeddings via repository
	deletedCount, err := s.repo.DeleteByArticleID(ctx, req.ArticleId)
	if err != nil {
		slog.Error("failed to delete embeddings",
			slog.Int64("article_id", req.ArticleId),
			slog.String("error", err.Error()),
		)
		return &pb.DeleteEmbeddingResponse{
			Success:      false,
			ErrorMessage: "failed to delete embeddings",
		}, nil
	}

	slog.Info("embeddings deleted successfully",
		slog.Int64("article_id", req.ArticleId),
		slog.Int64("deleted_count", deletedCount),
	)

	return &pb.DeleteEmbeddingResponse{
		Success:      true,
		DeletedCount: deletedCount,
	}, nil
}

// entityToProto converts a domain entity to a proto message.
func entityToProto(e *entity.ArticleEmbedding) *pb.ArticleEmbedding {
	return &pb.ArticleEmbedding{
		Id:            e.ID,
		ArticleId:     e.ArticleID,
		EmbeddingType: string(e.EmbeddingType),
		Provider:      string(e.Provider),
		Model:         e.Model,
		Dimension:     e.Dimension,
		Embedding:     e.Embedding,
		CreatedAt:     e.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     e.UpdatedAt.Format(time.RFC3339),
	}
}
