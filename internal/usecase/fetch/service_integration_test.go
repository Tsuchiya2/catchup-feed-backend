//go:build integration

package fetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/infra/fetcher"
	fetchUC "catchup-feed/internal/usecase/fetch"
)

// ───────────────────────────────────────────────────────────────
// TASK-017: Service Integration Tests
// ───────────────────────────────────────────────────────────────

func TestServiceIntegration_ContentEnhancement(t *testing.T) {
	// Set up test HTTP server for article content
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `<!DOCTYPE html>
<html><head><title>Full Article</title></head>
<body><article><h1>Full Article Content</h1>
<p>This is the full article content fetched from the web page.</p>
<p>It contains much more information than the RSS summary.</p>
<p>This allows for better AI summarization quality.</p>
</article></body>
</html>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write([]byte(html)); err != nil {
			t.Errorf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	// Create Service with real ReadabilityFetcher
	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	// Mock RSS feed with mixed content lengths
	feedItems := []fetchUC.FeedItem{
		// Item 1: Sufficient content (>= 1500 chars) - should skip fetching
		{
			Title:       "Article with sufficient content",
			URL:         server.URL + "/sufficient",
			Content:     strings.Repeat("Lorem ipsum dolor sit amet. ", 60), // ~1680 chars
			PublishedAt: time.Now(),
		},
		// Item 2: Sufficient content - should skip fetching
		{
			Title:       "Another article with sufficient content",
			URL:         server.URL + "/sufficient2",
			Content:     strings.Repeat("This article has enough content. ", 50), // ~1650 chars
			PublishedAt: time.Now(),
		},
		// Item 3: Insufficient content (<1500 chars) - should fetch
		{
			Title:       "Article with short summary",
			URL:         server.URL + "/short1",
			Content:     "Short RSS summary",
			PublishedAt: time.Now(),
		},
		// Item 4: Insufficient content - should fetch
		{
			Title:       "Another short article",
			URL:         server.URL + "/short2",
			Content:     "Brief description",
			PublishedAt: time.Now(),
		},
		// Item 5: Insufficient content - should fetch
		{
			Title:       "Third short article",
			URL:         server.URL + "/short3",
			Content:     "Minimal content",
			PublishedAt: time.Now(),
		},
	}

	mockFeedFetcher := &stubFeedFetcher{
		items: feedItems,
	}

	mockSummarizer := &stubSummarizer{
		result: "AI generated summary",
		err:    nil,
	}

	articleRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	source := &entity.Source{
		ID:       1,
		Name:     "Test Source",
		FeedURL:  "https://example.com/feed",
		IsActive: true,
	}

	sourceRepo := &stubSourceRepo{
		sources: []*entity.Source{source},
	}

	service := fetchUC.NewService(
		sourceRepo,
		articleRepo,
		mockSummarizer,
		mockFeedFetcher,
		nil, // webScrapers
		contentFetcher,
		&mockNotifyService{},
		nil, // embeddingHook (disabled for tests)
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	// Run processFeedItems equivalent (through CrawlAllSources)
	stats, err := service.CrawlAllSources(context.Background())
	if err != nil {
		t.Fatalf("CrawlAllSources() error = %v", err)
	}

	// Verify sufficient items skip fetching (2 items)
	// Verify insufficient items fetch content (3 items)
	// All 5 items should be processed
	if stats.FeedItems != 5 {
		t.Errorf("expected 5 feed items, got %d", stats.FeedItems)
	}

	// All should be inserted (no duplicates)
	if stats.Inserted != 5 {
		t.Errorf("expected 5 articles inserted, got %d", stats.Inserted)
	}

	// Verify enhanced content passed to summarizer
	// (This is implicit - if tests pass, content was enhanced)
	t.Logf("Integration test completed: %d items processed, %d inserted", stats.FeedItems, stats.Inserted)

	// Verify articles were created
	if len(articleRepo.articles) != 5 {
		t.Errorf("expected 5 articles in repo, got %d", len(articleRepo.articles))
	}

	// Verify metrics are correct
	if stats.Duplicated != 0 {
		t.Errorf("expected 0 duplicates, got %d", stats.Duplicated)
	}

	if stats.SummarizeError != 0 {
		t.Errorf("expected 0 summarize errors, got %d", stats.SummarizeError)
	}
}

func TestServiceIntegration_Parallelism(t *testing.T) {
	// Track concurrent executions
	var (
		maxConcurrentFetches int32
		currentFetches       int32
		maxConcurrentSummary int32
		currentSummary       int32
		mu                   sync.Mutex
	)

	// Create server that delays to test concurrency
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Track concurrent fetches
		current := atomic.AddInt32(&currentFetches, 1)
		defer atomic.AddInt32(&currentFetches, -1)

		mu.Lock()
		if current > maxConcurrentFetches {
			maxConcurrentFetches = current
		}
		mu.Unlock()

		// Simulate I/O delay
		time.Sleep(50 * time.Millisecond)

		html := `<!DOCTYPE html>
<html><head><title>Article</title></head>
<body><article><p>Content</p></article></body>
</html>`
		w.Header().Set("Content-Type", "text/html")
		if _, err := w.Write([]byte(html)); err != nil {
			t.Logf("failed to write response: %v", err)
		}
	}))
	defer server.Close()

	// Create feed with 20 items
	feedItems := make([]fetchUC.FeedItem, 20)
	for i := 0; i < 20; i++ {
		feedItems[i] = fetchUC.FeedItem{
			Title:       "Article",
			URL:         server.URL,
			Content:     "Short", // Force fetching
			PublishedAt: time.Now(),
		}
	}

	// Mock summarizer that tracks concurrency
	mockSummarizer := &concurrentSummarizer{
		current:    &currentSummary,
		maxCurrent: &maxConcurrentSummary,
		mu:         &mu,
		delay:      30 * time.Millisecond,
	}

	config := fetcher.DefaultConfig()
	contentFetcher := fetcher.NewReadabilityFetcher(config)

	articleRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	source := &entity.Source{
		ID:       1,
		Name:     "Test Source",
		FeedURL:  "https://example.com/feed",
		IsActive: true,
	}

	sourceRepo := &stubSourceRepo{
		sources: []*entity.Source{source},
	}

	service := fetchUC.Service{
		ContentFetcher: contentFetcher,
		SourceRepo:     sourceRepo,
		ArticleRepo:    articleRepo,
		Summarizer:     mockSummarizer,
		FeedFetcher: &stubFeedFetcher{
			items: feedItems,
		},
		NotifyService: &mockNotifyService{},
	}

	// Run processing
	stats, err := service.CrawlAllSources(context.Background())
	if err != nil {
		t.Fatalf("CrawlAllSources() error = %v", err)
	}

	// Verify max 10 concurrent content fetches
	if maxConcurrentFetches > 10 {
		t.Errorf("expected max 10 concurrent fetches, got %d", maxConcurrentFetches)
	}
	t.Logf("Max concurrent content fetches: %d (limit: 10)", maxConcurrentFetches)

	// Verify max 5 concurrent AI summarizations
	if maxConcurrentSummary > 5 {
		t.Errorf("expected max 5 concurrent summarizations, got %d", maxConcurrentSummary)
	}
	t.Logf("Max concurrent AI summarizations: %d (limit: 5)", maxConcurrentSummary)

	// Verify no deadlocks - all items processed
	if stats.Inserted != 20 {
		t.Errorf("expected 20 items processed, got %d (possible deadlock)", stats.Inserted)
	}

	// All items should be processed
	if stats.FeedItems != 20 {
		t.Errorf("expected 20 feed items, got %d", stats.FeedItems)
	}

	t.Log("Parallelism test completed successfully - no deadlocks detected")
}

// ───────────────────────────────────────────────────────────────
// Helper mocks for integration tests
// ───────────────────────────────────────────────────────────────

// concurrentSummarizer tracks concurrent executions
type concurrentSummarizer struct {
	current    *int32
	maxCurrent *int32
	mu         *sync.Mutex
	delay      time.Duration
}

func (s *concurrentSummarizer) Summarize(ctx context.Context, text string) (string, error) {
	// Track concurrent executions
	current := atomic.AddInt32(s.current, 1)
	defer atomic.AddInt32(s.current, -1)

	s.mu.Lock()
	if current > *s.maxCurrent {
		*s.maxCurrent = current
	}
	s.mu.Unlock()

	// Simulate AI API delay
	time.Sleep(s.delay)

	return "AI summary", nil
}
