package grpc_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"catchup-feed/internal/domain/entity"
	grpcserver "catchup-feed/internal/interface/grpc"
	pb "catchup-feed/internal/interface/grpc/pb/embedding"
	"catchup-feed/internal/repository"
	"catchup-feed/tests/fixtures"
)

/* ─────────────────────────── Mock Repository ─────────────────────────── */

type MockEmbeddingRepository struct {
	mock.Mock
}

func (m *MockEmbeddingRepository) Upsert(ctx context.Context, embedding *entity.ArticleEmbedding) error {
	args := m.Called(ctx, embedding)
	return args.Error(0)
}

func (m *MockEmbeddingRepository) FindByArticleID(ctx context.Context, articleID int64) ([]*entity.ArticleEmbedding, error) {
	args := m.Called(ctx, articleID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*entity.ArticleEmbedding), args.Error(1)
}

func (m *MockEmbeddingRepository) SearchSimilar(ctx context.Context, embedding []float32, embeddingType entity.EmbeddingType, limit int) ([]repository.SimilarArticle, error) {
	args := m.Called(ctx, embedding, embeddingType, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]repository.SimilarArticle), args.Error(1)
}

func (m *MockEmbeddingRepository) DeleteByArticleID(ctx context.Context, articleID int64) (int64, error) {
	args := m.Called(ctx, articleID)
	val := args.Get(0)
	if val == nil {
		return 0, args.Error(1)
	}
	return val.(int64), args.Error(1)
}

/* ─────────────────────────── StoreEmbedding Tests ─────────────────────────── */

func TestEmbeddingServer_StoreEmbedding_Success(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	embedding := fixtures.GenerateTestVector(1536, 0.1)

	mockRepo.On("Upsert", mock.Anything, mock.MatchedBy(func(e *entity.ArticleEmbedding) bool {
		return e.ArticleID == 100 &&
			e.EmbeddingType == entity.EmbeddingTypeContent &&
			e.Provider == entity.EmbeddingProviderOpenAI
	})).Return(nil).Run(func(args mock.Arguments) {
		e := args.Get(1).(*entity.ArticleEmbedding)
		e.ID = 1
	})

	req := &pb.StoreEmbeddingRequest{
		ArticleId:     100,
		EmbeddingType: "content",
		Provider:      "openai",
		Model:         "text-embedding-3-small",
		Dimension:     1536,
		Embedding:     embedding,
	}

	resp, err := server.StoreEmbedding(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, int64(1), resp.EmbeddingId)
	assert.Empty(t, resp.ErrorMessage)
	mockRepo.AssertExpectations(t)
}

func TestEmbeddingServer_StoreEmbedding_InvalidArticleID(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	tests := []struct {
		name      string
		articleID int64
	}{
		{"zero article_id", 0},
		{"negative article_id", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &pb.StoreEmbeddingRequest{
				ArticleId:     tt.articleID,
				EmbeddingType: "content",
				Provider:      "openai",
				Model:         "text-embedding-3-small",
				Dimension:     3,
				Embedding:     []float32{0.1, 0.2, 0.3},
			}

			resp, err := server.StoreEmbedding(context.Background(), req)

			assert.NoError(t, err) // No gRPC error
			assert.NotNil(t, resp)
			assert.False(t, resp.Success)
			assert.Contains(t, resp.ErrorMessage, "article_id")
		})
	}
}

func TestEmbeddingServer_StoreEmbedding_EmptyEmbedding(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	req := &pb.StoreEmbeddingRequest{
		ArticleId:     100,
		EmbeddingType: "content",
		Provider:      "openai",
		Model:         "text-embedding-3-small",
		Dimension:     0,
		Embedding:     []float32{},
	}

	resp, err := server.StoreEmbedding(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "embedding")
}

func TestEmbeddingServer_StoreEmbedding_DimensionMismatch(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	req := &pb.StoreEmbeddingRequest{
		ArticleId:     100,
		EmbeddingType: "content",
		Provider:      "openai",
		Model:         "text-embedding-3-small",
		Dimension:     100,                       // mismatched
		Embedding:     []float32{0.1, 0.2, 0.3}, // only 3 elements
	}

	resp, err := server.StoreEmbedding(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.ErrorMessage, "dimension")
}

