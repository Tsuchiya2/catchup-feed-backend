package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/jobs"
)

type fakeMediaStore struct {
	mu       sync.Mutex
	expired  []*entity.Episode
	paths    []string
	cleared  []int64
	listErr  error
	clearErr error
}

func (s *fakeMediaStore) ListWithAudioBefore(_ context.Context, _ time.Time, _ int) ([]*entity.Episode, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.expired, nil
}

func (s *fakeMediaStore) ClearAudio(_ context.Context, id int64) error {
	if s.clearErr != nil {
		return s.clearErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleared = append(s.cleared, id)
	return nil
}

func (s *fakeMediaStore) ListAudioPaths(_ context.Context) ([]string, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.paths, nil
}

// writeMP3 creates a file with the given modification age.
func writeMP3(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("mp3"), 0o600))
	mtime := time.Now().Add(-age)
	require.NoError(t, os.Chtimes(path, mtime, mtime))
	return path
}

func cleanupJob() *entity.Job {
	return &entity.Job{ID: 9, Kind: entity.JobKindCleanupOldMedia, Payload: []byte(`{}`)}
}

func TestCleanupHandler_Handle(t *testing.T) {
	t.Run("expired episode: file deleted and reference cleared (D-4)", func(t *testing.T) {
		dir := t.TempDir()
		oldPath := writeMP3(t, dir, "2026-05-01.mp3", 50*24*time.Hour)
		freshPath := writeMP3(t, dir, "2026-07-04.mp3", 24*time.Hour)

		store := &fakeMediaStore{
			expired: []*entity.Episode{{ID: 1, AudioPath: oldPath, PublishedAt: time.Now().Add(-50 * 24 * time.Hour)}},
			paths:   []string{oldPath, freshPath},
		}
		handler := &jobs.CleanupHandler{Episodes: store, AudioDir: dir, Logger: slog.New(slog.DiscardHandler)}
		require.NoError(t, handler.Handle(context.Background(), cleanupJob()))

		assert.NoFileExists(t, oldPath)
		assert.FileExists(t, freshPath)
		assert.Equal(t, []int64{1}, store.cleared)
	})

	t.Run("already-deleted file still clears the reference (idempotent)", func(t *testing.T) {
		dir := t.TempDir()
		store := &fakeMediaStore{
			expired: []*entity.Episode{{ID: 2, AudioPath: filepath.Join(dir, "gone.mp3")}},
		}
		handler := &jobs.CleanupHandler{Episodes: store, AudioDir: dir, Logger: slog.New(slog.DiscardHandler)}
		require.NoError(t, handler.Handle(context.Background(), cleanupJob()))
		assert.Equal(t, []int64{2}, store.cleared)
	})

	t.Run("path outside the audio dir is never deleted, reference cleared", func(t *testing.T) {
		dir := t.TempDir()
		outsideDir := t.TempDir()
		outside := writeMP3(t, outsideDir, "escape.mp3", 60*24*time.Hour)

		store := &fakeMediaStore{
			expired: []*entity.Episode{{ID: 3, AudioPath: outside}},
		}
		handler := &jobs.CleanupHandler{Episodes: store, AudioDir: dir, Logger: slog.New(slog.DiscardHandler)}
		require.NoError(t, handler.Handle(context.Background(), cleanupJob()))

		assert.FileExists(t, outside, "cleanup must not reach outside AudioDir")
		assert.Equal(t, []int64{3}, store.cleared)
	})

	t.Run("orphan mp3 handling (rsync 成功後 INSERT 失敗)", func(t *testing.T) {
		dir := t.TempDir()
		oldOrphan := writeMP3(t, dir, "orphan-old.mp3", 72*time.Hour)
		freshOrphan := writeMP3(t, dir, "orphan-fresh.mp3", time.Hour)
		referenced := writeMP3(t, dir, "2026-07-04.mp3", 72*time.Hour)
		rsyncTemp := writeMP3(t, dir, ".2026-07-05.mp3.Xy12", 72*time.Hour)
		notMP3 := writeMP3(t, dir, "notes.txt", 72*time.Hour)

		store := &fakeMediaStore{paths: []string{"/data/episodes/2026-07-04.mp3"}} // abs path, matched by base name
		handler := &jobs.CleanupHandler{Episodes: store, AudioDir: dir, Logger: slog.New(slog.DiscardHandler)}
		require.NoError(t, handler.Handle(context.Background(), cleanupJob()))

		assert.NoFileExists(t, oldOrphan, "old unreferenced mp3 is an orphan")
		assert.FileExists(t, freshOrphan, "a fresh file may belong to a batch still between rsync and INSERT")
		assert.FileExists(t, referenced, "referenced files stay regardless of age")
		assert.FileExists(t, rsyncTemp, "dot-prefixed rsync temp files are not touched")
		assert.FileExists(t, notMP3, "only mp3 files are considered")
	})

	t.Run("missing audio dir is a clean no-op", func(t *testing.T) {
		store := &fakeMediaStore{}
		handler := &jobs.CleanupHandler{
			Episodes: store,
			AudioDir: filepath.Join(t.TempDir(), "does-not-exist"),
			Logger:   slog.New(slog.DiscardHandler),
		}
		assert.NoError(t, handler.Handle(context.Background(), cleanupJob()))
	})

	t.Run("db errors are returned for a queue retry", func(t *testing.T) {
		handler := &jobs.CleanupHandler{
			Episodes: &fakeMediaStore{listErr: errors.New("db down")},
			AudioDir: t.TempDir(),
			Logger:   slog.New(slog.DiscardHandler),
		}
		err := handler.Handle(context.Background(), cleanupJob())
		require.Error(t, err)
		assert.False(t, jobs.IsPermanent(err))
	})

	t.Run("clear-audio failure keeps the delete error visible", func(t *testing.T) {
		dir := t.TempDir()
		oldPath := writeMP3(t, dir, "2026-05-01.mp3", 50*24*time.Hour)
		store := &fakeMediaStore{
			expired:  []*entity.Episode{{ID: 5, AudioPath: oldPath}},
			clearErr: errors.New("db down"),
		}
		handler := &jobs.CleanupHandler{Episodes: store, AudioDir: dir, Logger: slog.New(slog.DiscardHandler)}
		err := handler.Handle(context.Background(), cleanupJob())
		require.Error(t, err)
	})
}
