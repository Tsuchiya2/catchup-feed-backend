// Package fixtures provides reusable test data generators for integration tests.
package fixtures

import (
	"time"

	"catchup-feed/internal/domain/entity"
)

// EmbeddingOption is a functional option for customizing test embeddings.
type EmbeddingOption func(*entity.ArticleEmbedding)

// NewTestEmbedding creates a valid ArticleEmbedding with sensible defaults.
// Use functional options to customize the embedding for specific test cases.
//
// Example:
//
//	embedding := NewTestEmbedding()
//	embedding := NewTestEmbedding(WithArticleID(100), WithEmbeddingType(entity.EmbeddingTypeTitle))
func NewTestEmbedding(opts ...EmbeddingOption) *entity.ArticleEmbedding {
	now := time.Now()
	e := &entity.ArticleEmbedding{
		ID:            1,
		ArticleID:     1,
		EmbeddingType: entity.EmbeddingTypeContent,
		Provider:      entity.EmbeddingProviderOpenAI,
		Model:         "text-embedding-3-small",
		Dimension:     1536,
		Embedding:     GenerateTestVector(1536, 0.1),
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// WithID sets the ID of the embedding.
func WithID(id int64) EmbeddingOption {
	return func(e *entity.ArticleEmbedding) {
		e.ID = id
	}
}

// WithArticleID sets the ArticleID of the embedding.
func WithArticleID(id int64) EmbeddingOption {
	return func(e *entity.ArticleEmbedding) {
		e.ArticleID = id
	}
}

// WithEmbeddingType sets the EmbeddingType of the embedding.
func WithEmbeddingType(t entity.EmbeddingType) EmbeddingOption {
	return func(e *entity.ArticleEmbedding) {
		e.EmbeddingType = t
	}
}

// WithProvider sets the Provider of the embedding.
func WithProvider(p entity.EmbeddingProvider) EmbeddingOption {
	return func(e *entity.ArticleEmbedding) {
		e.Provider = p
	}
}

// WithModel sets the Model of the embedding.
func WithModel(model string) EmbeddingOption {
	return func(e *entity.ArticleEmbedding) {
		e.Model = model
	}
}

// WithDimension sets the Dimension and generates a matching embedding vector.
func WithDimension(dim int32) EmbeddingOption {
	return func(e *entity.ArticleEmbedding) {
		e.Dimension = dim
		e.Embedding = GenerateTestVector(int(dim), 0.1)
	}
}

// WithEmbedding sets the Embedding vector and updates Dimension to match.
func WithEmbedding(embedding []float32) EmbeddingOption {
	return func(e *entity.ArticleEmbedding) {
		e.Embedding = embedding
		e.Dimension = int32(len(embedding))
	}
}

// WithTimestamps sets CreatedAt and UpdatedAt timestamps.
func WithTimestamps(createdAt, updatedAt time.Time) EmbeddingOption {
	return func(e *entity.ArticleEmbedding) {
		e.CreatedAt = createdAt
		e.UpdatedAt = updatedAt
	}
}

// GenerateTestVector creates a deterministic vector of the specified dimension.
// The seed value is used to generate predictable but different vectors for testing.
//
// Example:
//
//	vec := GenerateTestVector(1536, 0.1) // [0.1, 0.101, 0.102, ...]
//	vec := GenerateTestVector(1536, 0.5) // [0.5, 0.501, 0.502, ...]
func GenerateTestVector(dimension int, seed float32) []float32 {
	vec := make([]float32, dimension)
	for i := 0; i < dimension; i++ {
		vec[i] = seed + float32(i)*0.001
	}
	return vec
}

// ZeroVector creates a vector of zeros with the specified dimension.
// Useful for testing edge cases with zero vectors.
//
// Example:
//
//	vec := ZeroVector(1536) // [0.0, 0.0, 0.0, ...]
func ZeroVector(dimension int) []float32 {
	return make([]float32, dimension)
}

// UnitVector creates a unit vector with 1.0 at the specified index and 0.0 elsewhere.
// Useful for testing specific similarity calculations.
//
// Example:
//
//	vec := UnitVector(1536, 0)    // [1.0, 0.0, 0.0, ...]
//	vec := UnitVector(1536, 100)  // [0.0, ..., 1.0, 0.0, ...]
func UnitVector(dimension int, index int) []float32 {
	vec := make([]float32, dimension)
	if index >= 0 && index < dimension {
		vec[index] = 1.0
	}
	return vec
}

// NormalizedVector creates a normalized vector (unit length) from the seed.
// The resulting vector has a magnitude of 1.0, suitable for cosine similarity tests.
//
// Example:
//
//	vec := NormalizedVector(1536, 0.1)
func NormalizedVector(dimension int, seed float32) []float32 {
	vec := GenerateTestVector(dimension, seed)

	// Calculate magnitude
	var magnitude float32
	for _, v := range vec {
		magnitude += v * v
	}
	magnitude = float32(sqrt64(float64(magnitude)))

	// Normalize
	if magnitude > 0 {
		for i := range vec {
			vec[i] /= magnitude
		}
	}

	return vec
}

// sqrt64 computes the square root of a float64.
// Using a simple Newton-Raphson method to avoid importing math package.
func sqrt64(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x / 2
	for i := 0; i < 10; i++ {
		z = z - (z*z-x)/(2*z)
	}
	return z
}

// SimilarVector creates a vector similar to the base vector with a specified similarity.
// The similarity parameter ranges from 0.0 (orthogonal) to 1.0 (identical).
// Note: This is an approximation and may not produce exact similarity values.
//
// Example:
//
//	base := GenerateTestVector(1536, 0.1)
//	similar := SimilarVector(base, 0.9) // ~90% similar to base
func SimilarVector(base []float32, similarity float32) []float32 {
	dimension := len(base)
	result := make([]float32, dimension)

	// Mix the base vector with a random-ish perturbation
	perturbation := 1.0 - similarity
	for i := 0; i < dimension; i++ {
		// Add small perturbation based on index
		noise := perturbation * float32(i%10) * 0.01
		result[i] = base[i]*(1.0-perturbation) + noise
	}

	return result
}
