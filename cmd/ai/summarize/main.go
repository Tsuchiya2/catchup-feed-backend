// Package main provides a CLI command for generating article summaries.
// Usage: catchup-ai-summarize [--period week|month] [--highlights N] [--output json]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"catchup-feed/internal/config"
	"catchup-feed/internal/infra/grpc"
	"catchup-feed/internal/usecase/ai"
)

// SummaryOutput represents the JSON output format for summary results.
type SummaryOutput struct {
	Period       string            `json:"period"`
	StartDate    string            `json:"start_date"`
	EndDate      string            `json:"end_date"`
	ArticleCount int32             `json:"article_count"`
	Summary      string            `json:"summary"`
	Highlights   []HighlightOutput `json:"highlights"`
}

// HighlightOutput represents a single highlight in the summary.
type HighlightOutput struct {
	Topic        string `json:"topic"`
	Description  string `json:"description"`
	ArticleCount int32  `json:"article_count"`
}

func main() {
	// Parse command-line arguments
	var (
		period       string
		maxHighlights int
		outputFormat string
	)

	flag.StringVar(&period, "period", "week", "Time period to summarize: week or month")
	flag.IntVar(&maxHighlights, "highlights", 5, "Maximum number of highlights to include")
	flag.StringVar(&outputFormat, "output", "text", "Output format: text or json")
	flag.Parse()

	// Validate period
	var summaryPeriod ai.SummaryPeriod
	switch period {
	case "week":
		summaryPeriod = ai.SummaryPeriodWeek
	case "month":
		summaryPeriod = ai.SummaryPeriodMonth
	default:
		fmt.Fprintf(os.Stderr, "Error: Invalid period '%s' (must be 'week' or 'month')\n", period)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: catchup-ai-summarize [--period week|month] [--highlights N] [--output json]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  catchup-ai-summarize")
		fmt.Fprintln(os.Stderr, "  catchup-ai-summarize --period month")
		fmt.Fprintln(os.Stderr, "  catchup-ai-summarize --period week --highlights 10")
		fmt.Fprintln(os.Stderr, "  catchup-ai-summarize --output json")
		os.Exit(1)
	}

	// Initialize logger
	logger := initLogger()

	// Load AI configuration
	aiConfig, err := config.LoadAIConfig()
	if err != nil {
		logger.Error("failed to load AI configuration", slog.Any("error", err))
		fmt.Fprintf(os.Stderr, "Error: Failed to load AI configuration: %v\n", err)
		os.Exit(1)
	}

	// Validate configuration
	if err := aiConfig.Validate(); err != nil {
		logger.Error("invalid AI configuration", slog.Any("error", err))
		fmt.Fprintf(os.Stderr, "Error: Invalid AI configuration: %v\n", err)
		os.Exit(1)
	}

	// Create AI provider
	var provider ai.AIProvider
	if aiConfig.Enabled {
		provider, err = grpc.NewGRPCAIProvider(aiConfig)
		if err != nil {
			logger.Error("failed to create AI provider", slog.Any("error", err))
			fmt.Fprintf(os.Stderr, "Error: Failed to connect to AI service: %v\n", err)
			os.Exit(1)
		}
		defer func() {
			if closeErr := provider.Close(); closeErr != nil {
				logger.Error("failed to close AI provider", slog.Any("error", closeErr))
			}
		}()
	} else {
		provider = grpc.NewNoopAIProvider()
	}

	// Create AI service
	aiService := ai.NewService(provider, aiConfig.Enabled)

	// Execute summarization with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	logger.Info("Generating summary",
		slog.String("period", period),
		slog.Int("max_highlights", maxHighlights))

	resp, err := aiService.Summarize(ctx, summaryPeriod, int32(maxHighlights))
	if err != nil {
		logger.Error("summarize failed", slog.Any("error", err))
		fmt.Fprintf(os.Stderr, "Error: Summarize failed: %v\n", err)
		os.Exit(1)
	}

	// Output results
	if outputFormat == "json" {
		outputJSON(period, resp)
	} else {
		outputText(period, resp)
	}
}

// outputText prints summary results in human-readable format.
func outputText(period string, resp *ai.SummaryResponse) {
	fmt.Printf("Summary for %s (%s to %s)\n", period, resp.StartDate, resp.EndDate)
	fmt.Printf("Articles analyzed: %d\n\n", resp.ArticleCount)

	fmt.Printf("Summary:\n%s\n\n", resp.Summary)

	if len(resp.Highlights) > 0 {
		fmt.Printf("Key Highlights:\n")
		for i, highlight := range resp.Highlights {
			fmt.Printf("%d. %s\n", i+1, highlight.Topic)
			fmt.Printf("   %s\n", highlight.Description)
			fmt.Printf("   (%d articles)\n", highlight.ArticleCount)
		}
	}
}

// outputJSON prints summary results in JSON format.
func outputJSON(period string, resp *ai.SummaryResponse) {
	highlights := make([]HighlightOutput, len(resp.Highlights))
	for i, highlight := range resp.Highlights {
		highlights[i] = HighlightOutput{
			Topic:        highlight.Topic,
			Description:  highlight.Description,
			ArticleCount: highlight.ArticleCount,
		}
	}

	output := SummaryOutput{
		Period:       period,
		StartDate:    resp.StartDate,
		EndDate:      resp.EndDate,
		ArticleCount: resp.ArticleCount,
		Summary:      resp.Summary,
		Highlights:   highlights,
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fmt.Fprintf(os.Stderr, "Error: Failed to encode JSON: %v\n", err)
		os.Exit(1)
	}
}

// initLogger initializes and returns a structured logger.
func initLogger() *slog.Logger {
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)
	return logger
}
