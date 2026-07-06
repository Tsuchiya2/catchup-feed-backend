package fetch

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync/atomic"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"

	"golang.org/x/sync/errgroup"
)

const (
	summarizerParallelism = 5 // AI summarization parallelism (rate-limited)

	// YouTubeDirectMaxPerCycle caps §5.1 stage-1 (Gemini URL 直接入力)
	// attempts per crawl cycle. One video burns free-tier tokens on the
	// order of a hundred text summaries, so the cap bounds a cycle's worst
	// case (e.g. a freshly registered channel whose RSS is entirely new);
	// the overflow takes the transcribe queue exactly like a stage-1
	// failure. Right-sized against U-20 (2-3 channels total, typically ≤1
	// new video/day each) with the hourly cron giving 24 cycles/day of
	// headroom, and against the crawl budget (attempts are sequential,
	// each bounded by the describer's own request timeout).
	YouTubeDirectMaxPerCycle = 3

	// TranscribeBackfillCutoff bounds how far back the transcribe path
	// (youtube/podcast) ingests feed items (D-15, Phase 2 §5.2): items whose
	// published_at is older than this are skipped entirely — no articles
	// INSERT, no transcribe job, no §5.1 stage-1 attempt. Podcast feeds
	// carry their full history (e.g. Latent Space, 212 items), and ingesting
	// it would fill every morning's radio with months of back-catalog.
	// Items with an unknown published_at (zero value) are treated as new —
	// D-15: 判定不能を落とさない. The rss path is NOT affected.
	TranscribeBackfillCutoff = 14 * 24 * time.Hour
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
// EnclosureURL carries the first media enclosure URL when the feed
// provides one (podcast episodes, Phase 2 §5.2); empty otherwise.
type FeedItem struct {
	Title        string
	URL          string
	Content      string
	PublishedAt  time.Time
	EnclosureURL string
}

// Service provides feed crawling and article fetching use cases.
// It orchestrates the process of fetching feeds, summarizing content, and storing articles.
//
// Note: the old per-article notification hook is gone by design. pulse
// notifies per *episode* via the jobs queue (§3.3 / §7); per-article pings
// were the old system's failure mode (最適化目標の転換, design doc §1).
type Service struct {
	SourceRepo     repository.SourceRepository
	ArticleRepo    repository.ArticleRepository
	Summarizer     Summarizer
	FeedFetcher    FeedFetcher
	ContentFetcher ContentFetcher     // Content enhancement for B-rated feeds
	contentConfig  ContentFetchConfig // Configuration for content fetching behavior

	// SummaryRepo is required only by SweepUnsummarized (Phase 2 §5.2b):
	// the sweep upserts summaries for articles whose content arrived
	// after insert (transcripts). Not part of NewService because the
	// crawl path persists summaries atomically via CreateWithSummary.
	SummaryRepo repository.SummaryRepository

	// VideoDescriber, when non-nil, enables the §5.1 stage-1 attempt for
	// kind='youtube' items before they fall back to the transcribe queue.
	// nil (e.g. GEMINI_API_KEY unset) skips stage 1 entirely and every new
	// video is enqueued for the Mac worker. Optional like SummaryRepo:
	// not part of NewService.
	VideoDescriber VideoDescriber
}

// VideoDescriber is the §5.1 stage-1 backend (Gemini に動画 URL を直接入力):
// a single attempt to turn one public YouTube video URL into a detailed
// transcript plus a Japanese summary in one request. Implemented by
// summarizer.Gemini; Name() is persisted to summaries.provider. Only public
// video URLs may be passed (C-12). Errors are never retried by the caller —
// the transcribe queue (stage 2/3) is the recovery path (C-14).
type VideoDescriber interface {
	Name() string
	DescribeVideo(ctx context.Context, videoURL string) (transcript, summary string, err error)
}

// Summarizer is an interface for AI-powered text summarization.
type Summarizer interface {
	Summarize(ctx context.Context, text string) (string, error)
}

// ProviderSummarizer is optionally implemented by summarizers that can report
// which backend produced the summary (e.g. the Gemini -> Groq -> Ollama
// fallback chain). The provider name is persisted to summaries.provider
// (§4, §8: fallback occurrences must be observable after the fact).
type ProviderSummarizer interface {
	SummarizeWithProvider(ctx context.Context, text string) (summary string, provider string, err error)
}

// NewService creates a new fetch Service with the provided dependencies.
// This constructor ensures proper initialization of the Service with all required components.
//
// Parameters:
//   - sourceRepo: Repository for managing feed sources
//   - articleRepo: Repository for managing articles (articles + summaries
//     are persisted atomically via CreateWithSummary)
//   - summarizer: AI service for text summarization
//   - feedFetcher: Service for fetching RSS/Atom feeds
//   - contentFetcher: Service for fetching full article content (can be nil to disable)
//   - contentConfig: Configuration for content fetching behavior (parallelism, threshold)
//
// Returns:
//   - Service: Configured fetch service ready to use
//
// Example:
//
//	config := ContentFetchConfig{Parallelism: 10, Threshold: 1500}
//	service := NewService(sourceRepo, articleRepo, summarizer, feedFetcher, contentFetcher, config)
func NewService(
	sourceRepo repository.SourceRepository,
	articleRepo repository.ArticleRepository,
	summarizer Summarizer,
	feedFetcher FeedFetcher,
	contentFetcher ContentFetcher,
	contentConfig ContentFetchConfig,
) Service {
	return Service{
		SourceRepo:     sourceRepo,
		ArticleRepo:    articleRepo,
		Summarizer:     summarizer,
		FeedFetcher:    feedFetcher,
		ContentFetcher: contentFetcher,
		contentConfig:  contentConfig,
	}
}

// CrawlStats contains statistics about a crawl operation.
// TranscribeEnqueued counts articles inserted content-less with a pending
// transcribe job (youtube/podcast sources, Phase 2 §5); those articles are
// also counted in Inserted. SkippedNoMedia counts feed items dropped
// because no media URL could be determined. SkippedBackfill counts
// transcribe-path items dropped by the D-15 backlog cutoff (published_at
// older than TranscribeBackfillCutoff). YouTubeDirectAttempts counts
// §5.1 stage-1 tries this cycle (capped at YouTubeDirectMaxPerCycle) and
// YouTubeDirectSucceeded the ones persisted with a summary and no
// transcribe job (also counted in Inserted, not in TranscribeEnqueued).
type CrawlStats struct {
	Sources                int
	FeedItems              int64
	Inserted               int64
	Duplicated             int64
	SummarizeError         int64
	TranscribeEnqueued     int64
	SkippedNoMedia         int64
	SkippedBackfill        int64
	YouTubeDirectAttempts  int64
	YouTubeDirectSucceeded int64
	Duration               time.Duration
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

	// transcribe kind (youtube/podcast) を rss より先に処理する(安定ソート:
	// 同 kind 内は ListActive の返す id 順を維持)。transcribe 経路は
	// go-readability も要約フォールバック連鎖も通らず(検知 + INSERT +
	// transcribe ジョブ enqueue、§5.1 第1段は失敗しても transcribe へ落ちる
	// だけ)数秒で完了する。一方 rss 経路は要約全プロバイダ全滅でクロール
	// 全体を中断し得る(§8)し、CrawlTimeout も rss 処理中に尽きやすい。
	// id 順のままだと末尾の youtube/podcast ソースが要約詰まりの人質になって
	// 毎サイクル未到達になる(本番障害: id 305〜309 に一度も到達せず)。
	// 先行させれば、クオータ枯渇日でも新着検知と enqueue は必ず成立する。
	sort.SliceStable(srcs, func(i, j int) bool {
		return isTranscribeKind(srcs[i]) && !isTranscribeKind(srcs[j])
	})

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
		slog.Int64("transcribe_enqueued", stats.TranscribeEnqueued),
		slog.Int64("skipped_no_media", stats.SkippedNoMedia),
		slog.Int64("skipped_backfill", stats.SkippedBackfill),
		slog.Int64("youtube_direct_attempts", stats.YouTubeDirectAttempts),
		slog.Int64("youtube_direct_succeeded", stats.YouTubeDirectSucceeded),
		slog.Duration("duration", stats.Duration),
	)

	return stats, nil
}

