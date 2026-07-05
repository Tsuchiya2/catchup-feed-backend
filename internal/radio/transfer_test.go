package radio_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/radio"
)

type transferRun struct {
	name string
	args []string
	err  error
}

func (c *transferRun) run(_ context.Context, name string, args ...string) error {
	c.name = name
	c.args = args
	return c.err
}

func TestRsyncTransfer(t *testing.T) {
	t.Run("assembles the rsync command and returns the Pi path", func(t *testing.T) {
		captured := &transferRun{}
		tr := &radio.RsyncTransfer{
			RsyncPath:   "/usr/bin/rsync",
			Dest:        "pi@pi.tailnet:/data/episodes/",
			EpisodesDir: "/data/episodes",
			Run:         captured.run,
		}

		audioPath, err := tr.Transfer(context.Background(), "/tmp/work/2026-07-05.mp3", "2026-07-05.mp3")
		require.NoError(t, err)

		assert.Equal(t, "/usr/bin/rsync", captured.name)
		assert.Equal(t, []string{"-t", "/tmp/work/2026-07-05.mp3", "pi@pi.tailnet:/data/episodes/2026-07-05.mp3"}, captured.args)
		assert.Equal(t, "/data/episodes/2026-07-05.mp3", audioPath, "DB stores the Pi-local path (C-10)")
	})

	t.Run("rsync failure propagates", func(t *testing.T) {
		tr := &radio.RsyncTransfer{
			RsyncPath:   "rsync",
			Dest:        "pi@pi.tailnet:/data/episodes",
			EpisodesDir: "/data/episodes",
			Run:         (&transferRun{err: errors.New("connection unexpectedly closed")}).run,
		}
		_, err := tr.Transfer(context.Background(), "/tmp/a.mp3", "a.mp3")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rsync")
	})
}

func TestLocalTransfer(t *testing.T) {
	src := filepath.Join(t.TempDir(), "episode.mp3")
	require.NoError(t, os.WriteFile(src, []byte("mp3-bytes"), 0o600))
	dstDir := filepath.Join(t.TempDir(), "episodes")

	tr := &radio.LocalTransfer{EpisodesDir: dstDir}
	audioPath, err := tr.Transfer(context.Background(), src, "2026-07-05.mp3")
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dstDir, "2026-07-05.mp3"), audioPath)
	copied, err := os.ReadFile(audioPath)
	require.NoError(t, err)
	assert.Equal(t, "mp3-bytes", string(copied))
}

func TestNewTransferer_ModeSelection(t *testing.T) {
	rsyncCfg := radio.Config{RsyncDest: "pi@pi:/data/episodes", RsyncPath: "rsync", EpisodesDir: "/data/episodes"}
	localCfg := radio.Config{EpisodesDir: "/data/episodes"}

	assert.IsType(t, &radio.RsyncTransfer{}, radio.NewTransferer(rsyncCfg))
	assert.IsType(t, &radio.LocalTransfer{}, radio.NewTransferer(localCfg),
		"RADIO_RSYNC_DEST 未設定はローカル完結モード (§6-5)")
}
