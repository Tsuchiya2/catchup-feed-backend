package fetch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/observability/metrics"
	"catchup-feed/internal/repository"
	"catchup-feed/internal/usecase/notify"

	"golang.org/x/sync/errgroup"
)

// scraperConfigKey is the context key for ScraperConfig.
type scraperConfigKey string

const (
	summarizerParallelism = 5 // AI summarization parallelism (rate-limited)
)

// FeedFetcher is an interface for fetching RSS/Atom feeds from a URL.
type FeedFetcher interface {
	Fetch(ctx context.Context, url string) ([]FeedItem, error)
}

// ContentFetchConfig holds configuration for content fetching behavior.
// This is passed to the Service to control parallelism and threshold settings.
type ContentFetchConfig struct {
	Parallelism int // Maximum number of concurrent content fetching operations
	Threshold   int // Minimum RSS content length before fetching full content
}

// FeedItem represents a single item from an RSS/Atom feed.
type FeedItem struct {
	Title       string
	URL         string
	Content     string
	PublishedAt time.Time
}

// EmbeddingHook is an interface for asynchronous article embedding.
// This is used to decouple the fetch service from AI implementation.
type EmbeddingHook interface {
	EmbedArticleAsync(ctx context.Context, article *entity.Article)
}

// Service provides feed crawling and article fetching use cases.
// It orchestrates the process of fetching feeds, summarizing content, and storing articles.
type Service struct {
	SourceRepo     repository.SourceRepository
	ArticleRepo    repository.ArticleRepository
	Summarizer     Summarizer
	FeedFetcher    FeedFetcher
	WebScrapers    map[string]FeedFetcher // NEW: Web scraper registry for non-RSS sources
	ContentFetcher ContentFetcher         // NEW: Content enhancement for B-rated feeds
	NotifyService  notify.Service
	EmbeddingHook  EmbeddingHook          // NEW: AI embedding hook for async embedding generation
	contentConfig  ContentFetchConfig     // Configuration for content fetching behavior
}

// Summarizer is an interface for AI-powered text summarization.
type Summarizer interface {
	Summarize(ctx context.Context, text string) (string, error)
}

// NewService creates a new fetch Service with the provided dependencies.
// This constructor ensures proper initialization of the Service with all required components.
//
// Parameters:
//   - sourceRepo: Repository for managing feed sources
//   - articleRepo: Repository for managing articles
//   - summarizer: AI service for text summarization
//   - feedFetcher: Service for fetching RSS/Atom feeds
//   - webScrapers: Map of web scrapers for non-RSS sources (can be nil to disable)
//   - contentFetcher: Service for fetching full article content (can be nil to disable)
//   - notifyService: Service for sending notifications
//   - embeddingHook: Hook for async embedding generation (can be nil to disable)
//   - contentConfig: Configuration for content fetching behavior (parallelism, threshold)
//
// Returns:
//   - Service: Configured fetch service ready to use
//
// Example:
//
//	config := ContentFetchConfig{Parallelism: 10, Threshold: 1500}
//	scrapers := scraperFactory.CreateScrapers()
//	service := NewService(sourceRepo, articleRepo, summarizer, feedFetcher, scrapers, contentFetcher, notifyService, embeddingHook, config)
func NewService(
	sourceRepo repository.SourceRepository,
	articleRepo repository.ArticleRepository,
	summarizer Summarizer,
	feedFetcher FeedFetcher,
	webScrapers map[string]FeedFetcher,
	contentFetcher ContentFetcher,
	notifyService notify.Service,
	embeddingHook EmbeddingHook,
	contentConfig ContentFetchConfig,
) Service {
	return Service{
		SourceRepo:     sourceRepo,
		ArticleRepo:    articleRepo,
		Summarizer:     summarizer,
		FeedFetcher:    feedFetcher,
		WebScrapers:    webScrapers,
		ContentFetcher: contentFetcher,
		NotifyService:  notifyService,
		EmbeddingHook:  embeddingHook,
		contentConfig:  contentConfig,
	}
}

// CrawlStats contains statistics about a crawl operation.
type CrawlStats struct {
	Sources        int
	FeedItems      int64
	Inserted       int64
	Duplicated     int64
	SummarizeError int64
	Duration       time.Duration
}

