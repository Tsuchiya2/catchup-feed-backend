package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/robfig/cron/v3"

	"catchup-feed/internal/config"
	hhttp "catchup-feed/internal/handler/http/respond"
	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/infra/db"
	"catchup-feed/internal/infra/fetcher"
	"catchup-feed/internal/infra/grpc"
	"catchup-feed/internal/infra/notifier"
	"catchup-feed/internal/infra/scraper"
	"catchup-feed/internal/infra/summarizer"
	workerPkg "catchup-feed/internal/infra/worker"
	aiUC "catchup-feed/internal/usecase/ai"
	fetchUC "catchup-feed/internal/usecase/fetch"
	"catchup-feed/internal/usecase/notify"
)

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

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load worker configuration (fail-open strategy)
	workerMetrics := workerPkg.NewWorkerMetrics()
	workerMetrics.MustRegister()
	workerConfig, err := workerPkg.LoadConfigFromEnv(logger, workerMetrics)
	if err != nil {
		logger.Error("failed to load worker configuration", slog.Any("error", err))
		os.Exit(1)
	}
	logger.Info("worker configuration loaded",
		slog.String("cron_schedule", workerConfig.CronSchedule),
		slog.String("timezone", workerConfig.Timezone),
		slog.Int("notify_max_concurrent", workerConfig.NotifyMaxConcurrent),
		slog.Duration("crawl_timeout", workerConfig.CrawlTimeout),
		slog.Int("health_port", workerConfig.HealthPort))

	// Initialize Discord notification channel
	discordConfig := loadDiscordConfig(logger)
	var discordChannel notify.Channel
	if discordConfig.Enabled {
		discordChannel = notify.NewDiscordChannel(discordConfig)
		logger.Info("Discord channel initialized", slog.String("status", "enabled"))
	} else {
		logger.Info("Discord channel disabled")
	}

	// Initialize Slack notification channel
	slackConfig := loadSlackConfig(logger)
	var slackChannel notify.Channel
	if slackConfig.Enabled {
		slackChannel = notify.NewSlackChannel(slackConfig)
		logger.Info("Slack channel initialized", slog.String("status", "enabled"))
	} else {
		logger.Info("Slack channel disabled")
	}

	// Initialize notification service (use workerConfig)
	var channels []notify.Channel
	if discordChannel != nil {
		channels = append(channels, discordChannel)
	}
	if slackChannel != nil {
		channels = append(channels, slackChannel)
	}

	notifyService := notify.NewService(channels, workerConfig.NotifyMaxConcurrent)
	logger.Info("Notification service initialized",
		slog.Int("channels", len(channels)),
		slog.Int("max_concurrent", workerConfig.NotifyMaxConcurrent))

	// Start metrics HTTP server
	startMetricsServer(ctx, logger, notifyService)

	// Start health check server
	healthAddr := fmt.Sprintf(":%d", workerConfig.HealthPort)
	healthServer := workerPkg.NewHealthServer(healthAddr, logger)
	go func() {
		if err := healthServer.Start(ctx); err != nil && err != http.ErrServerClosed {
			logger.Error("health server failed", slog.Any("error", err))
		}
	}()
	logger.Info("health check server started", slog.String("addr", healthAddr))

	svc, aiCleanup := setupFetchService(logger, database, notifyService)
	defer aiCleanup()

	startCronWorker(logger, svc, workerConfig, workerMetrics, healthServer)
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

// setupFetchService creates and configures the fetch service with all dependencies.
// Returns the service and a cleanup function for graceful shutdown.
func setupFetchService(logger *slog.Logger, database *sql.DB, notifyService notify.Service) (fetchUC.Service, func()) {
	srcRepo := pgRepo.NewSourceRepo(database)
	artRepo := pgRepo.NewArticleRepo(database)

	sum := createSummarizer(logger)
	httpClient := createHTTPClient()
	feedFetcher := scraper.NewRSSFetcher(httpClient)

	// Create web scraper HTTP client with SSRF protection
	webScraperClient := createWebScraperHTTPClient()

	// Create web scraper factory and generate scrapers
	scraperFactory := scraper.NewScraperFactory(webScraperClient)
	webScrapers := scraperFactory.CreateScrapers()
	logger.Info("Web scrapers initialized",
		slog.Int("count", len(webScrapers)))

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

	// Setup AI embedding hook
	embeddingHook, aiCleanup := setupEmbeddingHook(logger)

	service := fetchUC.NewService(
		srcRepo,
		artRepo,
		sum,
		feedFetcher,
		webScrapers, // NEW: Web scraper registry
		contentFetcher,
		notifyService,
		embeddingHook, // NEW: AI embedding hook
		fetchConfig,
	)

	return service, aiCleanup
}

