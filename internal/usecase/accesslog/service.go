// Package accesslog provides the feed access log use cases for the
// dashboard (§5.1 admin API): the per-friend timeline and the neglect
// detection summary (C-8: logs aggregate per friend via the token's
// subscriber).
package accesslog

import (
	"context"
	"fmt"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// Limits for the timeline listing. Right-sized single-user defaults: the
// table grows unbounded, everything else stays small.
const (
	DefaultLimit = 100
	MaxLimit     = 1000
)

// Summary is one friend's aggregated access row plus the derived neglect
// indicator, so the dashboard can flag "最終アクセスが N 日以上前" without
// re-deriving it client-side.
type Summary struct {
	entity.SubscriberAccessSummary
	// DaysSinceLastAccess is full days elapsed since the last access;
	// nil when the friend never accessed the feed.
	DaysSinceLastAccess *int
}

// Service provides access log queries. Aggregation is plain SQL in the
// repository — no metrics stack (設計原則 1).
type Service struct {
	Logs repository.FeedAccessLogRepository
	// Now returns the current time; nil means time.Now. Injected for
	// deterministic window / staleness computation in tests.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// List returns the access timeline, newest first. A non-nil subscriberID
// narrows it to one friend. limit <= 0 falls back to DefaultLimit and is
// capped at MaxLimit.
func (s *Service) List(ctx context.Context, subscriberID *int64, limit int) ([]*entity.FeedAccessRecord, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	records, err := s.Logs.ListRecords(ctx, subscriberID, limit)
	if err != nil {
		return nil, fmt.Errorf("list access logs: %w", err)
	}
	return records, nil
}

// Summarize returns one row per friend: last access, 7-day / 30-day
// counts and days since the last access (nil = never accessed).
func (s *Service) Summarize(ctx context.Context) ([]*Summary, error) {
	now := s.now()
	rows, err := s.Logs.SummarizeBySubscriber(ctx, now.AddDate(0, 0, -7), now.AddDate(0, 0, -30))
	if err != nil {
		return nil, fmt.Errorf("summarize access logs: %w", err)
	}
	summaries := make([]*Summary, 0, len(rows))
	for _, row := range rows {
		summary := &Summary{SubscriberAccessSummary: *row}
		if row.LastAccessedAt != nil {
			days := int(now.Sub(*row.LastAccessedAt).Hours() / 24)
			if days < 0 {
				days = 0 // clock skew guard: an access "in the future" is not neglect
			}
			summary.DaysSinceLastAccess = &days
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}
