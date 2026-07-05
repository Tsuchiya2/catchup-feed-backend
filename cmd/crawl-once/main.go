// Package main provides a one-time crawl command for manual execution.
// This command fetches articles from all active RSS sources without waiting for cron.
package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/infra/db"
	"catchup-feed/internal/infra/fetcher"
	"catchup-feed/internal/infra/scraper"
	"catchup-feed/internal/infra/summarizer"
	fetchUC "catchup-feed/internal/usecase/fetch"
)

func main() {
	logger := initLogger()
	logger.Info("Starting one-time crawl...")

	database := db.Open()
	defer func() {
		if err := database.Close(); err != nil {
			logger.Error("failed to close database", slog.Any("error", err))
		}
	}()

	// Wait for migrations to be ready
	waitForMigrations(logger, database)

	// Setup fetch service
	svc := setupFetchService(logger, database)

	// Execute crawl with 30-minute timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	logger.Info("Crawling all sources...")
	stats, err := svc.CrawlAllSources(ctx)
	if err != nil {
		logger.Error("crawl failed", slog.Any("error", err))
		os.Exit(1)
	}

	logger.Info("Crawl completed successfully",
		slog.Int("sources", stats.Sources),
		slog.Int64("feed_items", stats.FeedItems),
		slog.Int64("inserted", stats.Inserted),
		slog.Int64("duplicated", stats.Duplicated),
		slog.Int64("summarize_errors", stats.SummarizeError),
		slog.Duration("duration", stats.Duration),
	)
}

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

func waitForMigrations(logger *slog.Logger, db *sql.DB) {
	const probe = "SELECT 1 FROM sources LIMIT 1"
	for i := range 10 {
		if _, err := db.Exec(probe); err == nil {
			return
		}
		logger.Info("waiting for migrations, retrying in 3s", slog.Int("attempt", i+1))
		time.Sleep(3 * time.Second)
	}
	logger.Error("migrations did not complete in time")
	os.Exit(1)
}

func setupFetchService(logger *slog.Logger, database *sql.DB) fetchUC.Service {
	srcRepo := pgRepo.NewSourceRepo(database)
	artRepo := pgRepo.NewArticleRepo(database)

	sum := createSummarizer(logger)
	httpClient := createHTTPClient()
	feedFetcher := scraper.NewRSSFetcher(httpClient)

	// Load content fetch configuration from environment
	contentFetchConfig, err := fetcher.LoadConfigFromEnv()
	if err != nil {
		logger.Warn("Content fetching disabled due to configuration error", slog.Any("error", err))
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
	}

	// Create fetch service configuration
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
// environment variables. Unlike cmd/worker, a one-shot crawl stays useful
// without summarization, so an empty chain degrades to NoOp with a warning.
func createSummarizer(logger *slog.Logger) fetchUC.Summarizer {
	chain, err := summarizer.NewChainFromEnv(logger)
	if err != nil {
		logger.Warn("no summarizer provider configured, using NoOp summarizer (no summarization)",
			slog.Any("error", err))
		return summarizer.NewNoOp()
	}
	return chain
}

func createHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}
}