// setupEmbeddingHook creates an AI embedding hook and returns a cleanup function.
// The cleanup function should be called during shutdown to close the gRPC connection.
func setupEmbeddingHook(logger *slog.Logger) (fetchUC.EmbeddingHook, func()) {
	// Load AI configuration
	aiConfig, err := config.LoadAIConfig()
	if err != nil {
		logger.Warn("Failed to load AI configuration, AI features disabled", slog.Any("error", err))
		return nil, func() {}
	}

	// Validate configuration
	if err := aiConfig.Validate(); err != nil {
		logger.Warn("Invalid AI configuration, AI features disabled", slog.Any("error", err))
		return nil, func() {}
	}

	// Check if AI is enabled
	if !aiConfig.Enabled {
		logger.Info("AI features disabled via configuration")
		return nil, func() {}
	}

	// Create AI provider
	provider, err := grpc.NewGRPCAIProvider(aiConfig)
	if err != nil {
		logger.Warn("Failed to create AI provider, AI features disabled", slog.Any("error", err))
		return nil, func() {}
	}

	logger.Info("AI embedding hook initialized",
		slog.String("grpc_address", aiConfig.GRPCAddress))

	// Return cleanup function to close gRPC connection
	cleanup := func() {
		if err := provider.Close(); err != nil {
			logger.Error("failed to close AI provider", slog.Any("error", err))
		} else {
			logger.Info("AI provider closed")
		}
	}

	return aiUC.NewEmbeddingHook(provider, aiConfig.Enabled), cleanup
}

// createSummarizer creates a summarizer based on the SUMMARIZER_TYPE environment variable.
func createSummarizer(logger *slog.Logger) fetchUC.Summarizer {
	summarizerType := os.Getenv("SUMMARIZER_TYPE")
	if summarizerType == "" {
		summarizerType = "claude"
	}

	switch summarizerType {
	case "claude":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			logger.Error("ANTHROPIC_API_KEY is required when SUMMARIZER_TYPE=claude")
			os.Exit(1)
		}
		logger.Info("Using Claude API for summarization", slog.String("type", "claude"))
		return summarizer.NewClaude(apiKey)
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			logger.Error("OPENAI_API_KEY is required when SUMMARIZER_TYPE=openai")
			os.Exit(1)
		}
		// Load and validate OpenAI configuration
		config, err := summarizer.LoadOpenAIConfig()
		if err != nil {
			logger.Error("Failed to load OpenAI configuration", slog.Any("error", err))
			os.Exit(1)
		}
		logger.Info("Using OpenAI API for summarization",
			slog.String("type", "openai"),
			slog.Int("character_limit", config.GetCharacterLimit()))
		return summarizer.NewOpenAI(apiKey, config)
	default:
		logger.Error("Invalid SUMMARIZER_TYPE",
			slog.String("type", summarizerType),
			slog.String("expected", "openai or claude"))
		os.Exit(1)
		return nil
	}
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

// createWebScraperHTTPClient creates an HTTP client for web scraping with SSRF protection.
// It has shorter timeouts and validates redirects to prevent security issues.
func createWebScraperHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second, // Shorter timeout for scraping
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12, // Enforce TLS 1.2+
			},
		},
		// Redirect validation is handled by the scraper implementations
	}
}

// loadDiscordConfig loads Discord configuration from environment variables.
//
// Environment variables:
//   - DISCORD_ENABLED: Boolean flag to enable Discord notifications (default: false)
//   - DISCORD_WEBHOOK_URL: Discord webhook URL (required if enabled)
//
// Returns:
//   - notifier.DiscordConfig: Configuration with validation applied
func loadDiscordConfig(logger *slog.Logger) notifier.DiscordConfig {
	enabled := os.Getenv("DISCORD_ENABLED") == "true"
	webhookURL := os.Getenv("DISCORD_WEBHOOK_URL")

	if !enabled {
		return notifier.DiscordConfig{Enabled: false}
	}

	// Validate webhook URL format
	if webhookURL == "" {
		logger.Warn("Discord webhook URL is empty, disabling notifications")
		return notifier.DiscordConfig{Enabled: false}
	}

	u, err := url.Parse(webhookURL)
	if err != nil {
		logger.Warn("Invalid Discord webhook URL format, disabling notifications", slog.Any("error", err))
		return notifier.DiscordConfig{Enabled: false}
	}

	if u.Scheme != "https" {
		logger.Warn("Discord webhook URL must use HTTPS, disabling notifications")
		return notifier.DiscordConfig{Enabled: false}
	}

	if u.Host != "discord.com" {
		logger.Warn("Invalid Discord webhook host, disabling notifications", slog.String("host", u.Host))
		return notifier.DiscordConfig{Enabled: false}
	}

	if !strings.HasPrefix(u.Path, "/api/webhooks/") {
		logger.Warn("Invalid Discord webhook path, disabling notifications", slog.String("path", u.Path))
		return notifier.DiscordConfig{Enabled: false}
	}

	return notifier.DiscordConfig{
		Enabled:    true,
		WebhookURL: webhookURL,
		Timeout:    30 * time.Second,
	}
}

