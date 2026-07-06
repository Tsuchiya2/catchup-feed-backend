package fetch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"catchup-feed/internal/domain/entity"
)

// DefaultSweepLimit bounds one sweep cycle (§5.2b: 1サイクルの処理上限).
// Right-sized for a single-user system: a night of transcriptions is a
// handful of items, so 50 is only ever reached after a long outage — and
// then the hourly cron drains the backlog 50 at a time.
const DefaultSweepLimit = 50

// SweepStats reports one SweepUnsummarized run.
type SweepStats struct {
	Candidates int   // articles picked up (content present, no summary)
	Summarized int64 // summaries upserted
	Failed     int64 // summarization failures, left for the next cycle
	LimitHit   bool  // candidate query returned exactly the limit
	Duration   time.Duration
}

// SweepUnsummarized summarizes articles whose content was filled in after
// insert — the transcribe path of Phase 2 §5.2b. RSS articles never
// qualify: CreateWithSummary persists article + summary atomically, so
// "content present, summary missing" is by itself the correct target
// definition and no kind filter is needed.
//
// Failures are deliberately soft (§8 縮退許容): a summarization error
// (e.g. all free-tier providers down) leaves the article untouched, and
// the next hourly cron retries it. Only database errors and a dead ctx
// abort the sweep. Requires SummaryRepo to be set.
func (s *Service) SweepUnsummarized(ctx context.Context) (*SweepStats, error) {
	if s.SummaryRepo == nil {
		return nil, errors.New("sweep: SummaryRepo is not configured")
	}
	logger := slog.Default()
	start := time.Now()
	stats := &SweepStats{}

	articles, err := s.ArticleRepo.ListUnsummarized(ctx, DefaultSweepLimit)
	if err != nil {
		return nil, fmt.Errorf("list unsummarized articles: %w", err)
	}
	stats.Candidates = len(articles)
	stats.LimitHit = len(articles) == DefaultSweepLimit
	if stats.LimitHit {
		logger.Warn("summary sweep hit the per-cycle limit, remainder deferred to next cycle",
			slog.Int("limit", DefaultSweepLimit))
	}

	// Sequential on purpose: sweep volume is a few transcripts per night,
	// and the summarizer chain is the rate-limited resource the hourly
	// crawl already shares (right-sizing over throughput).
	for _, art := range articles {
		if ctx.Err() != nil {
			stats.Duration = time.Since(start)
			return stats, ctx.Err()
		}

		summary, provider, err := s.summarize(ctx, art.Content)
		if err != nil {
			// Judge criticality by ctx, not errors.Is: provider timeouts
			// wrap context.DeadlineExceeded while the sweep itself is
			// fine (same reasoning as processFeedItems).
			if ctx.Err() != nil {
				stats.Duration = time.Since(start)
				return stats, err
			}
			stats.Failed++
			logger.Warn("sweep summarization failed, article left for next cycle",
				slog.Int64("article_id", art.ID),
				slog.String("url", art.URL),
				slog.Any("error", err))
			continue
		}

		if provider == "" {
			provider = entity.SummaryProviderUnknown
		}
		sum := &entity.Summary{ArticleID: art.ID, Body: summary, Provider: provider}
		if err := s.SummaryRepo.Upsert(ctx, sum); err != nil {
			stats.Duration = time.Since(start)
			return stats, fmt.Errorf("upsert summary for article %d: %w", art.ID, err)
		}
		stats.Summarized++

		logger.Info("swept article summarized",
			slog.Int64("article_id", art.ID),
			slog.String("url", art.URL),
			slog.String("summary_provider", provider))
	}

	stats.Duration = time.Since(start)
	return stats, nil
}
