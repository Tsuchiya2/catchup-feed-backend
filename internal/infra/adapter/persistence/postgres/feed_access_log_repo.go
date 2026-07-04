package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// FeedAccessLogRepo persists public feed accesses (§4).
type FeedAccessLogRepo struct{ db *sql.DB }

func NewFeedAccessLogRepo(db *sql.DB) repository.FeedAccessLogRepository {
	return &FeedAccessLogRepo{db: db}
}

// Insert records one access. AccessedAt is left to the DB default (now())
// and read back so the entity is complete.
func (repo *FeedAccessLogRepo) Insert(ctx context.Context, log *entity.FeedAccessLog) error {
	const query = `
INSERT INTO feed_access_logs (token_id, episode_id, user_agent)
VALUES ($1, $2, $3)
RETURNING id, accessed_at`
	err := repo.db.QueryRowContext(ctx, query,
		log.TokenID, log.EpisodeID, log.UserAgent,
	).Scan(&log.ID, &log.AccessedAt)
	if err != nil {
		return fmt.Errorf("Insert: %w", err)
	}
	return nil
}

// ListRecords returns up to limit accesses joined with their subscriber
// (C-8), newest first (id DESC ~ insertion order, avoiding an extra
// accessed_at index on the fastest-growing table). A non-nil subscriberID
// narrows the timeline to one friend; the $1 IS NULL guard keeps it a
// single placeholder-only query for both cases.
func (repo *FeedAccessLogRepo) ListRecords(ctx context.Context, subscriberID *int64, limit int) ([]*entity.FeedAccessRecord, error) {
	const query = `
SELECT l.id, l.token_id, l.episode_id, l.user_agent, l.accessed_at,
       t.subscriber_id, s.name
FROM feed_access_logs l
INNER JOIN feed_tokens t ON t.id = l.token_id
INNER JOIN subscribers s ON s.id = t.subscriber_id
WHERE $1::bigint IS NULL OR t.subscriber_id = $1
ORDER BY l.id DESC
LIMIT $2`
	rows, err := repo.db.QueryContext(ctx, query, subscriberID, limit)
	if err != nil {
		return nil, fmt.Errorf("ListRecords: %w", err)
	}
	defer func() { _ = rows.Close() }()

	records := make([]*entity.FeedAccessRecord, 0, limit)
	for rows.Next() {
		var rec entity.FeedAccessRecord
		if err := rows.Scan(
			&rec.ID, &rec.TokenID, &rec.EpisodeID, &rec.UserAgent, &rec.AccessedAt,
			&rec.SubscriberID, &rec.SubscriberName,
		); err != nil {
			return nil, fmt.Errorf("ListRecords: %w", err)
		}
		records = append(records, &rec)
	}
	return records, rows.Err()
}

// SummarizeBySubscriber aggregates accesses per friend in one SQL pass
// (single-user scale, no aggregation infrastructure — 設計原則 1). LEFT
// JOINs keep subscribers without tokens or accesses in the result so the
// dashboard can flag never-accessed friends too.
func (repo *FeedAccessLogRepo) SummarizeBySubscriber(ctx context.Context, since7d, since30d time.Time) ([]*entity.SubscriberAccessSummary, error) {
	const query = `
SELECT s.id, s.name, s.deactivated_at IS NULL AS active,
       MAX(l.accessed_at) AS last_accessed_at,
       COUNT(l.id) FILTER (WHERE l.accessed_at >= $1) AS count_7d,
       COUNT(l.id) FILTER (WHERE l.accessed_at >= $2) AS count_30d
FROM subscribers s
LEFT JOIN feed_tokens t ON t.subscriber_id = s.id
LEFT JOIN feed_access_logs l ON l.token_id = t.id
GROUP BY s.id, s.name, s.deactivated_at
ORDER BY s.id ASC`
	rows, err := repo.db.QueryContext(ctx, query, since7d, since30d)
	if err != nil {
		return nil, fmt.Errorf("SummarizeBySubscriber: %w", err)
	}
	defer func() { _ = rows.Close() }()

	summaries := make([]*entity.SubscriberAccessSummary, 0, 10)
	for rows.Next() {
		var s entity.SubscriberAccessSummary
		if err := rows.Scan(
			&s.SubscriberID, &s.SubscriberName, &s.Active,
			&s.LastAccessedAt, &s.Count7d, &s.Count30d,
		); err != nil {
			return nil, fmt.Errorf("SummarizeBySubscriber: %w", err)
		}
		summaries = append(summaries, &s)
	}
	return summaries, rows.Err()
}
