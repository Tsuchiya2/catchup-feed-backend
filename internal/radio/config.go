// Package radio orchestrates the nightly episode batch (§6): article
// selection -> script generation -> VOICEVOX synthesis -> ffmpeg encoding ->
// transfer to the Pi -> DB registration -> job enqueue. It runs on the Mac
// and talks to the Pi only through PostgreSQL and rsync (C-4).
package radio

import (
	"fmt"
	"log/slog"
	"time"

	pkgconfig "catchup-feed/pkg/config"
)

const (
	// defaultMaxArticles is the on-air article cap (§6-1: 上限 N 件、
	// 初期値 8。超過分はショーノートにリンクのみ).
	defaultMaxArticles = 8

	// defaultShowName is the program name used in titles, prompts and ID3
	// tags (仮称 pulse).
	defaultShowName = "pulse"

	// defaultTimezone anchors the broadcast day (§3.3: 04:30 JST).
	defaultTimezone = "Asia/Tokyo"

	// defaultEpisodesDir matches the compose volume layout on the Pi.
	defaultEpisodesDir = "/data/episodes"

	// defaultRunTimeout bounds one whole batch run. One episode is minutes
	// of LLM + TTS + encoding work; an hour is a generous ceiling that
	// still guarantees the batch cannot wedge. Ollama-only degraded runs
	// may legitimately need more — override with RADIO_TIMEOUT.
	defaultRunTimeout = time.Hour
)

// Config holds the radio batch settings.
type Config struct {
	// ShowName is the program name (RADIO_SHOW_NAME).
	ShowName string
	// MaxArticles caps on-air articles per episode (RADIO_MAX_ARTICLES).
	MaxArticles int
	// Location anchors the broadcast day for titles and rev counting
	// (RADIO_TIMEZONE).
	Location *time.Location
	// EpisodesDir is the Pi-local episodes directory recorded in
	// episodes.audio_path (RADIO_EPISODES_DIR). In local mode (no rsync
	// destination) the mp3 is copied there directly.
	EpisodesDir string
	// RsyncDest is the rsync target, e.g. "pi@pi.tailnet:/data/episodes"
	// (RADIO_RSYNC_DEST). Empty selects local mode (開発・単一ホスト運用).
	RsyncDest string
	// RsyncPath is the rsync binary (RADIO_RSYNC_PATH).
	RsyncPath string
	// Timeout bounds one whole batch run (RADIO_TIMEOUT).
	Timeout time.Duration
}

// LoadConfig reads the radio batch settings from environment variables:
//
//   - RADIO_SHOW_NAME: program name (default "pulse")
//   - RADIO_MAX_ARTICLES: on-air article cap (default 8, §6-1)
//   - RADIO_TIMEZONE: broadcast-day timezone (default Asia/Tokyo)
//   - RADIO_EPISODES_DIR: Pi-local episodes dir for audio_path (default /data/episodes)
//   - RADIO_RSYNC_DEST: rsync destination; empty = copy locally into RADIO_EPISODES_DIR
//   - RADIO_RSYNC_PATH: rsync binary (default "rsync")
//   - RADIO_TIMEOUT: whole-run timeout as a Go duration (default 1h)
func LoadConfig(logger *slog.Logger) (Config, error) {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := Config{
		ShowName:    pkgconfig.GetEnvString("RADIO_SHOW_NAME", defaultShowName),
		MaxArticles: pkgconfig.GetEnvInt("RADIO_MAX_ARTICLES", defaultMaxArticles),
		EpisodesDir: pkgconfig.GetEnvString("RADIO_EPISODES_DIR", defaultEpisodesDir),
		RsyncDest:   pkgconfig.GetEnvString("RADIO_RSYNC_DEST", ""),
		RsyncPath:   pkgconfig.GetEnvString("RADIO_RSYNC_PATH", "rsync"),
		Timeout:     pkgconfig.GetEnvDuration("RADIO_TIMEOUT", defaultRunTimeout),
	}
	if cfg.Timeout <= 0 {
		logger.Warn("RADIO_TIMEOUT must be positive, using default",
			slog.Duration("value", cfg.Timeout), slog.Duration("default", defaultRunTimeout))
		cfg.Timeout = defaultRunTimeout
	}
	if cfg.MaxArticles <= 0 {
		logger.Warn("RADIO_MAX_ARTICLES must be positive, using default",
			slog.Int("value", cfg.MaxArticles), slog.Int("default", defaultMaxArticles))
		cfg.MaxArticles = defaultMaxArticles
	}

	tz := pkgconfig.GetEnvString("RADIO_TIMEZONE", defaultTimezone)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("radio: invalid RADIO_TIMEZONE %q: %w", tz, err)
	}
	cfg.Location = loc
	return cfg, nil
}
