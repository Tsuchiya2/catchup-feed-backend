package entity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEmbeddingType_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		et       EmbeddingType
		expected bool
	}{
		{"title is valid", EmbeddingTypeTitle, true},
		{"content is valid", EmbeddingTypeContent, true},
		{"summary is valid", EmbeddingTypeSummary, true},
		{"empty is invalid", EmbeddingType(""), false},
		{"unknown is invalid", EmbeddingType("unknown"), false},
		{"uppercase is invalid", EmbeddingType("TITLE"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.et.IsValid())
		})
	}
}

func TestEmbeddingProvider_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		ep       EmbeddingProvider
		expected bool
	}{
		{"openai is valid", EmbeddingProviderOpenAI, true},
		{"voyage is valid", EmbeddingProviderVoyage, true},
		{"empty is invalid", EmbeddingProvider(""), false},
		{"unknown is invalid", EmbeddingProvider("anthropic"), false},
		{"uppercase is invalid", EmbeddingProvider("OPENAI"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.ep.IsValid())
		})
	}
}

func TestArticleEmbedding_Validate(t *testing.T) {
	validEmbedding := func() *ArticleEmbedding {
		return &ArticleEmbedding{
			ID:            1,
			ArticleID:     100,
			EmbeddingType: EmbeddingTypeContent,
			Provider:      EmbeddingProviderOpenAI,
			Model:         "text-embedding-3-small",
			Dimension:     1536,
			Embedding:     make([]float32, 1536),
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
	}

	t.Run("valid embedding passes validation", func(t *testing.T) {
		e := validEmbedding()
		err := e.Validate()
		assert.NoError(t, err)
	})

	t.Run("zero article_id fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.ArticleID = 0
		err := e.Validate()
		assert.Error(t, err)
		var validationErr *ValidationError
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "ArticleID", validationErr.Field)
	})

	t.Run("negative article_id fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.ArticleID = -1
		err := e.Validate()
		assert.Error(t, err)
		var validationErr *ValidationError
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "ArticleID", validationErr.Field)
	})

	t.Run("invalid embedding_type fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.EmbeddingType = EmbeddingType("invalid")
		err := e.Validate()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidEmbeddingType)
	})

	t.Run("empty embedding_type fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.EmbeddingType = EmbeddingType("")
		err := e.Validate()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidEmbeddingType)
	})

	t.Run("invalid provider fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.Provider = EmbeddingProvider("invalid")
		err := e.Validate()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidEmbeddingProvider)
	})

	t.Run("empty provider fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.Provider = EmbeddingProvider("")
		err := e.Validate()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidEmbeddingProvider)
	})

	t.Run("empty embedding fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.Embedding = []float32{}
		err := e.Validate()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrEmptyEmbedding)
	})

	t.Run("nil embedding fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.Embedding = nil
		err := e.Validate()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrEmptyEmbedding)
	})

	t.Run("dimension mismatch fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.Dimension = 1024 // doesn't match len(Embedding) = 1536
		err := e.Validate()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidEmbeddingDimension)
	})

	t.Run("dimension larger than embedding length fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.Dimension = 2048 // larger than len(Embedding) = 1536
		err := e.Validate()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidEmbeddingDimension)
	})

	t.Run("dimension smaller than embedding length fails validation", func(t *testing.T) {
		e := validEmbedding()
		e.Dimension = 512 // smaller than len(Embedding) = 1536
		err := e.Validate()
		assert.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidEmbeddingDimension)
	})
}

