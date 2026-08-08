package fetch

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"catchup-feed/internal/domain/entity"
)

const (
	// DefaultNewsletterMaxArticles caps how many linked articles one
	// newsletter issue may expand into (env NEWSLETTER_MAX_ARTICLES).
	// Links are taken in document order — link-roundup newsletters put the
	// headline stories first, so the head of the list is the right
	// heuristic slice. Right-sized: 10 articles/issue × 每週 1 issue per
	// source keeps the summarizer quota impact comparable to a normal rss
	// source.
	DefaultNewsletterMaxArticles = 10

	// newsletterMinInlineLinks is the threshold below which the RSS
	// description is considered link-poor (some newsletters ship a teaser
	// instead of the issue body) and the issue page itself is fetched as
	// HTML instead. Cooperpress issues carry dozens of links, so <3 means
	// "the description is not the issue".
	newsletterMinInlineLinks = 3
)

// HTMLFetcher is the optional capability of a ContentFetcher to return raw
// page HTML (no readability extraction — readability flattens the page to
// text and destroys the <a href> structure the link extractor needs).
// Implemented by fetcher.ReadabilityFetcher; the newsletter path discovers
// it by type assertion (same pattern as ProviderSummarizer), so wiring
// stays unchanged and the fallback silently disables when unavailable.
// Implementations MUST apply the same SSRF protections as FetchContent
// (validateURL + per-hop redirect validation).
type HTMLFetcher interface {
	FetchHTML(ctx context.Context, url string) (string, error)
}

// processNewsletterItems handles new items of kind='newsletter' sources.
// One feed item is one *issue* of a link-roundup newsletter (Golang Weekly
// 等): instead of storing the issue page as a single article (旧挙動 —
// kind='rss' では号ページの生 HTML が 1 記事になり要約入力は先頭 10KB の断片
// だった), each issue is expanded into the articles it links to, and each
// linked article goes through the existing fetch → summarize pipeline.
//
// Error handling follows the rss path's spirit but is deliberately softer
// (縮退許容): a single link failing to fetch / summarize / insert skips
// that link only; a whole-issue failure skips that issue only; the crawl
// (other sources included) is aborted ONLY when ctx itself is dead —
// judged by ctx.Err() directly, same as processFeedItems (B-1).
func (s *Service) processNewsletterItems(
	ctx context.Context,
	src *entity.Source,
	feedItems []FeedItem,
	existsMap map[string]bool,
	stats *CrawlStats,
) error {
	logger := slog.Default()

	// Without a ContentFetcher there is nothing to expand links with: skip
	// the whole source (no issue markers), so issues stay unknown and are
	// caught up when content fetching is enabled again.
	if s.ContentFetcher == nil {
		logger.Warn("content fetching disabled, newsletter source skipped",
			slog.Int64("source_id", src.ID),
			slog.String("feed_url", src.FeedURL))
		return nil
	}

	maxArticles := s.NewsletterMaxArticles
	if maxArticles <= 0 {
		maxArticles = DefaultNewsletterMaxArticles
	}

	for _, item := range feedItems {
		atomic.AddInt64(&stats.FeedItems, 1)

		// 号 URL での重複判定: 一度処理した号は号マーカー(下)が
		// ExistsByURLBatch に載るため、次サイクル以降ここで落ちる。
		if existsMap[item.URL] {
			atomic.AddInt64(&stats.Duplicated, 1)
			continue
		}
		if item.URL == "" {
			// 号 URL が無いと重複判定も号マーカーも成立しない(毎サイクル
			// 再展開されてしまう)ので、リンク展開はせずスキップする。
			logger.Warn("newsletter item without URL, skipping",
				slog.Int64("source_id", src.ID),
				slog.String("title", item.Title))
			continue
		}

		if err := s.processNewsletterIssue(ctx, src, item, maxArticles, stats); err != nil {
			return err
		}
	}
	return nil
}