// loadSlackConfig loads Slack configuration from environment variables.
//
// Environment variables:
//   - SLACK_ENABLED: Boolean flag to enable Slack notifications (default: false)
//   - SLACK_WEBHOOK_URL: Slack webhook URL (required if enabled)
//
// Returns:
//   - notifier.SlackConfig: Configuration with validation applied
func loadSlackConfig(logger *slog.Logger) notifier.SlackConfig {
	enabled := os.Getenv("SLACK_ENABLED") == "true"
	webhookURL := os.Getenv("SLACK_WEBHOOK_URL")

	if !enabled {
		return notifier.SlackConfig{Enabled: false}
	}

	// Validate webhook URL format
	if webhookURL == "" {
		logger.Warn("Slack webhook URL is empty, disabling notifications")
		return notifier.SlackConfig{Enabled: false}
	}

	u, err := url.Parse(webhookURL)
	if err != nil {
		logger.Warn("Invalid Slack webhook URL format, disabling notifications", slog.Any("error", err))
		return notifier.SlackConfig{Enabled: false}
	}

	if u.Scheme != "https" {
		logger.Warn("Slack webhook URL must use HTTPS, disabling notifications")
		return notifier.SlackConfig{Enabled: false}
	}

	if u.Host != "hooks.slack.com" {
		logger.Warn("Invalid Slack webhook host, disabling notifications", slog.String("host", u.Host))
		return notifier.SlackConfig{Enabled: false}
	}

	if !strings.HasPrefix(u.Path, "/services/") {
		logger.Warn("Invalid Slack webhook path, disabling notifications", slog.String("path", u.Path))
		return notifier.SlackConfig{Enabled: false}
	}

	return notifier.SlackConfig{
		Enabled:    true,
		WebhookURL: webhookURL,
		Timeout:    30 * time.Second,
	}
}

// startCronWorker starts the cron scheduler and runs the crawl job periodically.
func startCronWorker(logger *slog.Logger, svc fetchUC.Service, cfg *workerPkg.WorkerConfig, metrics *workerPkg.WorkerMetrics, healthServer *workerPkg.HealthServer) {
	// Load timezone
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		logger.Error("invalid timezone, using UTC", slog.String("timezone", cfg.Timezone), slog.Any("error", err))
		loc = time.UTC
	}
	c := cron.New(cron.WithLocation(loc))

	_, err = c.AddFunc(cfg.CronSchedule, func() {
		runCrawlJob(logger, svc, cfg, metrics)
	})
	if err != nil {
		logger.Error("failed to add cron job", slog.Any("error", err))
		os.Exit(1)
	}
	c.Start()

	// Mark as ready after cron is set up
	healthServer.SetReady(true)
	logger.Info("worker marked as ready")

	logger.Info("worker started", slog.String("schedule", cfg.CronSchedule), slog.String("timezone", cfg.Timezone))
	select {}
}

// runCrawlJob executes a single crawl job with timeout and error handling.
func runCrawlJob(logger *slog.Logger, svc fetchUC.Service, cfg *workerPkg.WorkerConfig, metrics *workerPkg.WorkerMetrics) {
	startTime := time.Now()
	metrics.RecordJobRun("started")
	logger.Info("crawl started")

	// クロール処理のタイムアウト（設定から取得）
	ctx, cancel := context.WithTimeout(context.Background(), cfg.CrawlTimeout)
	defer cancel()

	stats, err := svc.CrawlAllSources(ctx)
	if err != nil {
		// 機密情報をマスクしてログ出力
		logger.Error("crawl failed", slog.Any("error", hhttp.SanitizeError(err)))
		metrics.RecordJobRun("failure")
		metrics.RecordJobDuration(time.Since(startTime).Seconds())
		return
	}

	// Record metrics
	metrics.RecordJobRun("success")
	metrics.RecordJobDuration(time.Since(startTime).Seconds())
	metrics.RecordFeedsProcessed(stats.Sources)
	metrics.RecordLastSuccess()

	logger.Info("crawl completed",
		slog.Int("sources", stats.Sources),
		slog.Int64("feed_items", stats.FeedItems),
		slog.Int64("inserted", stats.Inserted),
		slog.Int64("duplicated", stats.Duplicated),
		slog.Int64("summarize_errors", stats.SummarizeError),
		slog.Duration("duration", stats.Duration),
	)
}

