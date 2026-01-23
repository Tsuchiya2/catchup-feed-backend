package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"catchup-feed/internal/domain/entity"
)

// BenchmarkArticleEmbeddingRepo_Integration runs benchmarks against a real PostgreSQL database.
// These tests require DATABASE_URL environment variable to be set.
// Run with: DATABASE_URL=postgres://... go test -bench=BenchmarkArticleEmbeddingRepo -benchtime=10s -run=^$
//
// Prerequisites:
// 1. PostgreSQL with pgvector extension
// 2. article_embeddings table created (via MigrateUp)
// 3. articles table with test data

func skipIfNoDatabase(b *testing.B) *sql.DB {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		b.Skip("DATABASE_URL not set, skipping integration benchmark")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		b.Fatalf("Failed to connect to database: %v", err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		// Close the connection before skipping to avoid resource leak
		_ = db.Close()
		b.Skipf("Failed to ping database: %v", err)
	}

	return db
}

// BenchmarkArticleEmbeddingRepo_Upsert_Integration benchmarks Upsert against real database.
func BenchmarkArticleEmbeddingRepo_Upsert_Integration(b *testing.B) {
	db := skipIfNoDatabase(b)
	defer func() { _ = db.Close() }()

	repo := NewArticleEmbeddingRepo(db)
	ctx := context.Background()

	// Generate test embedding
	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) / 1536.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := &entity.ArticleEmbedding{
			ArticleID:     int64(i%1000 + 1), // Cycle through article IDs 1-1000
			EmbeddingType: entity.EmbeddingTypeContent,
			Provider:      entity.EmbeddingProviderOpenAI,
			Model:         "text-embedding-3-small",
			Dimension:     1536,
			Embedding:     embedding,
		}
		if err := repo.Upsert(ctx, e); err != nil {
			b.Logf("Upsert error (may be expected if article doesn't exist): %v", err)
		}
	}
}

// BenchmarkArticleEmbeddingRepo_FindByArticleID_Integration benchmarks FindByArticleID.
func BenchmarkArticleEmbeddingRepo_FindByArticleID_Integration(b *testing.B) {
	db := skipIfNoDatabase(b)
	defer func() { _ = db.Close() }()

	repo := NewArticleEmbeddingRepo(db)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = repo.FindByArticleID(ctx, int64(i%1000+1))
	}
}

// BenchmarkArticleEmbeddingRepo_SearchSimilar_Integration benchmarks SearchSimilar.
func BenchmarkArticleEmbeddingRepo_SearchSimilar_Integration(b *testing.B) {
	db := skipIfNoDatabase(b)
	defer func() { _ = db.Close() }()

	repo := NewArticleEmbeddingRepo(db)
	ctx := context.Background()

	// Generate query embedding
	queryEmbedding := make([]float32, 1536)
	for i := range queryEmbedding {
		queryEmbedding[i] = float32(i) / 1536.0
	}

	limits := []int{10, 50, 100}
	for _, limit := range limits {
		b.Run(fmt.Sprintf("limit_%d", limit), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = repo.SearchSimilar(ctx, queryEmbedding, entity.EmbeddingTypeContent, limit)
			}
		})
	}
}

// BenchmarkArticleEmbeddingRepo_SearchSimilar_Parallel_Integration benchmarks concurrent searches.
func BenchmarkArticleEmbeddingRepo_SearchSimilar_Parallel_Integration(b *testing.B) {
	db := skipIfNoDatabase(b)
	defer func() { _ = db.Close() }()

	// Configure connection pool for parallel benchmark
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(25)

	repo := NewArticleEmbeddingRepo(db)
	ctx := context.Background()

	// Generate query embedding
	queryEmbedding := make([]float32, 1536)
	for i := range queryEmbedding {
		queryEmbedding[i] = float32(i) / 1536.0
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = repo.SearchSimilar(ctx, queryEmbedding, entity.EmbeddingTypeContent, 10)
		}
	})
}

// BenchmarkArticleEmbeddingRepo_MixedWorkload_Integration simulates realistic mixed workload.
func BenchmarkArticleEmbeddingRepo_MixedWorkload_Integration(b *testing.B) {
	db := skipIfNoDatabase(b)
	defer func() { _ = db.Close() }()

	repo := NewArticleEmbeddingRepo(db)
	ctx := context.Background()

	embedding := make([]float32, 1536)
	for i := range embedding {
		embedding[i] = float32(i) / 1536.0
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		articleID := int64(i%1000 + 1)

		switch i % 10 {
		case 0, 1: // 20% writes
			e := &entity.ArticleEmbedding{
				ArticleID:     articleID,
				EmbeddingType: entity.EmbeddingTypeContent,
				Provider:      entity.EmbeddingProviderOpenAI,
				Model:         "text-embedding-3-small",
				Dimension:     1536,
				Embedding:     embedding,
			}
			_ = repo.Upsert(ctx, e)
		case 2, 3, 4: // 30% reads
			_, _ = repo.FindByArticleID(ctx, articleID)
		default: // 50% searches
			_, _ = repo.SearchSimilar(ctx, embedding, entity.EmbeddingTypeContent, 10)
		}
	}
}
