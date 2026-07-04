package postgres

import (
	"context"
	"database/sql"
	"fmt"

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

// ListRecent returns up to limit logs, newest first (id DESC ~ insertion
// order, avoiding an extra accessed_at index on the fastest-growing table).
func (repo *FeedAccessLogRepo) ListRecent(ctx context.Context, limit int) ([]*entity.FeedAccessLog, error) {
	const query = `
SELECT id, token_id, episode_id, user_agent, accessed_at
FROM feed_access_logs
ORDER BY id DESC
LIMIT $1`
	rows, err := repo.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("ListRecent: %w", err)
	}
	defer func() { _ = rows.Close() }()

	logs := make([]*entity.FeedAccessLog, 0, limit)
	for rows.Next() {
		var log entity.FeedAccessLog
		if err := rows.Scan(
			&log.ID, &log.TokenID, &log.EpisodeID, &log.UserAgent, &log.AccessedAt,
		); err != nil {
			return nil, fmt.Errorf("ListRecent: %w", err)
		}
		logs = append(logs, &log)
	}
	return logs, rows.Err()
}