// CrawlAllSources fetches and processes articles from all active sources.
// It performs the following steps for each source:
// 1. Fetches the RSS/Atom feed
// 2. Filters out duplicate articles using batch URL checking
// 3. Summarizes article content in parallel using AI
// 4. Stores new articles in the repository
// Returns crawl statistics including counts of processed, inserted, and duplicated articles.
func (s *Service) CrawlAllSources(ctx context.Context) (*CrawlStats, error) {
	logger := slog.Default()
	startAll := time.Now()
	stats := &CrawlStats{}

	srcs, err := s.SourceRepo.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active sources: %w", err)
	}
	stats.Sources = len(srcs)

	for _, src := range srcs {
		if err := s.processSingleSource(ctx, src, stats); err != nil {
			return stats, err
		}
	}

	stats.Duration = time.Since(startAll)
	logger.Info("all sources crawl completed",
		slog.Int("sources", stats.Sources),
		slog.Int64("feed_items", stats.FeedItems),
		slog.Int64("inserted", stats.Inserted),
		slog.Int64("duplicated", stats.Duplicated),
		slog.Int64("summarize_errors", stats.SummarizeError),
		slog.Duration("duration", stats.Duration),
	)

	return stats, nil
}

// selectFetcher chooses the appropriate fetcher based on the source type.
// It returns the RSS fetcher for RSS sources, or the appropriate web scraper for other types.
// Falls back to RSS fetcher if the source type is unknown.
func (s *Service) selectFetcher(src *entity.Source) FeedFetcher {
	// Default to RSS for empty source type (backward compatibility)
	if src.SourceType == "" || src.SourceType == "RSS" {
		return s.FeedFetcher
	}

	// Look up web scraper for this source type
	if s.WebScrapers != nil {
		if fetcher, exists := s.WebScrapers[src.SourceType]; exists {
			return fetcher
		}
	}

	// Unknown source type - log warning and fallback to RSS
	slog.Warn("unknown source type, falling back to RSS fetcher",
		slog.String("source_type", src.SourceType),
		slog.Int64("source_id", src.ID),
		slog.String("source_name", src.Name))
	return s.FeedFetcher
}

// processSingleSource processes a single feed source by fetching, deduplicating,
// summarizing, and storing articles. It updates the provided stats atomically.
// Returns error only for critical failures (summarizer errors, timestamp updates).
// Logs and continues for recoverable failures (fetch errors, batch check errors).
func (s *Service) processSingleSource(ctx context.Context, src *entity.Source, stats *CrawlStats) error {
	logger := slog.Default()
	sourceStart := time.Now()

	// Select appropriate fetcher based on source type
	fetcher := s.selectFetcher(src)

	// Add scraper config to context for web scrapers
	if src.ScraperConfig != nil {
		ctx = context.WithValue(ctx, scraperConfigKey("scraper_config"), src.ScraperConfig)
	}

	feedItems, err := fetcher.Fetch(ctx, src.FeedURL)
	if err != nil {
		logger.Warn("failed to fetch feed",
			slog.Int64("source_id", src.ID),
			slog.String("feed_url", src.FeedURL),
			slog.Any("error", err))
		// Record fetch error metric
		metrics.RecordFeedCrawlError(src.ID, "fetch_failed")
		// Continue with other sources even if one fails
		return nil
	}

	if len(feedItems) == 0 {
		logger.Info("feed is empty",
			slog.Int64("source_id", src.ID),
			slog.String("feed_url", src.FeedURL))
		return nil
	}

	// N+1問題解消: 事前に全URLをバッチで存在チェック
	urls := make([]string, 0, len(feedItems))
	for _, item := range feedItems {
		urls = append(urls, item.URL)
	}
	existsMap, err := s.ArticleRepo.ExistsByURLBatch(ctx, urls)
	if err != nil {
		logger.Warn("failed to batch check URLs",
			slog.Int64("source_id", src.ID),
			slog.Any("error", err))
		// Record batch check error metric
		metrics.RecordFeedCrawlError(src.ID, "batch_check_failed")
		// Continue with other sources even if batch check fails
		return nil
	}

	// Track stats before processing for metrics
	beforeInserted := atomic.LoadInt64(&stats.Inserted)
	beforeDuplicated := atomic.LoadInt64(&stats.Duplicated)

	if err := s.processFeedItems(ctx, src, feedItems, existsMap, stats); err != nil {
		metrics.RecordFeedCrawlError(src.ID, "process_items_failed")
		return fmt.Errorf("process feed items: %w", err)
	}

	safeCtx := context.WithoutCancel(ctx)
	if err := s.SourceRepo.TouchCrawledAt(safeCtx, src.ID, time.Now()); err != nil {
		return fmt.Errorf("update source crawled timestamp: %w", err)
	}

	sourceDuration := time.Since(sourceStart)
	itemsFound := int64(len(feedItems))
	itemsInserted := atomic.LoadInt64(&stats.Inserted) - beforeInserted
	itemsDuplicated := atomic.LoadInt64(&stats.Duplicated) - beforeDuplicated

	// Record metrics for this source crawl
	metrics.RecordFeedCrawl(src.ID, sourceDuration, itemsFound, itemsInserted, itemsDuplicated)

	logger.Info("source crawl completed",
		slog.Int64("source_id", src.ID),
		slog.Int64("feed_items", itemsFound),
		slog.Int64("inserted", itemsInserted),
		slog.Int64("duplicated", itemsDuplicated),
		slog.Duration("duration", sourceDuration),
	)

	return nil
}

