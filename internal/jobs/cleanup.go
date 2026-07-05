package jobs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"catchup-feed/internal/domain/entity"
)

// Retention defaults (D-4: mp3 は直近45日で削除).
const (
	// DefaultRetention is how long an episode keeps its mp3.
	DefaultRetention = 45 * 24 * time.Hour
	// DefaultOrphanMinAge protects freshly rsync'd files: the radio batch
	// transfers the mp3 *before* inserting the episode row (§6-5), so a
	// file is only an orphan once it has stayed unreferenced well past any
	// plausible batch duration.
	DefaultOrphanMinAge = 48 * time.Hour
	// purgeBatchLimit bounds one run; leftovers wait for tomorrow's job.
	purgeBatchLimit = 500
)

// EpisodeMediaStore is the slice of the episode repository the cleanup
// needs. Satisfied by repository.EpisodeRepository.
type EpisodeMediaStore interface {
	ListWithAudioBefore(ctx context.Context, cutoff time.Time, limit int) ([]*entity.Episode, error)
	ClearAudio(ctx context.Context, id int64) error
	ListAudioPaths(ctx context.Context) ([]string, error)
}

// CleanupHandler handles 'cleanup_old_media' (D-4): it deletes mp3 files
// of episodes older than the retention window and clears their file
// reference (the row itself — show notes, segments — survives as a Phase 3
// asset), then deletes orphan mp3 files that no episode row references
// (rsync 成功後 INSERT 失敗で発生し得る). Idempotent: a retry re-scans and
// finds less to do.
type CleanupHandler struct {
	Episodes EpisodeMediaStore
	// AudioDir is the episodes directory (same value the feed server
	// uses). Files are only ever deleted inside it.
	AudioDir     string
	Retention    time.Duration // 0 = DefaultRetention
	OrphanMinAge time.Duration // 0 = DefaultOrphanMinAge
	Logger       *slog.Logger
	Now          func() time.Time // nil = time.Now
}

// Handle runs one cleanup pass. Partial failures are joined and returned
// for a queue retry; every step is safe to repeat.
func (h *CleanupHandler) Handle(ctx context.Context, job *entity.Job) error {
	logger := h.logger().With(slog.Int64("job_id", job.ID))
	now := h.now()

	var errs []error
	errs = append(errs, h.purgeExpired(ctx, logger, now)...)
	errs = append(errs, h.deleteOrphans(ctx, logger, now)...)
	return errors.Join(errs...)
}

// purgeExpired deletes the mp3 and clears the file reference of every
// episode published before the retention cutoff.
func (h *CleanupHandler) purgeExpired(ctx context.Context, logger *slog.Logger, now time.Time) []error {
	cutoff := now.Add(-h.retention())
	episodes, err := h.Episodes.ListWithAudioBefore(ctx, cutoff, purgeBatchLimit)
	if err != nil {
		return []error{fmt.Errorf("cleanup: list expired episodes: %w", err)}
	}

	var errs []error
	for _, episode := range episodes {
		rel, ok := relInsideDir(h.AudioDir, episode.AudioPath)
		if !ok {
			// A path outside the audio dir is never deleted; clear the
			// reference so the row stops resurfacing every run.
			logger.Warn("cleanup: audio path outside audio dir, clearing reference only",
				slog.Int64("episode_id", episode.ID), slog.String("audio_path", episode.AudioPath))
		} else if err := os.Remove(filepath.Join(h.AudioDir, rel)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("cleanup: delete %s: %w", episode.AudioPath, err))
			continue // keep the reference; retry deletes it next run
		}
		if err := h.Episodes.ClearAudio(ctx, episode.ID); err != nil {
			errs = append(errs, fmt.Errorf("cleanup: clear audio of episode %d: %w", episode.ID, err))
			continue
		}
		logger.Info("cleanup: expired episode audio removed (D-4)",
			slog.Int64("episode_id", episode.ID),
			slog.Time("published_at", episode.PublishedAt),
			slog.String("audio_path", episode.AudioPath))
	}
	return errs
}

// deleteOrphans removes mp3 files in the audio dir that no episode row
// references and that are older than OrphanMinAge (so an mp3 rsync'd by a
// still-running radio batch is never touched).
func (h *CleanupHandler) deleteOrphans(ctx context.Context, logger *slog.Logger, now time.Time) []error {
	paths, err := h.Episodes.ListAudioPaths(ctx)
	if err != nil {
		return []error{fmt.Errorf("cleanup: list audio paths: %w", err)}
	}
	// Referenced files are matched by base name: audio_path values may be
	// absolute Pi paths while the scan below sees names relative to
	// AudioDir, and episode filenames (date + rev, §6-6) are unique.
	referenced := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		referenced[filepath.Base(path)] = struct{}{}
	}

	entries, err := os.ReadDir(h.AudioDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // nothing transferred yet
		}
		return []error{fmt.Errorf("cleanup: read audio dir: %w", err)}
	}

	var errs []error
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".mp3") {
			continue // rsync temp files are dot-prefixed; skip non-mp3
		}
		if _, ok := referenced[name]; ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			errs = append(errs, fmt.Errorf("cleanup: stat %s: %w", name, err))
			continue
		}
		if now.Sub(info.ModTime()) < h.orphanMinAge() {
			continue // possibly a batch still between rsync and INSERT
		}
		if err := os.Remove(filepath.Join(h.AudioDir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("cleanup: delete orphan %s: %w", name, err))
			continue
		}
		logger.Info("cleanup: orphan mp3 removed (unreferenced by episodes)",
			slog.String("file", name), slog.Time("mod_time", info.ModTime()))
	}
	return errs
}

// relInsideDir resolves an episodes.audio_path (absolute or relative) to a
// path relative to dir, refusing anything that escapes it — the same
// traversal guard the feed server applies before serving.
func relInsideDir(dir, path string) (string, bool) {
	if path == "" || dir == "" {
		return "", false
	}
	rel := path
	if filepath.IsAbs(path) {
		base, err := filepath.Abs(dir)
		if err != nil {
			return "", false
		}
		rel, err = filepath.Rel(base, path)
		if err != nil {
			return "", false
		}
	} else {
		rel = filepath.Clean(rel)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func (h *CleanupHandler) retention() time.Duration {
	if h.Retention > 0 {
		return h.Retention
	}
	return DefaultRetention
}

func (h *CleanupHandler) orphanMinAge() time.Duration {
	if h.OrphanMinAge > 0 {
		return h.OrphanMinAge
	}
	return DefaultOrphanMinAge
}

func (h *CleanupHandler) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

func (h *CleanupHandler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}
