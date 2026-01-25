// Package main provides a CLI command for RAG-based Q&A.
// Usage: catchup-ai-ask "question" [--context N] [--output json]
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

// AskOutput represents the JSON output format for Q&A results.
type AskOutput struct {
	Question   string         `json:"question"`
	Answer     string         `json:"answer"`
	Confidence float32        `json:"confidence"`
	Sources    []SourceOutput `json:"sources"`
}

// SourceOutput represents a source article in the answer.
type SourceOutput struct {
	ArticleID int64   `json:"article_id"`
	Title     string  `json:"title"`
	URL       string  `json:"url"`
	Relevance float32 `json:"relevance"`
}

func main() {
	// Parse command-line arguments
	var (
		maxContext   int
		outputFormat string
	)

	flag.IntVar(&maxContext, "context", 5, "Maximum number of articles to use as context")
	flag.StringVar(&outputFormat, "output", "text", "Output format: text or json")
	flag.Parse()

	// Get question from positional argument
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Error: Question is required")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: catchup-ai-ask \"question\" [--context N] [--output json]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  catchup-ai-ask \"What are the benefits of Go?\"")
		fmt.Fprintln(os.Stderr, "  catchup-ai-ask \"How does machine learning work?\" --context 10")
		fmt.Fprintln(os.Stderr, "  catchup-ai-ask \"What is Kubernetes?\" --output json")
		os.Exit(1)
	}
	question := args[0]

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

	// Execute query with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	logger.Info("Asking question",
		slog.String("question", question),
		slog.Int("max_context", maxContext))

	resp, err := aiService.Ask(ctx, question, int32(maxContext))
	if err != nil {
		logger.Error("ask failed", slog.Any("error", err))
		fmt.Fprintf(os.Stderr, "Error: Ask failed: %v\n", err)
		os.Exit(1)
	}

	// Output results
	if outputFormat == "json" {
		outputJSON(question, resp)
	} else {
		outputText(question, resp)
	}
}

// outputText prints Q&A results in human-readable format.
func outputText(question string, resp *ai.QueryResponse) {
	fmt.Printf("Question: %s\n\n", question)
	fmt.Printf("Answer (Confidence: %.2f%%):\n", resp.Confidence*100)
	fmt.Printf("%s\n\n", resp.Answer)

	if len(resp.Sources) > 0 {
		fmt.Printf("Sources:\n")
		for i, source := range resp.Sources {
			fmt.Printf("%d. %s (Relevance: %.2f%%)\n", i+1, source.Title, source.Relevance*100)
			fmt.Printf("   URL: %s\n", source.URL)
		}
	}
}

// outputJSON prints Q&A results in JSON format.
func outputJSON(question string, resp *ai.QueryResponse) {
	sources := make([]SourceOutput, len(resp.Sources))
	for i, source := range resp.Sources {
		sources[i] = SourceOutput{
			ArticleID: source.ArticleID,
			Title:     source.Title,
			URL:       source.URL,
			Relevance: source.Relevance,
		}
	}

	output := AskOutput{
		Question:   question,
		Answer:     resp.Answer,
		Confidence: resp.Confidence,
		Sources:    sources,
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
