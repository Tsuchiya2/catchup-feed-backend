package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"

	"github.com/stretchr/testify/assert"
)

func TestNewEmbeddingHook(t *testing.T) {
	provider := &MockAIProvider{}
	hook := NewEmbeddingHook(provider, true)

	assert.NotNil(t, hook)
	assert.True(t, hook.aiEnabled)
}

func TestNewEmbeddingHook_AIDisabled(t *testing.T) {
	provider := &MockAIProvider{}
	hook := NewEmbeddingHook(provider, false)

	assert.NotNil(t, hook)
	assert.False(t, hook.aiEnabled)
}

func TestEmbeddingHook_EmbedArticleAsync_Success(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	embedCalled := false
	provider := &MockAIProvider{
		embedFn: func(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
			embedCalled = true
			assert.Equal(t, int64(123), req.ArticleID)
			assert.Equal(t, "Test Article", req.Title)
			assert.Equal(t, "Test content", req.Content)
			assert.Equal(t, "https://example.com/article", req.URL)
			wg.Done()
			return &EmbedResponse{Success: true, Dimension: 768}, nil
		},
	}

	hook := NewEmbeddingHook(provider, true)

	article := &entity.Article{
		ID:      123,
		Title:   "Test Article",
		Summary: "Test content",
		URL:     "https://example.com/article",
	}

	hook.EmbedArticleAsync(context.Background(), article)

	// Wait for async completion with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		assert.True(t, embedCalled)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for embedding")
	}
}

func TestEmbeddingHook_EmbedArticleAsync_AIDisabled(t *testing.T) {
	embedCalled := false
	provider := &MockAIProvider{
		embedFn: func(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
			embedCalled = true
			return &EmbedResponse{Success: true}, nil
		},
	}

	hook := NewEmbeddingHook(provider, false)

	article := &entity.Article{
		ID:    123,
		Title: "Test Article",
	}

	hook.EmbedArticleAsync(context.Background(), article)

	// Give some time for goroutine to potentially execute
	time.Sleep(100 * time.Millisecond)

	assert.False(t, embedCalled, "Embed should not be called when AI is disabled")
}

func TestEmbeddingHook_EmbedArticleAsync_NilArticle(t *testing.T) {
	embedCalled := false
	provider := &MockAIProvider{
		embedFn: func(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
			embedCalled = true
			return &EmbedResponse{Success: true}, nil
		},
	}

	hook := NewEmbeddingHook(provider, true)

	hook.EmbedArticleAsync(context.Background(), nil)

	// Give some time for goroutine to potentially execute
	time.Sleep(100 * time.Millisecond)

	assert.False(t, embedCalled, "Embed should not be called for nil article")
}

func TestEmbeddingHook_EmbedArticleAsync_ProviderError(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	provider := &MockAIProvider{
		embedFn: func(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
			defer wg.Done()
			return nil, errors.New("provider error")
		},
	}

	hook := NewEmbeddingHook(provider, true)

	article := &entity.Article{
		ID:    123,
		Title: "Test Article",
	}

	// Should not panic
	hook.EmbedArticleAsync(context.Background(), article)

	// Wait for async completion with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - error was handled gracefully
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for embedding error handling")
	}
}

func TestEmbeddingHook_EmbedArticleAsync_ResponseNotSuccess(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	provider := &MockAIProvider{
		embedFn: func(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
			defer wg.Done()
			return &EmbedResponse{
				Success:      false,
				ErrorMessage: "embedding failed",
			}, nil
		},
	}

	hook := NewEmbeddingHook(provider, true)

	article := &entity.Article{
		ID:    123,
		Title: "Test Article",
	}

	// Should handle non-success response gracefully
	hook.EmbedArticleAsync(context.Background(), article)

	// Wait for async completion with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - error was handled gracefully
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for embedding response handling")
	}
}

func TestEmbeddingHook_EmbedArticleAsync_ExtractsRequestID(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)

	provider := &MockAIProvider{
		embedFn: func(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
			defer wg.Done()
			return &EmbedResponse{Success: true, Dimension: 768}, nil
		},
	}

	hook := NewEmbeddingHook(provider, true)

	article := &entity.Article{
		ID:    123,
		Title: "Test Article",
	}

	// Context with request ID
	ctx := context.WithValue(context.Background(), requestIDKey, "test-request-id-456")
	hook.EmbedArticleAsync(ctx, article)

	// Wait for async completion with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for embedding")
	}
}

func TestEmbeddingHook_EmbedArticleAsync_NonBlocking(t *testing.T) {
	provider := &MockAIProvider{
		embedFn: func(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
			// Simulate slow embedding
			time.Sleep(1 * time.Second)
			return &EmbedResponse{Success: true, Dimension: 768}, nil
		},
	}

	hook := NewEmbeddingHook(provider, true)

	article := &entity.Article{
		ID:    123,
		Title: "Test Article",
	}

	start := time.Now()
	hook.EmbedArticleAsync(context.Background(), article)
	elapsed := time.Since(start)

	// Should return almost immediately (non-blocking)
	assert.Less(t, elapsed, 100*time.Millisecond, "EmbedArticleAsync should be non-blocking")
}

func TestEmbeddingHook_EmbedArticleAsync_ConcurrentCalls(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	var wg sync.WaitGroup
	numArticles := 10
	wg.Add(numArticles)

	provider := &MockAIProvider{
		embedFn: func(ctx context.Context, req EmbedRequest) (*EmbedResponse, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			wg.Done()
			return &EmbedResponse{Success: true, Dimension: 768}, nil
		},
	}

	hook := NewEmbeddingHook(provider, true)

	// Spawn multiple concurrent embeddings
	for i := range numArticles {
		article := &entity.Article{
			ID:    int64(i),
			Title: "Test Article",
		}
		hook.EmbedArticleAsync(context.Background(), article)
	}

	// Wait for all completions with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		mu.Lock()
		assert.Equal(t, numArticles, callCount, "All articles should be embedded")
		mu.Unlock()
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for concurrent embeddings")
	}
}
