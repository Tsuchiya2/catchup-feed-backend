package repository

import (
	"context"

	"catchup-feed/internal/domain/entity"
)

// SimilarArticle represents the result of a similarity search.
// It contains the article ID and the similarity score (0.0 to 1.0).
type SimilarArticle struct {
	ArticleID  int64
	Similarity float64
}

// ArticleEmbeddingRepository defines the interface for managing article embeddings.
// It provides methods for storing, retrieving, searching, and deleting embeddings.
type ArticleEmbeddingRepository interface {
	// Upsert creates a new embedding or updates an existing one.
	// It uses the combination of (article_id, embedding_type, provider, model) as the unique key.
	// On conflict, it updates the embedding vector, dimension, and updated_at timestamp.
	// Returns an error if the embedding validation fails or database operation fails.
	Upsert(ctx context.Context, embedding *entity.ArticleEmbedding) error

	// FindByArticleID retrieves all embeddings for a given article ID.
	// Results are ordered by embedding_type, provider, and model.
	// Returns an empty slice (not nil) if no embeddings are found.
	// Returns an error if the database operation fails.
	FindByArticleID(ctx context.Context, articleID int64) ([]*entity.ArticleEmbedding, error)

	// SearchSimilar finds articles with embeddings similar to the provided vector.
	// It uses cosine similarity for comparison and returns results ordered by similarity (highest first).
	// The limit parameter controls the maximum number of results (default: 10, max: 100).
	// Only searches embeddings of the specified embedding_type.
	// Returns an error if the database operation fails or timeout occurs.
	SearchSimilar(ctx context.Context, embedding []float32, embeddingType entity.EmbeddingType, limit int) ([]SimilarArticle, error)

	// DeleteByArticleID removes all embeddings associated with an article.
	// Returns the number of deleted rows.
	// Returns 0 (not an error) if no embeddings were found.
	// Returns an error if the database operation fails.
	DeleteByArticleID(ctx context.Context, articleID int64) (int64, error)
}
