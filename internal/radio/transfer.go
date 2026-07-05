package radio

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Transferer moves the finished mp3 out of the temp dir and returns the
// Pi-local path stored in episodes.audio_path (C-10).
type Transferer interface {
	Transfer(ctx context.Context, localPath, filename string) (audioPath string, err error)
}

// RunFunc executes an external command (mirrors tts.RunFunc; kept local so
// the packages stay independent).
type RunFunc func(ctx context.Context, name string, args ...string) error

func execRun(ctx context.Context, name string, args ...string) error {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		const maxTail = 1024
		tail := string(out)
		if len(tail) > maxTail {
			tail = tail[len(tail)-maxTail:]
		}
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(tail))
	}
	return nil
}

// RsyncTransfer ships the mp3 to the Pi over Tailscale (§6-5: rsync over
// Tailscale). Dest is e.g. "pi@pi.tailnet:/data/episodes"; EpisodesDir is
// the same directory as seen from the Pi (recorded in the DB).
type RsyncTransfer struct {
	RsyncPath   string
	Dest        string
	EpisodesDir string
	Run         RunFunc // nil = real exec
}

func (r *RsyncTransfer) Transfer(ctx context.Context, localPath, filename string) (string, error) {
	run := r.Run
	if run == nil {
		run = execRun
	}
	dest := strings.TrimSuffix(r.Dest, "/") + "/" + filename
	// -t preserves mtime so repeated pushes stay idempotent on the Pi side.
	if err := run(ctx, r.RsyncPath, "-t", localPath, dest); err != nil {
		return "", fmt.Errorf("radio: rsync to %s: %w", dest, err)
	}
	return filepath.Join(r.EpisodesDir, filename), nil
}

// LocalTransfer copies the mp3 straight into the episodes directory —
// development and single-host mode when RADIO_RSYNC_DEST is unset (§6-5:
// ローカル完結モード).
type LocalTransfer struct {
	EpisodesDir string
}

func (l *LocalTransfer) Transfer(_ context.Context, localPath, filename string) (string, error) {
	if err := os.MkdirAll(l.EpisodesDir, 0o755); err != nil {
		return "", fmt.Errorf("radio: create episodes dir: %w", err)
	}
	dst := filepath.Join(l.EpisodesDir, filename)
	if err := copyFile(localPath, dst); err != nil {
		return "", fmt.Errorf("radio: copy episode: %w", err)
	}
	return dst, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// NewTransferer picks rsync or local mode from the config (§6-5).
func NewTransferer(cfg Config) Transferer {
	if cfg.RsyncDest != "" {
		return &RsyncTransfer{RsyncPath: cfg.RsyncPath, Dest: cfg.RsyncDest, EpisodesDir: cfg.EpisodesDir}
	}
	return &LocalTransfer{EpisodesDir: cfg.EpisodesDir}
}