// processNewsletterIssue expands one new issue: extract links, filter and
// cap them, run each through fetch → summarize → insert, then record the
// issue itself as a content-less marker row for next-cycle dedupe.
//
// The issue marker is written only when no attempted link failed OR at
// least one article was persisted. A cycle in which every attempted link
// failed (typically: all summarize providers down, §8) writes no marker,
// so the whole issue is retried next hour — already-persisted links are
// dedupe-skipped then. Partial failures alongside successes are accepted
// as lost (likely per-link permanent problems; retrying the issue forever
// would be worse — 縮退許容).
func (s *Service) processNewsletterIssue(
	ctx context.Context,
	src *entity.Source,
	item FeedItem,
	maxArticles int,
	stats *CrawlStats,
) error {
	logger := slog.Default()

	links := extractNewsletterLinks(item.Content, item.URL, src.FeedURL)

	// Fallback: the RSS description carried (almost) no links — some
	// newsletters ship a teaser instead of the issue body. Fetch the issue
	// page itself as raw HTML (readability would destroy the anchors).
	// SSRF 防御は FetchContent と同一(HTMLFetcher の契約)。
	if len(links) < newsletterMinInlineLinks {
		if hf, ok := s.ContentFetcher.(HTMLFetcher); ok {
			pageHTML, err := hf.FetchHTML(ctx, item.URL)
			if err != nil {
				if ctx.Err() != nil {
					return err
				}
				logger.Warn("newsletter issue page fetch failed, using inline links only",
					slog.Int64("source_id", src.ID),
					slog.String("issue_url", item.URL),
					slog.Any("error", err))
			} else if fetched := extractNewsletterLinks(pageHTML, item.URL, src.FeedURL); len(fetched) > len(links) {
				links = fetched
			}
		}
	}

	if len(links) > maxArticles {
		// Silent truncation 禁止: 何件落としたかを必ず残す。
		logger.Info("newsletter issue links truncated to cap",
			slog.Int64("source_id", src.ID),
			slog.String("issue_url", item.URL),
			slog.Int("links_found", len(links)),
			slog.Int("cap", maxArticles),
			slog.Int("dropped", len(links)-maxArticles))
		links = links[:maxArticles]
	}

	// DB 既存チェック(他ソースの記事と衝突し得る)。キーは正規化済み URL —
	// INSERT する文字列と同一(extractNewsletterLinks が保証)。
	urls := make([]string, 0, len(links))
	for _, ln := range links {
		urls = append(urls, ln.URL)
	}
	linkExists, err := s.ArticleRepo.ExistsByURLBatch(ctx, urls)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		// 号マーカー無しでスキップ → 次サイクルで号ごと再試行。
		logger.Warn("newsletter link batch check failed, issue deferred to next cycle",
			slog.Int64("source_id", src.ID),
			slog.String("issue_url", item.URL),
			slog.Any("error", err))
		return nil
	}

	// Sequential on purpose (裁量): at most maxArticles links per issue and
	// the summarizer chain is the rate-limited resource; the rss path's
	// two-tier parallelism would buy little here (右サイズ).
	var attempted, persisted, failed int
	for _, ln := range links {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if linkExists[ln.URL] {
			atomic.AddInt64(&stats.Duplicated, 1)
			continue
		}
		attempted++
		inserted, err := s.processNewsletterLink(ctx, src, item, ln, stats)
		if err != nil {
			return err // ctx-dead only (contract of processNewsletterLink)
		}
		if inserted {
			persisted++
		} else {
			failed++
		}
	}

	if failed > 0 && persisted == 0 {
		logger.Warn("all attempted newsletter links failed, issue left for next cycle",
			slog.Int64("source_id", src.ID),
			slog.String("issue_url", item.URL),
			slog.Int("attempted", attempted),
			slog.Int("failed", failed))
		return nil
	}

	// 号マーカー: content 空(DB では NULL)・要約なしの記事行。次サイクルの
	// ExistsByURLBatch(号 URL)に載せるためだけの行で、
	//   - SweepUnsummarized は content IS NOT NULL AND content <> '' を
	//     要求するので拾わない(ListUnsummarized の WHERE、実テストで担保)
	//   - radio 選定は summaries INNER JOIN なので乗らない
	// stats.Inserted には数えない(記事ではないため)。
	marker := &entity.Article{
		SourceID:    src.ID,
		Title:       item.Title,
		URL:         item.URL,
		Content:     "", // stored as NULL (nullString)
		PublishedAt: item.PublishedAt,
		CrawledAt:   time.Now(),
	}
	if err := s.ArticleRepo.Create(ctx, marker); err != nil {
		if ctx.Err() != nil {
			return err
		}
		// マーカーが無いだけなら次サイクルで号を再処理する(展開済み
		// リンクは dedupe で落ちる)ので、警告して続行。
		logger.Warn("newsletter issue marker insert failed, issue will be re-scanned next cycle",
			slog.Int64("source_id", src.ID),
			slog.String("issue_url", item.URL),
			slog.Any("error", err))
		return nil
	}

	logger.Info("newsletter issue expanded",
		slog.Int64("source_id", src.ID),
		slog.String("issue_url", item.URL),
		slog.Int("links", len(links)),
		slog.Int("persisted", persisted),
		slog.Int("failed", failed))
	return nil
}