// processFeedItems processes all feed items from a source in parallel,
// summarizing and storing new articles while tracking statistics.
// Uses two-tier parallelism: configurable concurrent content fetches, 5 concurrent AI summarizations.
//
// Error Handling:
//   - Context cancellation (context.Canceled, context.DeadlineExceeded): Propagates immediately (aborts crawl)
//   - Database errors: Propagates (aborts crawl for this source)
//   - Summarization errors: Logged and counted in stats.SummarizeError, processing continues with other articles
func (s *Service) processFeedItems(
	ctx context.Context,
	src *entity.Source,
	feedItems []FeedItem,
	existsMap map[string]bool,
	stats *CrawlStats,
) error {
	contentSem := make(chan struct{}, s.contentConfig.Parallelism)
	summarySem := make(chan struct{}, summarizerParallelism)
	eg, egCtx := errgroup.WithContext(ctx)

	for _, feedItem := range feedItems {
		item := feedItem

		atomic.AddInt64(&stats.FeedItems, 1)

		// 既に存在するURLはスキップ
		if existsMap[item.URL] {
			atomic.AddInt64(&stats.Duplicated, 1)
			continue
		}

		eg.Go(func() error {
			// Step 1: Content enhancement (higher parallelism for I/O-bound)
			contentSem <- struct{}{}
			content := s.enhanceContent(egCtx, item)
			<-contentSem

			// Step 2: AI summarization (lower parallelism, rate-limited)
			summarySem <- struct{}{}
			defer func() { <-summarySem }()

			// Measure summarization duration
			summaryStart := time.Now()
			summary, err := s.Summarizer.Summarize(egCtx, content)
			summaryDuration := time.Since(summaryStart)

			if err != nil {
				// Context cancellation is critical - propagate immediately
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}

				// TODO(FEAT-CRAWL-RESILIENCE-001-Phase2): [DEFERRED 2025-12-15] Replace inline error handling with ResilientProcessor.
				// Rationale: Current inline error handling is sufficient for current scale.
				// See design: docs/designs/security-and-quality-improvements.md Section 3.2.5
				atomic.AddInt64(&stats.SummarizeError, 1)
				metrics.RecordArticleSummarized(false)
				metrics.RecordSummarizationDuration(summaryDuration)

				// Log warning and skip this article instead of stopping entire crawl
				logger := slog.Default()
				logger.Warn("summarization failed, skipping article",
					slog.Int64("source_id", src.ID),
					slog.String("url", item.URL),
					slog.String("title", item.Title),
					slog.Any("error", err))
				return nil // Continue processing other articles
			}

			// Record successful summarization
			metrics.RecordArticleSummarized(true)
			metrics.RecordSummarizationDuration(summaryDuration)

			art := &entity.Article{
				SourceID:    src.ID,
				Title:       item.Title,
				URL:         item.URL,
				Summary:     summary,
				PublishedAt: item.PublishedAt,
				CreatedAt:   time.Now(),
			}
			if err := s.ArticleRepo.Create(egCtx, art); err != nil {
				return fmt.Errorf("create article in repository: %w", err)
			}
			atomic.AddInt64(&stats.Inserted, 1)

			// Generate embedding asynchronously (non-blocking)
			// Note: EmbeddingHook spawns goroutine internally, no need for go func() here
			if s.EmbeddingHook != nil {
				s.EmbeddingHook.EmbedArticleAsync(egCtx, art)
			}

			// Notify about new article (non-blocking)
			// Note: NotifyService handles goroutines internally, no need for go func() here
			if err := s.NotifyService.NotifyNewArticle(context.Background(), art, src); err != nil {
				// NotifyNewArticle returns nil (fire-and-forget), but keeping error check for future
				slog.Warn("Failed to dispatch notification",
					slog.Int64("article_id", art.ID),
					slog.String("url", art.URL),
					slog.Any("error", err))
			}

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}

// enhanceContent enhances RSS content by fetching full article content if needed.
// This method implements the content enhancement logic:
//  1. Check if ContentFetcher is enabled (nil check)
//  2. Check if RSS content is sufficient (>= threshold)
//  3. Attempt to fetch full content from source URL
//  4. Use fetched content if longer than RSS content
//  5. Fall back to RSS content on any error
//
// The method NEVER returns an error - it always returns content (RSS or fetched).
// This ensures that content fetching failures do not break the crawl pipeline.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - item: Feed item containing URL and RSS content
//
// Returns:
//   - string: Enhanced content (either fetched or RSS fallback)
//
// Behavior:
//   - ContentFetcher == nil → return RSS content (feature disabled)
//   - RSS length >= threshold → return RSS content (skip fetch)
//   - RSS length < threshold → attempt fetch, fallback to RSS on error
//   - Fetched content shorter than RSS → return RSS content
//
// Example:
//
//	content := s.enhanceContent(ctx, feedItem)
//	// content is guaranteed to be non-error, either enhanced or RSS
func (s *Service) enhanceContent(ctx context.Context, item FeedItem) string {
	logger := slog.Default()

	// Check if content fetching is enabled
	if s.ContentFetcher == nil {
		// Feature disabled, use RSS content
		return item.Content
	}

	// Check RSS content length threshold
	rssLength := len(item.Content)
	if rssLength >= s.contentConfig.Threshold {
		// RSS content is sufficient, skip fetching
		logger.Debug("RSS content sufficient, skipping fetch",
			slog.String("url", item.URL),
			slog.Int("rss_length", rssLength),
			slog.Int("threshold", s.contentConfig.Threshold))
		metrics.RecordContentFetchSkipped()
		return item.Content
	}

	// RSS content is insufficient, fetch full article
	logger.Info("Fetching full article content",
		slog.String("url", item.URL),
		slog.Int("rss_length", rssLength))

	fetchStart := time.Now()
	fullContent, err := s.ContentFetcher.FetchContent(ctx, item.URL)
	fetchDuration := time.Since(fetchStart)

	if err != nil {
		// Content fetch failed, use RSS fallback
		logger.Warn("Content fetch failed, using RSS fallback",
			slog.String("url", item.URL),
			slog.Any("error", err),
			slog.Duration("fetch_duration", fetchDuration))
		metrics.RecordContentFetchFailed(fetchDuration)
		return item.Content
	}

	// Content fetch successful
	fetchedLength := len(fullContent)
	logger.Info("Content fetch successful",
		slog.String("url", item.URL),
		slog.Int("rss_length", rssLength),
		slog.Int("fetched_length", fetchedLength),
		slog.Duration("fetch_duration", fetchDuration))
	metrics.RecordContentFetchSuccess(fetchDuration, fetchedLength)

	// Use fetched content only if it's longer than RSS content
	// This prevents using truncated or poor-quality extracted content
	if fetchedLength > rssLength {
		return fullContent
	}

	// Fetched content is shorter than RSS, use RSS content
	logger.Debug("Fetched content shorter than RSS, using RSS",
		slog.String("url", item.URL),
		slog.Int("rss_length", rssLength),
		slog.Int("fetched_length", fetchedLength))
	return item.Content
}
