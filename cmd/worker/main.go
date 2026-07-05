// Command worker is the Pi-resident daemon (§3.2 / §3.3): robfig/cron
// drives the hourly crawl → summarize pipeline, and a jobs-table consumer
// executes the follow-up work the radio batch enqueues (regenerate_feed,
// notify_episode, notify_error) plus the daily media retention job (D-4).
// All inter-process coordination happens through PostgreSQL (C-4).
package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/robfig/cron/v3"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/feed"
	hhttp "catchup-feed/internal/handler/http/respond"
	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/infra/db"
	"catchup-feed/internal/infra/fetcher"
	"catchup-feed/internal/infra/scraper"
	"catchup-feed/internal/infra/summarizer"
	workerPkg "catchup-feed/internal/infra/worker"
	"catchup-feed/internal/jobs"
	"catchup-feed/internal/notify"
	"catchup-feed/internal/repository"
	fetchUC "catchup-feed/internal/usecase/fetch"
	pkgconfig "catchup-feed/pkg/config"
)

// cleanupCronDefault schedules the daily cleanup_old_media enqueue (D-4:
// worker の日次ジョブ), after the 04:30 radio batch window.
const cleanupCronDefault = "30 6 * * *"

func waitForMigrations(logger *slog.Logger, db *sql.DB) {
	const probe = "SELECT 1 FROM sources LIMIT 1"
	for i := 0; i < 10; i++ {
		if _, err := db.Exec(probe); err == nil {
			return
		}
		logger.Info("waiting for migrations, retrying in 3s", slog.Int("attempt", i+1))
		time.Sleep(3 * time.Second)
	}
	logger.Error("migrations did not complete in time")
	os.Exit(1)
}

func main() {
	logger := initLogger()
	database := initDatabase(logger)
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error("failed to close database", slog.Any("error", err))
		}
	}()

	// SIGINT/SIGTERM stop the consumer loop and the main wait.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Load worker configuration (fail-open strategy)
	workerConfig, err := workerPkg.LoadConfigFromEnv(logger)
	if err != nil {
		logger.Error("failed to load worker configuration", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("worker configuration loaded",
		slog.String("cron_schedule", workerConfig.CronSchedule),
		slog.String("timezone", workerConfig.Timezone),
		slog.Duration("crawl_timeout", workerConfig.CrawlTimeout),
		slog.Int("health_port", workerConfig.HealthPort))

	// Start health check server
	healthAddr := fmt.Sprintf(":%d", workerConfig.HealthPort)
	healthServer := workerPkg.NewHealthServer(healthAddr, logger)
	go func() {
		if err := healthServer.Start(ctx); err != nil && err != http.ErrServerClosed {
			logger.Error("health server failed", slog.Any("error", err))
		}
	}()
	logger.Info("health check server started", slog.String("addr", healthAddr))

	svc := setupFetchService(logger, database)

	// jobs consumer (§3.3): drains the queue the radio batch feeds.
	consumer := setupJobsConsumer(logger, database)
	go func() {
		if err := consumer.Run(ctx); err != nil && ctx.Err() == nil {
			logger.Error("jobs consumer stopped unexpectedly", slog.Any("error", err))
		}
	}()

	startCronWorker(ctx, logger, svc, workerConfig, healthServer, pgRepo.NewJobRepo(database))
}

// initLogger initializes and returns a structured logger based on environment configuration.
func initLogger() *slog.Logger {
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)
	return logger
}

// initDatabase opens the database connection and waits for migrations to complete.
func initDatabase(logger *slog.Logger) *sql.DB {
	database := db.Open()
	waitForMigrations(logger, database)
	return database
}

// setupJobsConsumer wires the §3.3 consumer: destinations from environment
// (D-7: 宣言的に有効/無効), the friend mailer (C-11) and the four Phase 1
// handlers. Feed config supplies the audio dir (D-4 cleanup) and the
// private base URL used for the admin-facing episode link.
func setupJobsConsumer(logger *slog.Logger, database *sql.DB) *jobs.Consumer {
	destinations := notify.LoadDestinationsFromEnv(logger)
	mailer := notify.LoadSMTPFromEnv(logger)
	feedCfg := feed.LoadConfig()
	episodeRepo := pgRepo.NewEpisodeRepo(database)

	episodeHandler := &jobs.NotifyEpisodeHandler{
		Episodes:       episodeRepo,
		Subscribers:    pgRepo.NewSubscriberRepo(database),
		Destinations:   destinations,
		PrivateBaseURL: feedCfg.PrivateBaseURL,
		AudioDir:       feedCfg.AudioDir,
		Logger:         logger,
	}
	if mailer != nil {
		episodeHandler.Mailer = mailer
	}

	return &jobs.Consumer{
		Jobs: pgRepo.NewJobRepo(database),
		Handlers: map[string]jobs.Handler{
			entity.JobKindRegenerateFeed: jobs.NewRegenerateFeedHandler(logger),
			entity.JobKindNotifyEpisode:  episodeHandler,
			entity.JobKindNotifyError:    &jobs.NotifyErrorHandler{Destinations: destinations, Logger: logger},
			entity.JobKindCleanupOldMedia: &jobs.CleanupHandler{
				Episodes: episodeRepo,
				AudioDir: feedCfg.AudioDir,
				Logger:   logger,
			},
		},
		PollInterval: pkgconfig.GetEnvDuration("JOBS_POLL_INTERVAL", jobs.DefaultPollInterval),
		Logger:       logger,
	}
}

