package repository

import (
	"context"

	"catchup-feed/internal/domain/entity"
)

// EpisodeRepository persists radio episodes and their script segments
// (episodes / segments tables, §4). The radio batch inserts an episode
// together with its segments; the feed generator lists episodes per feed
// kind. Segment scripts are long-lived assets (Phase 3) and are never
// deleted here.
type EpisodeRepository interface {
	// Create inserts the episode and its segments in one transaction and
	// sets episode.ID and each segment's ID/EpisodeID.
	Create(ctx context.Context, episode *entity.Episode, segments []*entity.Segment) error
	// Get returns the episode, or nil when not found.
	Get(ctx context.Context, id int64) (*entity.Episode, error)
	// ListByKind returns up to limit episodes of the given feed kind,
	// newest first (RSS feed generation order).
	ListByKind(ctx context.Context, feedKind string, limit int) ([]*entity.Episode, error)
	// ListRecent returns up to limit episodes of every feed kind, newest
	// first — the private feed order (§5.1: the tailnet feed serves
	// private and public episodes alike).
	ListRecent(ctx context.Context, limit int) ([]*entity.Episode, error)
	// ListSegments returns the episode's segments ordered by position.
	ListSegments(ctx context.Context, episodeID int64) ([]*entity.Segment, error)
}
