package fetch_test

import (
	"context"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	fetchUC "catchup-feed/internal/usecase/fetch"
)

// BenchmarkCrawlAllSources_SmallFeed measures performance with a single source and 10 items
func BenchmarkCrawlAllSources_SmallFeed(b *testing.B) {
	ctx := context.Background()
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	artRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	// 10 items
	items := make([]fetchUC.FeedItem, 10)
	for i := 0; i < 10; i++ {
		items[i] = fetchUC.FeedItem{
			Title:       "Article " + string(rune('0'+i)),
			URL:         "https://example.com/article" + string(rune('0'+i)),
			Content:     "Content for article " + string(rune('0'+i)),
			PublishedAt: now,
		}
	}

	fetcher := &stubFeedFetcher{items: items}
	summarizer := &stubSummarizer{result: "Test summary"}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		nil, // webScrapers
		&mockNotifyService{},
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.CrawlAllSources(ctx)
		// Reset for next iteration
		artRepo.articles = nil
		artRepo.nextID = 0
	}
}

// BenchmarkCrawlAllSources_LargeFeed measures performance with a single source and 100 items
func BenchmarkCrawlAllSources_LargeFeed(b *testing.B) {
	ctx := context.Background()
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	artRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	// 100 items (typical RSS feed size)
	items := make([]fetchUC.FeedItem, 100)
	for i := 0; i < 100; i++ {
		items[i] = fetchUC.FeedItem{
			Title:       "Article Title Lorem Ipsum Dolor Sit Amet",
			URL:         "https://example.com/article-" + string(rune('0'+i%10)),
			Content:     "This is a longer content for article to simulate real-world scenario with more text that needs to be summarized by AI service.",
			PublishedAt: now,
		}
	}

	fetcher := &stubFeedFetcher{items: items}
	summarizer := &stubSummarizer{result: "AI-generated summary of the article content"}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		nil, // webScrapers
		&mockNotifyService{},
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.CrawlAllSources(ctx)
		// Reset for next iteration
		artRepo.articles = nil
		artRepo.nextID = 0
	}
}

// BenchmarkCrawlAllSources_MultipleSources measures performance with 5 sources
func BenchmarkCrawlAllSources_MultipleSources(b *testing.B) {
	ctx := context.Background()
	now := time.Now()

	sources := make([]*entity.Source, 5)
	for i := 0; i < 5; i++ {
		sources[i] = &entity.Source{
			ID:      int64(i + 1),
			FeedURL: "https://example.com/feed" + string(rune('0'+i)),
			Active:  true,
		}
	}

	srcRepo := &stubSourceRepo{sources: sources}

	artRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	// 20 items per source
	items := make([]fetchUC.FeedItem, 20)
	for i := 0; i < 20; i++ {
		items[i] = fetchUC.FeedItem{
			Title:       "Article Title",
			URL:         "https://example.com/article-" + string(rune('0'+i%10)),
			Content:     "Article content that will be summarized.",
			PublishedAt: now,
		}
	}

	fetcher := &stubFeedFetcher{items: items}
	summarizer := &stubSummarizer{result: "Summary"}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		nil, // webScrapers
		&mockNotifyService{},
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.CrawlAllSources(ctx)
		// Reset for next iteration
		artRepo.articles = nil
		artRepo.nextID = 0
	}
}

// BenchmarkCrawlAllSources_WithDuplicates measures performance with 50% duplicate URLs
func BenchmarkCrawlAllSources_WithDuplicates(b *testing.B) {
	ctx := context.Background()
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	// Mark 50% of URLs as existing
	existsMap := make(map[string]bool)
	for i := 0; i < 50; i++ {
		existsMap["https://example.com/article-"+string(rune('0'+i%10))] = true
	}

	artRepo := &stubArticleRepo{
		existsMap: existsMap,
	}

	// 100 items
	items := make([]fetchUC.FeedItem, 100)
	for i := 0; i < 100; i++ {
		items[i] = fetchUC.FeedItem{
			Title:       "Article Title",
			URL:         "https://example.com/article-" + string(rune('0'+i%10)),
			Content:     "Article content.",
			PublishedAt: now,
		}
	}

	fetcher := &stubFeedFetcher{items: items}
	summarizer := &stubSummarizer{result: "Summary"}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		nil, // webScrapers
		&mockNotifyService{},
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = svc.CrawlAllSources(ctx)
		// Reset for next iteration
		artRepo.articles = nil
		artRepo.nextID = 0
	}
}

// BenchmarkExistsByURLBatch_Preallocation measures the impact of slice preallocation
// This benchmark demonstrates the performance benefit of preallocating slices
func BenchmarkExistsByURLBatch_Preallocation(b *testing.B) {
	urls := make([]string, 100)
	for i := 0; i < 100; i++ {
		urls[i] = "https://example.com/article-" + string(rune('0'+i%10))
	}

	b.Run("WithPreallocation", func(b *testing.B) {
		b.ResetTimer()
		var result []string
		for i := 0; i < b.N; i++ {
			// Simulates optimized code with preallocation (as implemented in the codebase)
			result = make([]string, 0, len(urls))
			result = append(result, urls...)
		}
		_ = result // Prevent compiler optimization
	})

	b.Run("WithoutPreallocation", func(b *testing.B) {
		b.ResetTimer()
		var result []string
		for i := 0; i < b.N; i++ {
			// Simulates unoptimized code without preallocation
			result = nil
			result = append(result, urls...)
		}
		_ = result // Prevent compiler optimization
	})
}