func TestArticleEmbedding_Validate_AllEmbeddingTypes(t *testing.T) {
	types := []EmbeddingType{
		EmbeddingTypeTitle,
		EmbeddingTypeContent,
		EmbeddingTypeSummary,
	}

	for _, et := range types {
		t.Run(string(et), func(t *testing.T) {
			e := &ArticleEmbedding{
				ArticleID:     1,
				EmbeddingType: et,
				Provider:      EmbeddingProviderOpenAI,
				Model:         "text-embedding-3-small",
				Dimension:     3,
				Embedding:     []float32{0.1, 0.2, 0.3},
			}
			err := e.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestArticleEmbedding_Validate_AllProviders(t *testing.T) {
	providers := []EmbeddingProvider{
		EmbeddingProviderOpenAI,
		EmbeddingProviderVoyage,
	}

	for _, p := range providers {
		t.Run(string(p), func(t *testing.T) {
			e := &ArticleEmbedding{
				ArticleID:     1,
				EmbeddingType: EmbeddingTypeContent,
				Provider:      p,
				Model:         "test-model",
				Dimension:     3,
				Embedding:     []float32{0.1, 0.2, 0.3},
			}
			err := e.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestArticleEmbedding_Struct(t *testing.T) {
	now := time.Now()
	embedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}

	e := ArticleEmbedding{
		ID:            1,
		ArticleID:     100,
		EmbeddingType: EmbeddingTypeContent,
		Provider:      EmbeddingProviderOpenAI,
		Model:         "text-embedding-3-small",
		Dimension:     5,
		Embedding:     embedding,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	assert.Equal(t, int64(1), e.ID)
	assert.Equal(t, int64(100), e.ArticleID)
	assert.Equal(t, EmbeddingTypeContent, e.EmbeddingType)
	assert.Equal(t, EmbeddingProviderOpenAI, e.Provider)
	assert.Equal(t, "text-embedding-3-small", e.Model)
	assert.Equal(t, int32(5), e.Dimension)
	assert.Equal(t, embedding, e.Embedding)
	assert.Equal(t, now, e.CreatedAt)
	assert.Equal(t, now, e.UpdatedAt)
}

func TestArticleEmbedding_ZeroValue(t *testing.T) {
	var e ArticleEmbedding

	assert.Equal(t, int64(0), e.ID)
	assert.Equal(t, int64(0), e.ArticleID)
	assert.Equal(t, EmbeddingType(""), e.EmbeddingType)
	assert.Equal(t, EmbeddingProvider(""), e.Provider)
	assert.Equal(t, "", e.Model)
	assert.Equal(t, int32(0), e.Dimension)
	assert.Nil(t, e.Embedding)
	assert.True(t, e.CreatedAt.IsZero())
	assert.True(t, e.UpdatedAt.IsZero())
}

func TestEmbeddingType_String(t *testing.T) {
	assert.Equal(t, "title", string(EmbeddingTypeTitle))
	assert.Equal(t, "content", string(EmbeddingTypeContent))
	assert.Equal(t, "summary", string(EmbeddingTypeSummary))
}

func TestEmbeddingProvider_String(t *testing.T) {
	assert.Equal(t, "openai", string(EmbeddingProviderOpenAI))
	assert.Equal(t, "voyage", string(EmbeddingProviderVoyage))
}

/* ─────────────────────────── Benchmarks ─────────────────────────── */

// BenchmarkEmbeddingType_IsValid benchmarks the EmbeddingType validation.
func BenchmarkEmbeddingType_IsValid(b *testing.B) {
	et := EmbeddingTypeContent
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = et.IsValid()
	}
}

// BenchmarkEmbeddingProvider_IsValid benchmarks the EmbeddingProvider validation.
func BenchmarkEmbeddingProvider_IsValid(b *testing.B) {
	ep := EmbeddingProviderOpenAI
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ep.IsValid()
	}
}

// BenchmarkArticleEmbedding_Validate benchmarks the full entity validation.
func BenchmarkArticleEmbedding_Validate(b *testing.B) {
	e := &ArticleEmbedding{
		ID:            1,
		ArticleID:     100,
		EmbeddingType: EmbeddingTypeContent,
		Provider:      EmbeddingProviderOpenAI,
		Model:         "text-embedding-3-small",
		Dimension:     1536,
		Embedding:     make([]float32, 1536),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Validate()
	}
}

// BenchmarkArticleEmbedding_Validate_SmallVector benchmarks validation with small vectors.
func BenchmarkArticleEmbedding_Validate_SmallVector(b *testing.B) {
	e := &ArticleEmbedding{
		ID:            1,
		ArticleID:     100,
		EmbeddingType: EmbeddingTypeContent,
		Provider:      EmbeddingProviderOpenAI,
		Model:         "text-embedding-3-small",
		Dimension:     256,
		Embedding:     make([]float32, 256),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Validate()
	}
}

// BenchmarkArticleEmbedding_Validate_LargeVector benchmarks validation with large vectors.
func BenchmarkArticleEmbedding_Validate_LargeVector(b *testing.B) {
	e := &ArticleEmbedding{
		ID:            1,
		ArticleID:     100,
		EmbeddingType: EmbeddingTypeContent,
		Provider:      EmbeddingProviderOpenAI,
		Model:         "text-embedding-3-large",
		Dimension:     3072,
		Embedding:     make([]float32, 3072),
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = e.Validate()
	}
}

// BenchmarkArticleEmbedding_Validate_Parallel benchmarks validation under concurrent load.
func BenchmarkArticleEmbedding_Validate_Parallel(b *testing.B) {
	e := &ArticleEmbedding{
		ID:            1,
		ArticleID:     100,
		EmbeddingType: EmbeddingTypeContent,
		Provider:      EmbeddingProviderOpenAI,
		Model:         "text-embedding-3-small",
		Dimension:     1536,
		Embedding:     make([]float32, 1536),
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = e.Validate()
		}
	})
}
