package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

const episodeColumns = "id, feed_kind, title, show_notes, audio_path, audio_bytes, duration_sec, published_at"

// EpisodeRepo persists radio episodes and their segments (§4).
type EpisodeRepo struct{ db *sql.DB }

func NewEpisodeRepo(db *sql.DB) repository.EpisodeRepository {
	return &EpisodeRepo{db: db}
}

func scanEpisode(s scanner) (*entity.Episode, error) {
	var episode entity.Episode
	if err := s.Scan(
		&episode.ID, &episode.FeedKind, &episode.Title, &episode.ShowNotes,
		&episode.AudioPath, &episode.AudioBytes, &episode.DurationSec, &episode.PublishedAt,
	); err != nil {
		return nil, err
	}
	return &episode, nil
}

// Create inserts the episode and its segments in one transaction. It sets
// episode.ID (and PublishedAt when defaulted by the DB) and each segment's
// ID / EpisodeID.
//
// A non-zero episode.PublishedAt is stored verbatim: the radio batch sets
// it to its article-selection timestamp so the next run's cursor (§6-1:
// 前回 public エピソード以降) starts exactly where this run's SELECT ran —
// summaries the worker created during the batch are not lost in the
// SELECT-to-INSERT window. A zero value falls back to the DB's now().
func (repo *EpisodeRepo) Create(ctx context.Context, episode *entity.Episode, segments []*entity.Segment) error {
	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("Create: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var publishedAt sql.NullTime
	if !episode.PublishedAt.IsZero() {
		publishedAt = sql.NullTime{Time: episode.PublishedAt, Valid: true}
	}
	const insertEpisode = `
INSERT INTO episodes (feed_kind, title, show_notes, audio_path, audio_bytes, duration_sec, published_at)
VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7, now()))
RETURNING id, published_at`
	if err := tx.QueryRowContext(ctx, insertEpisode,
		episode.FeedKind, episode.Title, episode.ShowNotes,
		episode.AudioPath, episode.AudioBytes, episode.DurationSec, publishedAt,
	).Scan(&episode.ID, &episode.PublishedAt); err != nil {
		return fmt.Errorf("Create: episode: %w", err)
	}

	const insertSegment = `
INSERT INTO segments (episode_id, position, kind, article_id, script)
VALUES ($1, $2, $3, $4, $5)
RETURNING id`
	for _, segment := range segments {
		segment.EpisodeID = episode.ID
		if err := tx.QueryRowContext(ctx, insertSegment,
			segment.EpisodeID, segment.Position, segment.Kind,
			segment.ArticleID, segment.Script,
		).Scan(&segment.ID); err != nil {
			return fmt.Errorf("Create: segment %d: %w", segment.Position, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("Create: commit: %w", err)
	}
	return nil
}

// Get returns the episode, or nil when not found.
func (repo *EpisodeRepo) Get(ctx context.Context, id int64) (*entity.Episode, error) {
	query := `
SELECT ` + episodeColumns + `
FROM episodes
WHERE id = $1
LIMIT 1`
	episode, err := scanEpisode(repo.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get: %w", err)
	}
	return episode, nil
}

// ListByKind returns up to limit episodes of the given feed kind, newest
// first — the RSS feed generation order (§5).
func (repo *EpisodeRepo) ListByKind(ctx context.Context, feedKind string, limit int) ([]*entity.Episode, error) {
	query := `
SELECT ` + episodeColumns + `
FROM episodes
WHERE feed_kind = $1
ORDER BY published_at DESC, id DESC
LIMIT $2`
	rows, err := repo.db.QueryContext(ctx, query, feedKind, limit)
	if err != nil {
		return nil, fmt.Errorf("ListByKind: %w", err)
	}
	defer func() { _ = rows.Close() }()

	episodes := make([]*entity.Episode, 0, limit)
	for rows.Next() {
		episode, err := scanEpisode(rows)
		if err != nil {
			return nil, fmt.Errorf("ListByKind: %w", err)
		}
		episodes = append(episodes, episode)
	}
	return episodes, rows.Err()
}

// ListRecent returns up to limit episodes of every feed kind, newest
// first — the private feed order (§5.1).
func (repo *EpisodeRepo) ListRecent(ctx context.Context, limit int) ([]*entity.Episode, error) {
	query := `
SELECT ` + episodeColumns + `
FROM episodes
ORDER BY published_at DESC, id DESC
LIMIT $1`
	rows, err := repo.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("ListRecent: %w", err)
	}
	defer func() { _ = rows.Close() }()

	episodes := make([]*entity.Episode, 0, limit)
	for rows.Next() {
		episode, err := scanEpisode(rows)
		if err != nil {
			return nil, fmt.Errorf("ListRecent: %w", err)
		}
		episodes = append(episodes, episode)
	}
	return episodes, rows.Err()
}

// CountByKindSince returns how many episodes of the feed kind were
// published at or after since (rev numbering for same-day re-runs, §6-6).
func (repo *EpisodeRepo) CountByKindSince(ctx context.Context, feedKind string, since time.Time) (int, error) {
	const query = `
SELECT count(*)
FROM episodes
WHERE feed_kind = $1 AND published_at >= $2`
	var count int
	if err := repo.db.QueryRowContext(ctx, query, feedKind, since).Scan(&count); err != nil {
		return 0, fmt.Errorf("CountByKindSince: %w", err)
	}
	return count, nil
}

// ListSegments returns the episode's segments ordered by position.
func (repo *EpisodeRepo) ListSegments(ctx context.Context, episodeID int64) ([]*entity.Segment, error) {
	const query = `
SELECT id, episode_id, position, kind, article_id, script
FROM segments
WHERE episode_id = $1
ORDER BY position ASC`
	rows, err := repo.db.QueryContext(ctx, query, episodeID)
	if err != nil {
		return nil, fmt.Errorf("ListSegments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	segments := make([]*entity.Segment, 0, 16)
	for rows.Next() {
		var segment entity.Segment
		if err := rows.Scan(
			&segment.ID, &segment.EpisodeID, &segment.Position,
			&segment.Kind, &segment.ArticleID, &segment.Script,
		); err != nil {
			return nil, fmt.Errorf("ListSegments: %w", err)
		}
		segments = append(segments, &segment)
	}
	return segments, rows.Err()
}