// isTranscribeKind reports whether the source is handled by the transcribe
// path (enqueueTranscribeItems: youtube/podcast, Phase 2 §5) as opposed to
// the rss summarize path. Used by CrawlAllSources to order transcribe
// sources ahead of rss sources.
func isTranscribeKind(src *entity.Source) bool {
	return src.Kind == entity.SourceKindYouTube || src.Kind == entity.SourceKindPodcast
}

// processSingleSource processes a single feed source by fetching, deduplicating,
// summarizing, and storing articles. It updates the provided stats atomically.
// Returns error only for critical failures (database errors).
// Logs and continues for recoverable failures (fetch errors, batch check errors).
func (s *Service) processSingleSource(ctx context.Context, src *entity.Source, stats *CrawlStats) error {
	logger := slog.Default()
	sourceStart := time.Now()

	feedItems, err := s.FeedFetcher.Fetch(ctx, src.FeedURL)
	if err != nil {
		logger.Warn("failed to fetch feed",
			slog.Int64("source_id", src.ID),
			slog.String("feed_url", src.FeedURL),
			slog.Any("error", err))
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
	// (キーは articleURLForItem — articles.url に入る値と同じでないと
	// dedupe が効かず、次サイクルで UNIQUE 制約に衝突する)
	urls := make([]string, 0, len(feedItems))
	for _, item := range feedItems {
		urls = append(urls, articleURLForItem(src, item))
	}
	existsMap, err := s.ArticleRepo.ExistsByURLBatch(ctx, urls)
	if err != nil {
		logger.Warn("failed to batch check URLs",
			slog.Int64("source_id", src.ID),
			slog.Any("error", err))
		// Continue with other sources even if batch check fails
		return nil
	}

	// Track stats before processing for logging
	beforeInserted := atomic.LoadInt64(&stats.Inserted)
	beforeDuplicated := atomic.LoadInt64(&stats.Duplicated)

	// kind 分岐 (Phase 2 §5): youtube/podcast share the gofeed new-item
	// detection above but never touch go-readability or the summarizer —
	// the article is stored content-less and a transcribe job carries the
	// media URL to the Mac worker. Summarization happens only after the
	// transcript fills content (§4: content が NULL のうちは要約対象外).
	switch src.Kind {
	case entity.SourceKindYouTube, entity.SourceKindPodcast:
		if err := s.enqueueTranscribeItems(ctx, src, feedItems, existsMap, stats); err != nil {
			return fmt.Errorf("enqueue transcribe items: %w", err)
		}
	default: // '' / 'rss': 既存挙動そのまま
		if err := s.processFeedItems(ctx, src, feedItems, existsMap, stats); err != nil {
			return fmt.Errorf("process feed items: %w", err)
		}
	}

	sourceDuration := time.Since(sourceStart)
	itemsFound := int64(len(feedItems))
	itemsInserted := atomic.LoadInt64(&stats.Inserted) - beforeInserted
	itemsDuplicated := atomic.LoadInt64(&stats.Duplicated) - beforeDuplicated

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

			summary, provider, err := s.summarize(egCtx, content)
			if err != nil {
				// Only a dead group context (shutdown or crawl deadline) is
				// critical. Judge by egCtx directly, NOT errors.Is on the
				// returned error: providers apply their own per-request
				// timeouts, so an all-providers-failed error can wrap
				// context.DeadlineExceeded while the crawl itself is fine.
				// That case must skip one article, not abort the crawl (§8).
				if egCtx.Err() != nil {
					return err
				}

				atomic.AddInt64(&stats.SummarizeError, 1)

				// Log warning and skip this article instead of stopping entire crawl
				logger := slog.Default()
				logger.Warn("summarization failed, skipping article",
					slog.Int64("source_id", src.ID),
					slog.String("url", item.URL),
					slog.String("title", item.Title),
					slog.Any("error", err))
				return nil // Continue processing other articles
			}

			// Persist article + summary atomically: a summary failure rolls
			// the article back, so no article can end up permanently
			// unsummarized — the URL stays unknown and the next hourly
			// crawl retries it (§8). summaries.provider records which
			// chain leg produced the summary (§4 fallback observability).
			if provider == "" {
				provider = entity.SummaryProviderUnknown
			}
			art := &entity.Article{
				SourceID:    src.ID,
				Title:       item.Title,
				URL:         item.URL,
				Content:     content,
				Summary:     summary, // read-only join field; persisted via summaries row below
				PublishedAt: item.PublishedAt,
				CrawledAt:   time.Now(),
			}
			sum := &entity.Summary{Body: summary, Provider: provider}
			if err := s.ArticleRepo.CreateWithSummary(egCtx, art, sum); err != nil {
				return fmt.Errorf("create article with summary in repository: %w", err)
			}
			atomic.AddInt64(&stats.Inserted, 1)

			slog.Info("article summarized",
				slog.Int64("article_id", art.ID),
				slog.String("url", art.URL),
				slog.String("summary_provider", provider))

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}

// articleURLForItem resolves the value stored in articles.url for a feed
// item. Podcast episodes may lack a <link> while still carrying a valid
// enclosure (§5.2); falling back to the enclosure URL keeps the row's
// unique key non-empty and stable, so dedupe works across crawl cycles.
// Every dedupe lookup (ExistsByURLBatch keys, existsMap checks) MUST use
// this same function — otherwise the next crawl re-inserts the episode and
// hits the articles.url UNIQUE constraint.
// For rss/youtube items this is the entry link unchanged (a youtube item
// without a link cannot be identified and is skipped as SkippedNoMedia).
func articleURLForItem(src *entity.Source, item FeedItem) string {
	if item.URL == "" && src.Kind == entity.SourceKindPodcast {
		return item.EnclosureURL
	}
	return item.URL
}

// enqueueTranscribeItems handles new items of youtube/podcast sources
// (Phase 2 §5.1 / §5.2, Pi 側): items older than TranscribeBackfillCutoff
// are dropped up front (D-15); each remaining new item becomes an articles row with
// content NULL plus a kind='transcribe' job, inserted atomically by the
// repository — except youtube items that the §5.1 stage-1 describer handles
// first (see describeVideoDirect). No parallelism: the DB writes are local
// and the stage-1 API calls are the rate-limited resource (right-sizing;
// attempts are capped per cycle anyway).
//
// Media URL resolution:
//   - youtube: the entry link (動画 URL — YouTube channel RSS entries link
//     to the watch page)
//   - podcast: the feed enclosure audio URL; items without an enclosure
//     are skipped with a log (§5.2: 無い項目はスキップしてログ)
func (s *Service) enqueueTranscribeItems(
	ctx context.Context,
	src *entity.Source,
	feedItems []FeedItem,
	existsMap map[string]bool,
	stats *CrawlStats,
) error {
	logger := slog.Default()

	// D-15 バックログカットオフ: published_at がこの時刻より古い item は
	// 取り込まない(§5.2)。published_at 不明(zero value)は新着扱い。
	// ソース単位のサマリ1行に集計するため件数はローカルに数える(初回
	// クロールのポッドキャストは数百件スキップし得るので item ごとの
	// ログは出さない)。
	backfillCutoff := time.Now().Add(-TranscribeBackfillCutoff)
	var skippedBackfill int64

	for _, item := range feedItems {
		atomic.AddInt64(&stats.FeedItems, 1)

		// カットオフ判定は dedupe より前・§5.1 第1段の cap より前に置く:
		// 古い item に Gemini 呼び出し枠(YouTubeDirectMaxPerCycle)を
		// 消費させないのが D-15 の要件。dedupe との順序はどちらでも
		// 正しさは変わらないが、ループ先頭に置くのが最も単純で、既存の
		// existsMap 構築(processSingleSource、kind 非依存)を触らずに
		// 済む — ExistsByURLBatch のキー数削減は初回クロール1回きりの
		// 話なので、そのために dedupe キー収集へ kind 分岐を持ち込む
		// 価値はない。
		if !item.PublishedAt.IsZero() && item.PublishedAt.Before(backfillCutoff) {
			skippedBackfill++
			continue
		}

		// articles.url に入る値(link 無し podcast は enclosure URL に
		// フォールバック)。dedupe キーと INSERT 値を必ず一致させる:
		// existsMap(ExistsByURLBatch)も同じ関数でキーを作っているので、
		// 一度 INSERT したエピソードは次サイクルで Duplicated になる。
		artURL := articleURLForItem(src, item)
		if existsMap[artURL] {
			atomic.AddInt64(&stats.Duplicated, 1)
			continue
		}

		// §5.1 第1段: youtube のみ、Gemini に公開動画の URL を直接入力して
		// 1回だけ試す。成功なら記事+要約が原子的に入り transcribe ジョブは
		// 積まない。失敗はここで再試行せず既存の transcribe 経路へ落とす
		// (C-14: 次段が回収する。dedupe により同一動画が翌サイクルで再び
		// 第1段に来ることもない)。podcast は第1段を通らない(§5.2)。
		// URL が空の item は動画を特定できないので第1段(Gemini 呼び出し
		// +cap 1枠)を消費させず、下の SkippedNoMedia 経路へ直行させる。
		if src.Kind == entity.SourceKindYouTube && s.VideoDescriber != nil && item.URL != "" {
			if atomic.LoadInt64(&stats.YouTubeDirectAttempts) >= YouTubeDirectMaxPerCycle {
				logger.Info("youtube direct cap reached for this cycle, deferring to transcribe queue",
					slog.Int64("source_id", src.ID),
					slog.String("url", item.URL),
					slog.Int("cap", YouTubeDirectMaxPerCycle))
			} else {
				atomic.AddInt64(&stats.YouTubeDirectAttempts, 1)
				handled, err := s.describeVideoDirect(ctx, src, item, stats)
				if err != nil {
					return err
				}
				if handled {
					continue
				}
			}
		}

		mediaURL := item.URL
		if src.Kind == entity.SourceKindPodcast {
			mediaURL = item.EnclosureURL
		}
		if mediaURL == "" {
			atomic.AddInt64(&stats.SkippedNoMedia, 1)
			logger.Warn("no media URL for feed item, skipping",
				slog.Int64("source_id", src.ID),
				slog.String("source_kind", src.Kind),
				slog.String("url", item.URL),
				slog.String("title", item.Title))
			continue
		}

		art := &entity.Article{
			SourceID:    src.ID,
			Title:       item.Title,
			URL:         artURL,
			Content:     "", // stored as NULL; the Mac transcribe worker fills it (§5)
			PublishedAt: item.PublishedAt,
			CrawledAt:   time.Now(),
		}
		if err := s.ArticleRepo.CreateWithTranscribeJob(ctx, art, mediaURL, src.Kind); err != nil {
			return fmt.Errorf("create article with transcribe job in repository: %w", err)
		}
		atomic.AddInt64(&stats.Inserted, 1)
		atomic.AddInt64(&stats.TranscribeEnqueued, 1)

		logger.Info("article enqueued for transcription",
			slog.Int64("article_id", art.ID),
			slog.String("url", art.URL),
			slog.String("source_kind", src.Kind),
			slog.String("media_url", mediaURL))
	}

	if skippedBackfill > 0 {
		atomic.AddInt64(&stats.SkippedBackfill, skippedBackfill)
		logger.Info("skipped backlog items older than cutoff (D-15)",
			slog.Int64("source_id", src.ID),
			slog.String("source_kind", src.Kind),
			slog.Int64("skipped_backfill", skippedBackfill),
			slog.Duration("cutoff", TranscribeBackfillCutoff))
	}

	return nil
}

// describeVideoDirect runs one §5.1 stage-1 attempt for a new youtube item.
// Returns handled=true when the transcript + summary were persisted (no
// transcribe job needed). A describer failure of any kind is not an error:
// it logs a warning and returns handled=false so the caller falls back to
// the transcribe queue — unless ctx itself is dead (shutdown / crawl
// deadline), which aborts the crawl. Criticality is judged by ctx.Err()
// directly, NOT errors.Is on the returned error: the describer applies its
// own per-request timeout, so a quota/timeout failure can wrap
// context.DeadlineExceeded while the crawl itself is fine (same reasoning
// as processFeedItems). Database errors abort the crawl as everywhere else.
func (s *Service) describeVideoDirect(ctx context.Context, src *entity.Source, item FeedItem, stats *CrawlStats) (bool, error) {
	logger := slog.Default()

	transcript, summary, err := s.VideoDescriber.DescribeVideo(ctx, item.URL)
	if err != nil {
		if ctx.Err() != nil {
			return false, err
		}
		logger.Warn("youtube direct description failed, falling back to transcribe queue",
			slog.Int64("source_id", src.ID),
			slog.String("url", item.URL),
			slog.String("title", item.Title),
			slog.Any("error", err))
		return false, nil
	}

	provider := s.VideoDescriber.Name()
	if provider == "" {
		provider = entity.SummaryProviderUnknown
	}
	// Persist transcript + summary atomically on the existing crawl path
	// (CreateWithSummary): the transcript goes to articles.content — same
	// slot the Mac transcribe worker would fill — so the rest of the
	// pipeline (選定 → 台本 → 放送) is untouched, and Phase 3 keeps the
	// source text (§5.1: 要約のみのショートカットは作らない).
	art := &entity.Article{
		SourceID:    src.ID,
		Title:       item.Title,
		URL:         item.URL,
		Content:     transcript,
		Summary:     summary, // read-only join field; persisted via summaries row below
		PublishedAt: item.PublishedAt,
		CrawledAt:   time.Now(),
	}
	sum := &entity.Summary{Body: summary, Provider: provider}
	if err := s.ArticleRepo.CreateWithSummary(ctx, art, sum); err != nil {
		return false, fmt.Errorf("create article with summary in repository: %w", err)
	}
	atomic.AddInt64(&stats.Inserted, 1)
	atomic.AddInt64(&stats.YouTubeDirectSucceeded, 1)

	logger.Info("youtube video described directly",
		slog.Int64("article_id", art.ID),
		slog.String("url", art.URL),
		slog.String("summary_provider", provider),
		slog.Int("transcript_length", len(transcript)))
	return true, nil
}

// summarize runs the configured summarizer, additionally reporting the
// provider name when the summarizer supports it (fallback chain).
// Returns an empty provider for plain Summarizer implementations.
func (s *Service) summarize(ctx context.Context, content string) (summary string, provider string, err error) {
	if ps, ok := s.Summarizer.(ProviderSummarizer); ok {
		return ps.SummarizeWithProvider(ctx, content)
	}
	summary, err = s.Summarizer.Summarize(ctx, content)
	return summary, "", err
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
		return item.Content
	}

	// Content fetch successful
	fetchedLength := len(fullContent)
	logger.Info("Content fetch successful",
		slog.String("url", item.URL),
		slog.Int("rss_length", rssLength),
		slog.Int("fetched_length", fetchedLength),
		slog.Duration("fetch_duration", fetchDuration))

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