// processNewsletterLink runs one linked article through the existing
// pipeline: readability fetch → summarize fallback chain (日本語要約 = 翻訳
// を兼ねる) → atomic insert with ON CONFLICT DO NOTHING semantics.
//
// Contract: a non-nil error is returned ONLY when ctx is dead (abort the
// crawl); every per-link failure logs, counts, and returns (false, nil) so
// the caller continues with the next link. published_at inherits the
// issue's published date (D-15b カットオフ・radio 選定順との整合).
func (s *Service) processNewsletterLink(
	ctx context.Context,
	src *entity.Source,
	issue FeedItem,
	ln newsletterLink,
	stats *CrawlStats,
) (bool, error) {
	logger := slog.Default()

	content, err := s.ContentFetcher.FetchContent(ctx, ln.URL)
	if err != nil {
		if ctx.Err() != nil {
			return false, err
		}
		logger.Warn("newsletter link content fetch failed, skipping link",
			slog.Int64("source_id", src.ID),
			slog.String("url", ln.URL),
			slog.String("title", ln.Title),
			slog.Any("error", err))
		return false, nil
	}

	summary, provider, err := s.summarize(ctx, content)
	if err != nil {
		// Judge criticality by ctx directly, NOT errors.Is on err: provider
		// per-request timeouts can wrap context.DeadlineExceeded while the
		// crawl itself is fine (same reasoning as processFeedItems).
		if ctx.Err() != nil {
			return false, err
		}
		atomic.AddInt64(&stats.SummarizeError, 1)
		logger.Warn("newsletter link summarization failed, skipping link",
			slog.Int64("source_id", src.ID),
			slog.String("url", ln.URL),
			slog.String("title", ln.Title),
			slog.Any("error", err))
		return false, nil
	}
	if provider == "" {
		provider = entity.SummaryProviderUnknown
	}

	art := &entity.Article{
		SourceID:    src.ID,
		Title:       ln.Title,
		URL:         ln.URL, // 正規化済み — ExistsByURLBatch のキーと同一文字列
		Content:     content,
		Summary:     summary, // read-only join field; persisted via summaries row
		PublishedAt: issue.PublishedAt,
		CrawledAt:   time.Now(),
	}
	sum := &entity.Summary{Body: summary, Provider: provider}
	inserted, err := s.ArticleRepo.CreateWithSummaryIfNew(ctx, art, sum)
	if err != nil {
		if ctx.Err() != nil {
			return false, err
		}
		logger.Warn("newsletter link insert failed, skipping link",
			slog.Int64("source_id", src.ID),
			slog.String("url", ln.URL),
			slog.Any("error", err))
		return false, nil
	}
	if !inserted {
		// バッチ既存チェック後に他ソースが同 URL を入れたレース。重複扱い。
		atomic.AddInt64(&stats.Duplicated, 1)
		logger.Info("newsletter link already stored by another source, skipped",
			slog.Int64("source_id", src.ID),
			slog.String("url", ln.URL))
		return true, nil
	}

	atomic.AddInt64(&stats.Inserted, 1)
	logger.Info("newsletter link article summarized",
		slog.Int64("article_id", art.ID),
		slog.String("url", art.URL),
		slog.String("summary_provider", provider))
	return true, nil
}
