// Command radio is the nightly episode batch (§3.2 / §6). It runs on the
// Mac (launchd, 04:30 JST — §3.3), connects to the Pi's PostgreSQL over the
// tailnet (DATABASE_URL), and generates one public episode: article
// selection -> LLM script -> VOICEVOX -> ffmpeg -> rsync -> DB registration.
//
// Exit codes: 0 = episode generated or cleanly skipped (no new articles,
// D-1); 1 = failure — the day is skipped and launchd retries tomorrow (§8:
// VOICEVOX 障害→当日スキップ).
//
// Flags:
//
//	-dry-run       run through script generation, print the scripts and
//	               show notes to stdout, skip TTS and every write (D-2:
//	               話者選定・プロンプト調整用)
//	-since <ts>    override the article-selection cursor (RFC 3339,
//	               e.g. 2026-07-04T00:00:00+09:00) for manual re-runs
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"catchup-feed/internal/domain/entity"
	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/infra/db"
	"catchup-feed/internal/infra/summarizer"
	"catchup-feed/internal/jobs"
	"catchup-feed/internal/learning"
	"catchup-feed/internal/radio"
	"catchup-feed/internal/repository"
	"catchup-feed/internal/script"
	"catchup-feed/internal/tts"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "generate the script only; print it and skip TTS / encoding / DB writes")
	sinceFlag := flag.String("since", "", "article selection cursor override (RFC 3339)")
	flag.Parse()

	logger := initLogger()

	opts := radio.RunOptions{DryRun: *dryRun}
	if *sinceFlag != "" {
		since, err := time.Parse(time.RFC3339, *sinceFlag)
		if err != nil {
			logger.Error("invalid -since value, want RFC 3339",
				slog.String("value", *sinceFlag), slog.Any("error", err))
			os.Exit(1)
		}
		opts.Since = &since
	}

	cfg, err := radio.LoadConfig(logger)
	if err != nil {
		logger.Error("failed to load radio configuration", slog.Any("error", err))
		os.Exit(1)
	}

	// D-3: 台本は要約と同一の Gemini -> Groq -> Ollama 連鎖。
	chain, err := summarizer.NewChainFromEnv(logger)
	if err != nil {
		logger.Error("failed to configure LLM fallback chain",
			slog.Any("error", err),
			slog.String("hint", "set GEMINI_API_KEY / GROQ_API_KEY or enable Ollama"))
		os.Exit(1)
	}

	database := db.Open()
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error("failed to close database", slog.Any("error", err))
		}
	}()

	voicevoxCfg := tts.LoadVoicevoxConfig()
	learningCfg := learning.LoadConfig(logger)
	logger.Info("radio batch starting",
		slog.String("show", cfg.ShowName),
		slog.Int("max_articles", cfg.MaxArticles),
		slog.Int("voicevox_speaker", voicevoxCfg.Speaker),
		slog.Float64("voicevox_speed", voicevoxCfg.SpeedScale),
		slog.Bool("rsync_mode", cfg.RsyncDest != ""),
		slog.Int("quiz_items_per_day", learningCfg.ItemsPerDay),
		slog.Bool("dry_run", *dryRun))

	pipeline := &radio.Pipeline{
		Articles:    pgRepo.NewRadioArticleRepo(database),
		Episodes:    pgRepo.NewEpisodeRepo(database),
		Jobs:        pgRepo.NewJobRepo(database),
		Script:      script.NewGenerator(chain, cfg.ShowName, logger),
		TTS:         tts.NewVoicevox(voicevoxCfg),
		Encoder:     tts.NewFFmpeg(),
		Transfer:    radio.NewTransferer(cfg),
		Learning:    pgRepo.NewLearningRepo(database),
		LearningCfg: learningCfg,
		Config:      cfg,
		Logger:      logger,
	}

	// Whole-run ceiling (RADIO_TIMEOUT, default 1h): the batch must never
	// wedge, but Ollama-only degraded runs may legitimately take long.
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	if err := pipeline.Run(ctx, opts); err != nil {
		if errors.Is(err, radio.ErrNoArticles) {
			logger.Info("no new summarized articles, skipping today's episode (D-1)")
			return
		}
		logger.Error("episode generation failed, skipping today (§8)", slog.Any("error", err))
		enqueueFailureNotice(logger, pgRepo.NewJobRepo(database), err)
		os.Exit(1)
	}
}

// enqueueFailureNotice queues a 'notify_error' job so the worker tells the
// admin the morning episode is missing (§8: VOICEVOX 障害→当日スキップ+通知).
// Strictly best-effort with its own fresh context: the run context may
// already be dead (timeout), and when the failure *is* the database, the
// enqueue fails too — then the notice is only in the logs and the silent
// morning is the signal. No retry loops here.
func enqueueFailureNotice(logger *slog.Logger, queue repository.JobRepository, runErr error) {
	payload, err := jobs.NewNotifyErrorPayload("radio", runErr.Error())
	if err != nil {
		logger.Error("failed to marshal failure notice", slog.Any("error", err))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := queue.Enqueue(ctx, entity.JobKindNotifyError, payload, time.Time{}); err != nil {
		logger.Error("failed to enqueue failure notice (best-effort, giving up)", slog.Any("error", err))
		return
	}
	logger.Info("failure notice enqueued for the worker (notify_error)")
}

func initLogger() *slog.Logger {
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	// Dry-run prints scripts to stdout; keep logs on stderr so the two
	// streams stay separable.
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)
	return logger
}