// setupFetchService creates and configures the fetch service with all dependencies.
func setupFetchService(logger *slog.Logger, database *sql.DB) fetchUC.Service {
	srcRepo := pgRepo.NewSourceRepo(database)
	artRepo := pgRepo.NewArticleRepo(database)

	sum := createSummarizer(logger)
	httpClient := createHTTPClient()
	feedFetcher := scraper.NewRSSFetcher(httpClient)

	// Load content fetch configuration from environment
	contentFetchConfig, err := fetcher.LoadConfigFromEnv()
	if err != nil {
		logger.Error("Failed to load content fetch configuration",
			slog.Any("error", err))
		logger.Warn("Content fetching disabled due to configuration error")
		contentFetchConfig = fetcher.DefaultConfig()
		contentFetchConfig.Enabled = false
	}

	// Create ContentFetcher if enabled
	var contentFetcher fetchUC.ContentFetcher
	if contentFetchConfig.Enabled {
		contentFetcher = fetcher.NewReadabilityFetcher(contentFetchConfig)
		logger.Info("Content fetching enabled",
			slog.Int("threshold", contentFetchConfig.Threshold),
			slog.Int("parallelism", contentFetchConfig.Parallelism),
			slog.Duration("timeout", contentFetchConfig.Timeout))
	} else {
		logger.Info("Content fetching disabled")
		contentFetcher = nil
	}

	// Create fetch service configuration from the loaded content config
	fetchConfig := fetchUC.ContentFetchConfig{
		Parallelism: contentFetchConfig.Parallelism,
		Threshold:   contentFetchConfig.Threshold,
	}

	return fetchUC.NewService(
		srcRepo,
		artRepo,
		sum,
		feedFetcher,
		contentFetcher,
		fetchConfig,
	)
}

// createSummarizer builds the Gemini -> Groq -> Ollama fallback chain from
// environment variables (GEMINI_API_KEY, GROQ_API_KEY, OLLAMA_HOST, ...).
// Providers without an API key are excluded automatically. The worker cannot
// run without at least one provider, so an empty chain is fatal.
func createSummarizer(logger *slog.Logger) fetchUC.Summarizer {
	chain, err := summarizer.NewChainFromEnv(logger)
	if err != nil {
		logger.Error("failed to configure summarizer fallback chain",
			slog.Any("error", err),
			slog.String("hint", "set GEMINI_API_KEY / GROQ_API_KEY or enable Ollama"))
		os.Exit(1)
	}
	return chain
}

// createHTTPClient creates an HTTP client with timeouts and connection pooling.
// TLS 1.2+ is enforced for security.
func createHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12, // Enforce TLS 1.2+
			},
		},
	}
}

// startCronWorker starts the cron scheduler (crawl + daily cleanup
// enqueue) and blocks until ctx is done.
func startCronWorker(ctx context.Context, logger *slog.Logger, svc fetchUC.Service, cfg *workerPkg.WorkerConfig, healthServer *workerPkg.HealthServer, jobQueue repository.JobRepository) {
	// Load timezone
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		logger.Error("invalid timezone, using UTC", slog.String("timezone", cfg.Timezone), slog.Any("error", err))
		loc = time.UTC
	}
	c := cron.New(cron.WithLocation(loc))

	_, err = c.AddFunc(cfg.CronSchedule, func() {
		runCrawlJob(logger, svc, cfg)
	})
	if err != nil {
		logger.Error("failed to add cron job", slog.Any("error", err))
		os.Exit(1)
	}

	// D-4: enqueue the daily media retention job. Going through the queue
	// (instead of running inline) gives the cleanup the same retry /
	// last_error bookkeeping as every other job.
	cleanupSchedule := pkgconfig.GetEnvString("CLEANUP_CRON_SCHEDULE", cleanupCronDefault)
	_, err = c.AddFunc(cleanupSchedule, func() {
		if _, err := jobQueue.Enqueue(context.Background(), entity.JobKindCleanupOldMedia, nil, time.Time{}); err != nil {
			logger.Error("failed to enqueue cleanup_old_media", slog.Any("error", err))
		}
	})
	if err != nil {
		logger.Error("failed to add cleanup cron job", slog.Any("error", err))
		os.Exit(1)
	}
	c.Start()

	// Mark as ready after cron is set up
	healthServer.SetReady(true)
	logger.Info("worker marked as ready")

	logger.Info("worker started",
		slog.String("schedule", cfg.CronSchedule),
		slog.String("cleanup_schedule", cleanupSchedule),
		slog.String("timezone", cfg.Timezone))

	<-ctx.Done()
	logger.Info("shutting down")
	<-c.Stop().Done()
}

// runCrawlJob executes a single crawl job with timeout and error handling.
func runCrawlJob(logger *slog.Logger, svc fetchUC.Service, cfg *workerPkg.WorkerConfig) {
	startTime := time.Now()
	logger.Info("crawl started")

	// クロール処理のタイムアウト（設定から取得）
	ctx, cancel := context.WithTimeout(context.Background(), cfg.CrawlTimeout)
	defer cancel()

	stats, err := svc.CrawlAllSources(ctx)
	if err != nil {
		// 機密情報をマスクしてログ出力
		logger.Error("crawl failed",
			slog.Any("error", hhttp.SanitizeError(err)),
			slog.Duration("duration", time.Since(startTime)))
		return
	}

	logger.Info("crawl completed",
		slog.Int("sources", stats.Sources),
		slog.Int64("feed_items", stats.FeedItems),
		slog.Int64("inserted", stats.Inserted),
		slog.Int64("duplicated", stats.Duplicated),
		slog.Int64("summarize_errors", stats.SummarizeError),
		slog.Duration("duration", stats.Duration),
	)
}
