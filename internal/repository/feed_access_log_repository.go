package repository

import (
	"context"

	"catchup-feed/internal/domain/entity"
)

// FeedAccessLogRepository persists public feed accesses
// (feed_access_logs table, §4). One row per authenticated request; the
// dashboard aggregates per friend via the token's subscriber (C-8).
type FeedAccessLogRepository interface {
	// Insert records one access and sets log.ID. AccessedAt is set by the
	// database when zero.
	Insert(ctx context.Context, log *entity.FeedAccessLog) error
	// ListRecent returns up to limit logs, newest first.
	ListRecent(ctx context.Context, limit int) ([]*entity.FeedAccessLog, error)
}
