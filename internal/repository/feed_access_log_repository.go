package repository

import (
	"context"
	"time"

	"catchup-feed/internal/domain/entity"
)

// FeedAccessLogRepository persists public feed accesses
// (feed_access_logs table, §4). One row per authenticated request; the
// dashboard aggregates per friend via the token's subscriber (C-8).
type FeedAccessLogRepository interface {
	// Insert records one access and sets log.ID. AccessedAt is set by the
	// database when zero.
	Insert(ctx context.Context, log *entity.FeedAccessLog) error
	// ListRecords returns up to limit accesses, newest first, each joined
	// with its subscriber (C-8). A non-nil subscriberID narrows the
	// timeline to one friend.
	ListRecords(ctx context.Context, subscriberID *int64, limit int) ([]*entity.FeedAccessRecord, error)
	// SummarizeBySubscriber aggregates accesses per subscriber: last
	// access and counts since since7d / since30d. Every subscriber
	// appears, including friends who never accessed the feed.
	SummarizeBySubscriber(ctx context.Context, since7d, since30d time.Time) ([]*entity.SubscriberAccessSummary, error)
}