func TestEmbeddingServer_StoreEmbedding_RepositoryError(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	mockRepo.On("Upsert", mock.Anything, mock.Anything).
		Return(errors.New("database connection failed"))

	req := &pb.StoreEmbeddingRequest{
		ArticleId:     100,
		EmbeddingType: "content",
		Provider:      "openai",
		Model:         "text-embedding-3-small",
		Dimension:     3,
		Embedding:     []float32{0.1, 0.2, 0.3},
	}

	resp, err := server.StoreEmbedding(context.Background(), req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Success)
	// Error message is sanitized for security - should not expose internal details
	assert.Contains(t, resp.ErrorMessage, "failed to store embedding")
	mockRepo.AssertExpectations(t)
}

/* ─────────────────────────── GetEmbeddings Tests ─────────────────────────── */

func TestEmbeddingServer_GetEmbeddings_Success(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	now := time.Now()
	embeddings := []*entity.ArticleEmbedding{
		{
			ID:            1,
			ArticleID:     100,
			EmbeddingType: entity.EmbeddingTypeContent,
			Provider:      entity.EmbeddingProviderOpenAI,
			Model:         "text-embedding-3-small",
			Dimension:     3,
			Embedding:     []float32{0.1, 0.2, 0.3},
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}

	mockRepo.On("FindByArticleID", mock.Anything, int64(100)).
		Return(embeddings, nil)

	req := &pb.GetEmbeddingsRequest{
		ArticleId: 100,
	}

	resp, err := server.GetEmbeddings(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Embeddings, 1)
	assert.Equal(t, int64(1), resp.Embeddings[0].Id)
	assert.Equal(t, int64(100), resp.Embeddings[0].ArticleId)
	assert.Equal(t, "content", resp.Embeddings[0].EmbeddingType)
	assert.Equal(t, "openai", resp.Embeddings[0].Provider)
	mockRepo.AssertExpectations(t)
}

func TestEmbeddingServer_GetEmbeddings_Empty(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	mockRepo.On("FindByArticleID", mock.Anything, int64(999)).
		Return([]*entity.ArticleEmbedding{}, nil)

	req := &pb.GetEmbeddingsRequest{
		ArticleId: 999,
	}

	resp, err := server.GetEmbeddings(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp.Embeddings)
	mockRepo.AssertExpectations(t)
}

func TestEmbeddingServer_GetEmbeddings_InvalidArticleID(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	req := &pb.GetEmbeddingsRequest{
		ArticleId: 0,
	}

	resp, err := server.GetEmbeddings(context.Background(), req)

	assert.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestEmbeddingServer_GetEmbeddings_RepositoryError(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	mockRepo.On("FindByArticleID", mock.Anything, int64(100)).
		Return(nil, errors.New("database error"))

	req := &pb.GetEmbeddingsRequest{
		ArticleId: 100,
	}

	resp, err := server.GetEmbeddings(context.Background(), req)

	assert.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockRepo.AssertExpectations(t)
}

/* ─────────────────────────── SearchSimilar Tests ─────────────────────────── */

func TestEmbeddingServer_SearchSimilar_Success(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	queryEmbedding := fixtures.GenerateTestVector(1536, 0.1)
	results := []repository.SimilarArticle{
		{ArticleID: 1, Similarity: 0.95},
		{ArticleID: 2, Similarity: 0.85},
	}

	mockRepo.On("SearchSimilar", mock.Anything, queryEmbedding, entity.EmbeddingTypeContent, 10).
		Return(results, nil)

	req := &pb.SearchSimilarRequest{
		Embedding:     queryEmbedding,
		EmbeddingType: "content",
		Limit:         10,
	}

	resp, err := server.SearchSimilar(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Articles, 2)
	assert.Equal(t, int64(1), resp.Articles[0].ArticleId)
	assert.InDelta(t, float32(0.95), resp.Articles[0].Similarity, 0.001)
	assert.Equal(t, int64(2), resp.Articles[1].ArticleId)
	assert.InDelta(t, float32(0.85), resp.Articles[1].Similarity, 0.001)
	mockRepo.AssertExpectations(t)
}

func TestEmbeddingServer_SearchSimilar_DefaultLimit(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	queryEmbedding := fixtures.GenerateTestVector(1536, 0.1)

	mockRepo.On("SearchSimilar", mock.Anything, queryEmbedding, entity.EmbeddingTypeContent, 10).
		Return([]repository.SimilarArticle{}, nil)

	req := &pb.SearchSimilarRequest{
		Embedding:     queryEmbedding,
		EmbeddingType: "content",
		Limit:         0, // Should default to 10
	}

	resp, err := server.SearchSimilar(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	mockRepo.AssertExpectations(t)
}

func TestEmbeddingServer_SearchSimilar_MaxLimit(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	queryEmbedding := fixtures.GenerateTestVector(1536, 0.1)

	mockRepo.On("SearchSimilar", mock.Anything, queryEmbedding, entity.EmbeddingTypeContent, 100).
		Return([]repository.SimilarArticle{}, nil)

	req := &pb.SearchSimilarRequest{
		Embedding:     queryEmbedding,
		EmbeddingType: "content",
		Limit:         150, // Should be capped to 100
	}

	resp, err := server.SearchSimilar(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	mockRepo.AssertExpectations(t)
}

func TestEmbeddingServer_SearchSimilar_EmptyEmbedding(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	req := &pb.SearchSimilarRequest{
		Embedding:     []float32{},
		EmbeddingType: "content",
		Limit:         10,
	}

	resp, err := server.SearchSimilar(context.Background(), req)

	assert.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestEmbeddingServer_SearchSimilar_InvalidEmbeddingType(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	queryEmbedding := fixtures.GenerateTestVector(1536, 0.1)

	req := &pb.SearchSimilarRequest{
		Embedding:     queryEmbedding,
		EmbeddingType: "invalid_type",
		Limit:         10,
	}

	resp, err := server.SearchSimilar(context.Background(), req)

	assert.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "invalid_type")
}

func TestEmbeddingServer_SearchSimilar_RepositoryError(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)

	queryEmbedding := fixtures.GenerateTestVector(1536, 0.1)

	mockRepo.On("SearchSimilar", mock.Anything, queryEmbedding, entity.EmbeddingTypeContent, 10).
		Return(nil, errors.New("search failed"))

	req := &pb.SearchSimilarRequest{
		Embedding:     queryEmbedding,
		EmbeddingType: "content",
		Limit:         10,
	}

	resp, err := server.SearchSimilar(context.Background(), req)

	assert.Error(t, err)
	assert.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockRepo.AssertExpectations(t)
}

/* ─────────────────────────── Constructor Test ─────────────────────────── */

func TestNewEmbeddingServer(t *testing.T) {
	mockRepo := new(MockEmbeddingRepository)
	server := grpcserver.NewEmbeddingServer(mockRepo)
	assert.NotNil(t, server)
}

/* ─────────────────────────── Benchmarks ─────────────────────────── */

// BenchmarkMockRepository is a benchmark-specific mock that avoids testify overhead.
type BenchmarkMockRepository struct{}

func (m *BenchmarkMockRepository) Upsert(_ context.Context, e *entity.ArticleEmbedding) error {
	e.ID = 1
	return nil
}

func (m *BenchmarkMockRepository) FindByArticleID(_ context.Context, _ int64) ([]*entity.ArticleEmbedding, error) {
	return []*entity.ArticleEmbedding{
		{ID: 1, ArticleID: 100, EmbeddingType: entity.EmbeddingTypeContent},
	}, nil
}

func (m *BenchmarkMockRepository) SearchSimilar(_ context.Context, _ []float32, _ entity.EmbeddingType, limit int) ([]repository.SimilarArticle, error) {
	results := make([]repository.SimilarArticle, 0, limit)
	for i := 0; i < limit && i < 10; i++ {
		results = append(results, repository.SimilarArticle{
			ArticleID:  int64(i + 1),
			Similarity: 0.95 - float64(i)*0.05,
		})
	}
	return results, nil
}

func (m *BenchmarkMockRepository) DeleteByArticleID(_ context.Context, _ int64) (int64, error) {
	return 1, nil
}

// BenchmarkEmbeddingServer_StoreEmbedding benchmarks the StoreEmbedding operation.
func BenchmarkEmbeddingServer_StoreEmbedding(b *testing.B) {
	mockRepo := &BenchmarkMockRepository{}
	server := grpcserver.NewEmbeddingServer(mockRepo)

	embedding := fixtures.GenerateTestVector(1536, 0.1)
	req := &pb.StoreEmbeddingRequest{
		ArticleId:     100,
		EmbeddingType: "content",
		Provider:      "openai",
		Model:         "text-embedding-3-small",
		Dimension:     1536,
		Embedding:     embedding,
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = server.StoreEmbedding(ctx, req)
	}
}

// BenchmarkEmbeddingServer_StoreEmbedding_Parallel benchmarks concurrent store operations.
func BenchmarkEmbeddingServer_StoreEmbedding_Parallel(b *testing.B) {
	mockRepo := &BenchmarkMockRepository{}
	server := grpcserver.NewEmbeddingServer(mockRepo)

	embedding := fixtures.GenerateTestVector(1536, 0.1)
	req := &pb.StoreEmbeddingRequest{
		ArticleId:     100,
		EmbeddingType: "content",
		Provider:      "openai",
		Model:         "text-embedding-3-small",
		Dimension:     1536,
		Embedding:     embedding,
	}

	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = server.StoreEmbedding(ctx, req)
		}
	})
}

