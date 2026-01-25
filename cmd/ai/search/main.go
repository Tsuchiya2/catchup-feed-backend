// Package main provides a CLI command for semantic article search.
// Usage: catchup-ai-search "query" [--limit N] [--min-similarity X] [--output json]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"os"
	"time"

	"catchup-feed/internal/config"
	"catchup-feed/internal/infra/grpc"
	"catchup-feed/internal/usecase/ai"
)

// SearchOutput represents the JSON output format for search results.
type SearchOutput struct {
	Query         string          `json:"query"`
	TotalSearched int64           `json:"total_searched"`
	ResultCount   int             `json:"result_count"`
	Articles      []ArticleOutput `json:"articles"`
}

// ArticleOutput represents a single article in the search results.
type ArticleOutput struct {
	ArticleID  int64   `json:"article_id"`
	Title      string  `json:"title"`
	URL        string  `json:"url"`
	Similarity float32 `json:"similarity"`
	Excerpt    string  `json:"excerpt"`
}

func main() {
	// Parse command-line arguments
	var (
		limit         int
		minSimilarity float64
		outputFormat  string
	)

	flag.IntVar(&limit, "limit", 10, "Maximum number of results to return")
	flag.Float64Var(&minSimilarity, "min-similarity", 0.7, "Minimum similarity score (0.0 to 1.0)")
	flag.StringVar(&outputFormat, "output", "text", "Output format: text or json")
	flag.Parse()

	// Get query from positional argument
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: Search query is required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: catchup-ai-search \"query\" [--limit N] [--min-similarity X] [--output json]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  catchup-ai-search \"Go concurrency patterns\"")
		fmt.Fprintln(os.Stderr, "  catchup-ai-search \"machine learning\" --limit 20 --min-similarity 0.8")
		fmt.Fprintln(os.Stderr, "  catchup-ai-search \"database\" --output json")
		os.Exit(1)
	}
	query := args[0]

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

	// Execute search with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Validate limit bounds for safe conversion and configured max
	const maxLimit = 50
	if limit < 0 || limit > math.MaxInt32 {
		limit = 10 // Use default
	}
	if limit > maxLimit {
		fmt.Fprintf(os.Stderr, "Warning: limit %d exceeds maximum %d, using %d\n", limit, maxLimit, maxLimit)
		limit = maxLimit
	}

	// Validate similarity bounds
	if minSimilarity < 0.0 || minSimilarity > 1.0 {
		fmt.Fprintf(os.Stderr, "Warning: min-similarity %.2f out of range [0.0, 1.0], using default 0.7\n", minSimilarity)
		minSimilarity = 0.7
	}

	logger.Info("Searching articles",
		slog.String("query", query),
		slog.Int("limit", limit),
		slog.Float64("min_similarity", minSimilarity))

	resp, err := aiService.Search(ctx, query, int32(limit), float32(minSimilarity)) // #nosec G115 - bounds checked above
	if err != nil {
		logger.Error("search failed", slog.Any("error", err))
		fmt.Fprintf(os.Stderr, "Error: Search failed: %v\n", err)
		os.Exit(1)
	}

	// Output results
	if outputFormat == "json" {
		outputJSON(query, resp)
	} else {
		outputText(query, resp)
	}
}

// outputText prints search results in human-readable format.
func outputText(query string, resp *ai.SearchResponse) {
	fmt.Printf("Search Results for: %q\n", query)
	fmt.Printf("Total Searched: %d articles\n", resp.TotalSearched)
	fmt.Printf("Results: %d\n\n", len(resp.Articles))

	if len(resp.Articles) == 0 {
		fmt.Println("No articles found matching your query.")
		return
	}

	for i, article := range resp.Articles {
		fmt.Printf("%d. %s\n", i+1, article.Title)
		fmt.Printf("   Similarity: %.2f%%\n", article.Similarity*100)
		fmt.Printf("   URL: %s\n", article.URL)
		if article.Excerpt != "" {
			fmt.Printf("   Excerpt: %s\n", article.Excerpt)
		}
		fmt.Println()
	}
}

// outputJSON prints search results in JSON format.
func outputJSON(query string, resp *ai.SearchResponse) {
	articles := make([]ArticleOutput, len(resp.Articles))
	for i, article := range resp.Articles {
		articles[i] = ArticleOutput{
			ArticleID:  article.ArticleID,
			Title:      article.Title,
			URL:        article.URL,
			Similarity: article.Similarity,
			Excerpt:    article.Excerpt,
		}
	}

	output := SearchOutput{
		Query:         query,
		TotalSearched: resp.TotalSearched,
		ResultCount:   len(resp.Articles),
		Articles:      articles,
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
