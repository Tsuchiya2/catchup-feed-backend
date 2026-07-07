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

	// defaultLearningURL is the dashboard grading page linked from private
	// show notes (Phase 3 §7.5: 聴取→採点の唯一の橋).
	defaultLearningURL = "https://pulse.catchup-feed.com/learning"

	// defaultBookReviewChunks is how many book_chunks one book_review covers
	// (§7.3: 初期値 2〜3チャンク≒紹介1トピック).
	defaultBookReviewChunks = 3

	// defaultPrivateEpisodeMaxMinutes caps the private episode length; over it
	// the book_review is deferred to tomorrow with no cursor advance (§7.1:
	// 18分を超える場合は book_review を翌日に回す).
	defaultPrivateEpisodeMaxMinutes = 18
)

// bookReviewEstimate is the assumed book_review length (§7.1: 書籍≒2分) added
// to the news+quiz duration for the §7.1 length guard. Deliberately a
// conservative constant, not env: the guard decides BEFORE generating so no
// Ollama/TTS work is spent on a day that is already too long, and erring
// toward deferral is the right 縮退 direction (翌日カーソル位置から再開).
// Revisit if BOOK_REVIEW_CHUNKS is raised well beyond the default.
const bookReviewEstimate = 3 * time.Minute

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
	// LearningURL is the grading page linked from private show notes
	// (RADIO_LEARNING_URL, Phase 3 §7.5).
	LearningURL string
	// BookReviewChunks is how many book_chunks one book_review covers
	// (BOOK_REVIEW_CHUNKS, §7.3).
	BookReviewChunks int
	// PrivateEpisodeMax caps the private episode length; over it book_review
	// is deferred (PRIVATE_EPISODE_MAX_MINUTES as minutes, §7.1).
	PrivateEpisodeMax time.Duration
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
//   - RADIO_LEARNING_URL: dashboard grading page for private show notes
//     (default https://pulse.catchup-feed.com/learning, Phase 3 §7.5)
//   - BOOK_REVIEW_CHUNKS: book_chunks per book_review (default 3, §7.3)
//   - PRIVATE_EPISODE_MAX_MINUTES: private episode length cap for the
//     book_review guard (default 18, §7.1)
func LoadConfig(logger *slog.Logger) (Config, error) {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := Config{
		ShowName:         pkgconfig.GetEnvString("RADIO_SHOW_NAME", defaultShowName),
		MaxArticles:      pkgconfig.GetEnvInt("RADIO_MAX_ARTICLES", defaultMaxArticles),
		EpisodesDir:      pkgconfig.GetEnvString("RADIO_EPISODES_DIR", defaultEpisodesDir),
		RsyncDest:        pkgconfig.GetEnvString("RADIO_RSYNC_DEST", ""),
		RsyncPath:        pkgconfig.GetEnvString("RADIO_RSYNC_PATH", "rsync"),
		Timeout:          pkgconfig.GetEnvDuration("RADIO_TIMEOUT", defaultRunTimeout),
		LearningURL:      pkgconfig.GetEnvString("RADIO_LEARNING_URL", defaultLearningURL),
		BookReviewChunks: pkgconfig.GetEnvInt("BOOK_REVIEW_CHUNKS", defaultBookReviewChunks),
		PrivateEpisodeMax: time.Duration(
			pkgconfig.GetEnvInt("PRIVATE_EPISODE_MAX_MINUTES", defaultPrivateEpisodeMaxMinutes)) * time.Minute,
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
	if cfg.BookReviewChunks <= 0 {
		logger.Warn("BOOK_REVIEW_CHUNKS must be positive, using default",
			slog.Int("value", cfg.BookReviewChunks), slog.Int("default", defaultBookReviewChunks))
		cfg.BookReviewChunks = defaultBookReviewChunks
	}
	if cfg.PrivateEpisodeMax <= 0 {
		logger.Warn("PRIVATE_EPISODE_MAX_MINUTES must be positive, using default",
			slog.Duration("value", cfg.PrivateEpisodeMax),
			slog.Int("default_minutes", defaultPrivateEpisodeMaxMinutes))
		cfg.PrivateEpisodeMax = defaultPrivateEpisodeMaxMinutes * time.Minute
	}

	tz := pkgconfig.GetEnvString("RADIO_TIMEZONE", defaultTimezone)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("radio: invalid RADIO_TIMEZONE %q: %w", tz, err)
	}
	cfg.Location = loc
	return cfg, nil
}
