package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"

	"github.com/pgvector/pgvector-go"
)

// DefaultSearchTimeout is the default timeout for similarity search queries.
const DefaultSearchTimeout = 5 * time.Second

// ArticleEmbeddingRepo implements the ArticleEmbeddingRepository interface for PostgreSQL.
type ArticleEmbeddingRepo struct {
	db *sql.DB
}

// NewArticleEmbeddingRepo creates a new PostgreSQL-based ArticleEmbeddingRepository.
func NewArticleEmbeddingRepo(db *sql.DB) repository.ArticleEmbeddingRepository {
	return &ArticleEmbeddingRepo{
		db: db,
	}
}

// Upsert creates a new embedding or updates an existing one.
// Uses INSERT ... ON CONFLICT DO UPDATE to handle unique constraint violations.
func (repo *ArticleEmbeddingRepo) Upsert(ctx context.Context, embedding *entity.ArticleEmbedding) error {
	// Check for nil pointer
	if embedding == nil {
		return fmt.Errorf("Upsert: embedding is nil")
	}

	// Validate entity before database operation
	if err := embedding.Validate(); err != nil {
		return fmt.Errorf("Upsert: %w", err)
	}

	// Convert []float32 to pgvector.Vector
	vector := pgvector.NewVector(embedding.Embedding)

	const query = `
INSERT INTO article_embeddings (article_id, embedding_type, provider, model, dimension, embedding, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
ON CONFLICT (article_id, embedding_type, provider, model)
DO UPDATE SET
	dimension = EXCLUDED.dimension,
	embedding = EXCLUDED.embedding,
	updated_at = NOW()
RETURNING id, created_at, updated_at`

	err := repo.db.QueryRowContext(ctx, query,
		embedding.ArticleID,
		string(embedding.EmbeddingType),
		string(embedding.Provider),
		embedding.Model,
		embedding.Dimension,
		vector,
	).Scan(&embedding.ID, &embedding.CreatedAt, &embedding.UpdatedAt)

	if err != nil {
		return fmt.Errorf("Upsert: %w", err)
	}

	return nil
}

// FindByArticleID retrieves all embeddings for a given article ID.
// Returns an empty slice if no embeddings are found.
func (repo *ArticleEmbeddingRepo) FindByArticleID(ctx context.Context, articleID int64) ([]*entity.ArticleEmbedding, error) {
	const query = `
SELECT id, article_id, embedding_type, provider, model, dimension, embedding, created_at, updated_at
FROM article_embeddings
WHERE article_id = $1
ORDER BY embedding_type, provider, model`

	rows, err := repo.db.QueryContext(ctx, query, articleID)
	if err != nil {
		return nil, fmt.Errorf("FindByArticleID: %w", err)
	}
	defer func() { _ = rows.Close() }()

	embeddings := make([]*entity.ArticleEmbedding, 0)
	for rows.Next() {
		emb := &entity.ArticleEmbedding{}
		var vector pgvector.Vector
		var embType string
		var provider string

		err := rows.Scan(
			&emb.ID,
			&emb.ArticleID,
			&embType,
			&provider,
			&emb.Model,
			&emb.Dimension,
			&vector,
			&emb.CreatedAt,
			&emb.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("FindByArticleID: Scan: %w", err)
		}

		// Convert pgvector.Vector to []float32
		emb.EmbeddingType = entity.EmbeddingType(embType)
		emb.Provider = entity.EmbeddingProvider(provider)
		emb.Embedding = vector.Slice()

		embeddings = append(embeddings, emb)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("FindByArticleID: %w", err)
	}

	return embeddings, nil
}

// DeleteByArticleID removes all embeddings associated with an article.
// Returns the number of deleted rows.
func (repo *ArticleEmbeddingRepo) DeleteByArticleID(ctx context.Context, articleID int64) (int64, error) {
	const query = `DELETE FROM article_embeddings WHERE article_id = $1`

	result, err := repo.db.ExecContext(ctx, query, articleID)
	if err != nil {
		return 0, fmt.Errorf("DeleteByArticleID: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("DeleteByArticleID: RowsAffected: %w", err)
	}

	return count, nil
}

// SearchSimilar finds articles with embeddings similar to the provided vector.
// Uses cosine distance operator (<=>) for similarity comparison.
func (repo *ArticleEmbeddingRepo) SearchSimilar(ctx context.Context, embedding []float32, embeddingType entity.EmbeddingType, limit int) ([]repository.SimilarArticle, error) {
	// Apply timeout to search query
	searchCtx, cancel := context.WithTimeout(ctx, DefaultSearchTimeout)
	defer cancel()

	// Validate and apply limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	// Convert []float32 to pgvector.Vector
	vector := pgvector.NewVector(embedding)

	const query = `
SELECT article_id, 1 - (embedding <=> $1) AS similarity
FROM article_embeddings
WHERE embedding_type = $2
ORDER BY embedding <=> $1
LIMIT $3`

	rows, err := repo.db.QueryContext(searchCtx, query, vector, string(embeddingType), limit)
	if err != nil {
		return nil, fmt.Errorf("SearchSimilar: %w", err)
	}
	defer func() { _ = rows.Close() }()

	results := make([]repository.SimilarArticle, 0, limit)
	for rows.Next() {
		var result repository.SimilarArticle
		err := rows.Scan(&result.ArticleID, &result.Similarity)
		if err != nil {
			return nil, fmt.Errorf("SearchSimilar: Scan: %w", err)
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("SearchSimilar: %w", err)
	}

	return results, nil
}