// BenchmarkEmbeddingServer_GetEmbeddings benchmarks the GetEmbeddings operation.
func BenchmarkEmbeddingServer_GetEmbeddings(b *testing.B) {
	mockRepo := &BenchmarkMockRepository{}
	server := grpcserver.NewEmbeddingServer(mockRepo)

	req := &pb.GetEmbeddingsRequest{ArticleId: 100}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = server.GetEmbeddings(ctx, req)
	}
}

// BenchmarkEmbeddingServer_GetEmbeddings_Parallel benchmarks concurrent get operations.
func BenchmarkEmbeddingServer_GetEmbeddings_Parallel(b *testing.B) {
	mockRepo := &BenchmarkMockRepository{}
	server := grpcserver.NewEmbeddingServer(mockRepo)

	req := &pb.GetEmbeddingsRequest{ArticleId: 100}

	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = server.GetEmbeddings(ctx, req)
		}
	})
}

// BenchmarkEmbeddingServer_SearchSimilar benchmarks the SearchSimilar operation.
func BenchmarkEmbeddingServer_SearchSimilar(b *testing.B) {
	mockRepo := &BenchmarkMockRepository{}
	server := grpcserver.NewEmbeddingServer(mockRepo)

	embedding := fixtures.GenerateTestVector(1536, 0.1)
	req := &pb.SearchSimilarRequest{
		Embedding:     embedding,
		EmbeddingType: "content",
		Limit:         10,
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = server.SearchSimilar(ctx, req)
	}
}

