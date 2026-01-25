package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// AIConfig holds configuration for the AI integration.
type AIConfig struct {
	// GRPCAddress is the catchup-ai gRPC server address.
	// Format: "host:port" (e.g., "localhost:50051")
	// Default: "localhost:50051"
	GRPCAddress string

	// Enabled controls whether AI features are active.
	// When false, embedding hook is skipped and CLI commands return error.
	// Default: true
	Enabled bool

	// ConnectionTimeout is the timeout for establishing gRPC connection.
	// Default: 10 seconds
	ConnectionTimeout time.Duration

	// Timeouts configures per-method timeouts (extensible).
	Timeouts TimeoutConfig

	// Search configures search parameters (extensible).
	Search SearchConfig

	// CircuitBreaker for AI service calls.
	CircuitBreaker CircuitBreakerConfig

	// Observability configures logging and tracing.
	Observability ObservabilityConfig
}

// TimeoutConfig holds per-method timeout settings.
// All values are configurable via environment variables.
type TimeoutConfig struct {
	// EmbedArticle timeout. Default: 30s
	EmbedArticle time.Duration
	// SearchSimilar timeout. Default: 30s
	SearchSimilar time.Duration
	// QueryArticles timeout. Default: 60s
	QueryArticles time.Duration
	// GenerateSummary timeout. Default: 120s
	GenerateSummary time.Duration
}

// SearchConfig holds search parameter defaults and limits.
type SearchConfig struct {
	// DefaultLimit for search results. Default: 10
	DefaultLimit int32
	// MaxLimit for search results. Default: 50
	MaxLimit int32
	// DefaultMinSimilarity threshold. Default: 0.7
	DefaultMinSimilarity float32
	// DefaultMaxContext for RAG. Default: 5
	DefaultMaxContext int32
	// MaxContext for RAG. Default: 20
	MaxContext int32
}

// ObservabilityConfig holds logging and tracing settings.
type ObservabilityConfig struct {
	// EnableTracing enables OpenTelemetry distributed tracing.
	EnableTracing bool
	// TracingEndpoint for OTLP exporter. Default: "localhost:4317"
	TracingEndpoint string
	// LogLevel for AI operations. Default: "info"
	LogLevel string
	// EnableMetrics enables Prometheus metrics.
	EnableMetrics bool
}

// CircuitBreakerConfig for AI service resilience.
type CircuitBreakerConfig struct {
	// MaxRequests in half-open state.
	MaxRequests uint32

	// Interval for clearing failure counts.
	Interval time.Duration

	// Timeout before transitioning from open to half-open.
	Timeout time.Duration

	// FailureThreshold ratio to trip circuit (0.0 to 1.0).
	FailureThreshold float64

	// MinRequests before calculating failure ratio.
	MinRequests uint32
}

// LoadAIConfig loads AI configuration from environment variables.
// Returns a config with defaults if environment variables are not set.
func LoadAIConfig() (*AIConfig, error) {
	config := &AIConfig{
		GRPCAddress:       getEnvOrDefault("AI_GRPC_ADDRESS", "localhost:50051"),
		Enabled:           getEnvBool("AI_ENABLED", true),
		ConnectionTimeout: getEnvDuration("AI_CONNECTION_TIMEOUT", 10*time.Second),
		Timeouts: TimeoutConfig{
			EmbedArticle:    getEnvDuration("AI_TIMEOUT_EMBED", 30*time.Second),
			SearchSimilar:   getEnvDuration("AI_TIMEOUT_SEARCH", 30*time.Second),
			QueryArticles:   getEnvDuration("AI_TIMEOUT_QUERY", 60*time.Second),
			GenerateSummary: getEnvDuration("AI_TIMEOUT_SUMMARY", 120*time.Second),
		},
		Search: SearchConfig{
			DefaultLimit:         int32(getEnvInt("AI_SEARCH_DEFAULT_LIMIT", 10)),
			MaxLimit:             int32(getEnvInt("AI_SEARCH_MAX_LIMIT", 50)),
			DefaultMinSimilarity: float32(getEnvFloat("AI_SEARCH_DEFAULT_MIN_SIMILARITY", 0.7)),
			DefaultMaxContext:    int32(getEnvInt("AI_SEARCH_DEFAULT_MAX_CONTEXT", 5)),
			MaxContext:           int32(getEnvInt("AI_SEARCH_MAX_CONTEXT", 20)),
		},
		CircuitBreaker: CircuitBreakerConfig{
			MaxRequests:      uint32(getEnvInt("AI_CB_MAX_REQUESTS", 3)),
			Interval:         getEnvDuration("AI_CB_INTERVAL", 10*time.Second),
			Timeout:          getEnvDuration("AI_CB_TIMEOUT", 30*time.Second),
			FailureThreshold: 0.6,
			MinRequests:      5,
		},
		Observability: ObservabilityConfig{
			EnableTracing:   getEnvBool("AI_TRACING_ENABLED", false),
			TracingEndpoint: getEnvOrDefault("AI_TRACING_ENDPOINT", "localhost:4317"),
			LogLevel:        getEnvOrDefault("AI_LOG_LEVEL", "info"),
			EnableMetrics:   getEnvBool("AI_METRICS_ENABLED", true),
		},
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid AI configuration: %w", err)
	}

	return config, nil
}

