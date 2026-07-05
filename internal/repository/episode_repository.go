package repository

import (
	"context"
	"time"

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
	// CountByKindSince returns how many episodes of the feed kind were
	// published at or after `since`. The radio batch counts today's
	// episodes to number same-day re-runs as new revisions (§6-6: 冪等性 —
	// 上書きせず rev 付与で新規版).
	CountByKindSince(ctx context.Context, feedKind string, since time.Time) (int, error)

	// ---- media retention (D-4: mp3 は直近45日で削除) ----
	// The episode row itself is never deleted: show notes and segment
	// scripts are Phase 3 assets. Cleanup deletes the mp3 file and clears
	// the file reference (audio_path / audio_bytes) on the row.

	// ListWithAudioBefore returns up to limit episodes published before
	// cutoff that still reference an audio file, oldest first.
	ListWithAudioBefore(ctx context.Context, cutoff time.Time, limit int) ([]*entity.Episode, error)
	// ClearAudio removes the file reference from the episode row
	// (audio_path = '', audio_bytes = 0) after the mp3 has been deleted.
	ClearAudio(ctx context.Context, id int64) error
	// ListAudioPaths returns every non-empty audio_path. The cleanup job
	// uses it to detect orphan mp3 files (rsync succeeded, INSERT failed)
	// that no episode row references.
	ListAudioPaths(ctx context.Context) ([]string, error)
}