// BenchmarkEmbeddingServer_SearchSimilar_Parallel benchmarks concurrent search operations.
func BenchmarkEmbeddingServer_SearchSimilar_Parallel(b *testing.B) {
	mockRepo := &BenchmarkMockRepository{}
	server := grpcserver.NewEmbeddingServer(mockRepo)

	embedding := fixtures.GenerateTestVector(1536, 0.1)
	req := &pb.SearchSimilarRequest{
		Embedding:     embedding,
		EmbeddingType: "content",
		Limit:         10,
	}

	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = server.SearchSimilar(ctx, req)
		}
	})
}

// BenchmarkEmbeddingServer_SearchSimilar_VaryingLimits benchmarks search with different limits.
func BenchmarkEmbeddingServer_SearchSimilar_VaryingLimits(b *testing.B) {
	limits := []int{1, 10, 50, 100}
	for _, limit := range limits {
		b.Run(fmt.Sprintf("limit_%d", limit), func(b *testing.B) {
			mockRepo := &BenchmarkMockRepository{}
			server := grpcserver.NewEmbeddingServer(mockRepo)

			embedding := fixtures.GenerateTestVector(1536, 0.1)
			req := &pb.SearchSimilarRequest{
				Embedding:     embedding,
				EmbeddingType: "content",
				Limit:         int32(limit),
			}

			ctx := context.Background()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = server.SearchSimilar(ctx, req)
			}
		})
	}
}