// Validate checks configuration correctness.
func (c *AIConfig) Validate() error {
	if c.GRPCAddress == "" {
		return fmt.Errorf("AI_GRPC_ADDRESS cannot be empty")
	}

	if c.ConnectionTimeout <= 0 {
		return fmt.Errorf("AI_CONNECTION_TIMEOUT must be positive")
	}

	if c.Timeouts.EmbedArticle <= 0 {
		return fmt.Errorf("AI_TIMEOUT_EMBED must be positive")
	}

	if c.Timeouts.SearchSimilar <= 0 {
		return fmt.Errorf("AI_TIMEOUT_SEARCH must be positive")
	}

	if c.Timeouts.QueryArticles <= 0 {
		return fmt.Errorf("AI_TIMEOUT_QUERY must be positive")
	}

	if c.Timeouts.GenerateSummary <= 0 {
		return fmt.Errorf("AI_TIMEOUT_SUMMARY must be positive")
	}

	if c.Search.DefaultLimit <= 0 || c.Search.DefaultLimit > c.Search.MaxLimit {
		return fmt.Errorf("AI_SEARCH_DEFAULT_LIMIT must be between 1 and MAX_LIMIT")
	}

	if c.Search.MaxLimit <= 0 || c.Search.MaxLimit > 100 {
		return fmt.Errorf("AI_SEARCH_MAX_LIMIT must be between 1 and 100")
	}

	if c.Search.DefaultMinSimilarity < 0 || c.Search.DefaultMinSimilarity > 1 {
		return fmt.Errorf("AI_SEARCH_DEFAULT_MIN_SIMILARITY must be between 0.0 and 1.0")
	}

	if c.Search.DefaultMaxContext <= 0 || c.Search.DefaultMaxContext > c.Search.MaxContext {
		return fmt.Errorf("AI_SEARCH_DEFAULT_MAX_CONTEXT must be between 1 and MAX_CONTEXT")
	}

	if c.Search.MaxContext <= 0 || c.Search.MaxContext > 50 {
		return fmt.Errorf("AI_SEARCH_MAX_CONTEXT must be between 1 and 50")
	}

	if c.CircuitBreaker.MaxRequests == 0 {
		return fmt.Errorf("AI_CB_MAX_REQUESTS must be positive")
	}

	if c.CircuitBreaker.Interval <= 0 {
		return fmt.Errorf("AI_CB_INTERVAL must be positive")
	}

	if c.CircuitBreaker.Timeout <= 0 {
		return fmt.Errorf("AI_CB_TIMEOUT must be positive")
	}

	return nil
}

// getEnvOrDefault returns environment variable value or default.
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvBool parses boolean environment variable with default.
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getEnvInt parses integer environment variable with default.
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getEnvFloat parses float environment variable with default.
func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}

// getEnvDuration parses duration environment variable with default.
// Supports formats like "30s", "1m", "2h".
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		parsed, err := time.ParseDuration(value)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}
