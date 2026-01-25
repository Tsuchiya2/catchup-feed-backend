package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAIConfig_Defaults(t *testing.T) {
	// Clear all AI-related environment variables
	clearAIEnvVars(t)

	config, err := LoadAIConfig()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify defaults
	assert.Equal(t, "localhost:50051", config.GRPCAddress)
	assert.True(t, config.Enabled)
	assert.Equal(t, 10*time.Second, config.ConnectionTimeout)

	// Timeouts
	assert.Equal(t, 30*time.Second, config.Timeouts.EmbedArticle)
	assert.Equal(t, 30*time.Second, config.Timeouts.SearchSimilar)
	assert.Equal(t, 60*time.Second, config.Timeouts.QueryArticles)
	assert.Equal(t, 120*time.Second, config.Timeouts.GenerateSummary)

	// Search
	assert.Equal(t, int32(10), config.Search.DefaultLimit)
	assert.Equal(t, int32(50), config.Search.MaxLimit)
	assert.Equal(t, float32(0.7), config.Search.DefaultMinSimilarity)
	assert.Equal(t, int32(5), config.Search.DefaultMaxContext)
	assert.Equal(t, int32(20), config.Search.MaxContext)

	// Circuit Breaker
	assert.Equal(t, uint32(3), config.CircuitBreaker.MaxRequests)
	assert.Equal(t, 10*time.Second, config.CircuitBreaker.Interval)
	assert.Equal(t, 30*time.Second, config.CircuitBreaker.Timeout)
	assert.Equal(t, 0.6, config.CircuitBreaker.FailureThreshold)
	assert.Equal(t, uint32(5), config.CircuitBreaker.MinRequests)

	// Observability
	assert.False(t, config.Observability.EnableTracing)
	assert.Equal(t, "localhost:4317", config.Observability.TracingEndpoint)
	assert.Equal(t, "info", config.Observability.LogLevel)
	assert.True(t, config.Observability.EnableMetrics)
}

func TestLoadAIConfig_CustomValues(t *testing.T) {
	clearAIEnvVars(t)

	// Set custom environment variables
	setEnv(t, "AI_GRPC_ADDRESS", "ai-service:9090")
	setEnv(t, "AI_ENABLED", "false")
	setEnv(t, "AI_CONNECTION_TIMEOUT", "20s")
	setEnv(t, "AI_TIMEOUT_EMBED", "45s")
	setEnv(t, "AI_TIMEOUT_SEARCH", "35s")
	setEnv(t, "AI_TIMEOUT_QUERY", "90s")
	setEnv(t, "AI_TIMEOUT_SUMMARY", "180s")
	setEnv(t, "AI_SEARCH_DEFAULT_LIMIT", "20")
	setEnv(t, "AI_SEARCH_MAX_LIMIT", "100")
	setEnv(t, "AI_SEARCH_DEFAULT_MIN_SIMILARITY", "0.7")
	setEnv(t, "AI_SEARCH_DEFAULT_MAX_CONTEXT", "10")
	setEnv(t, "AI_SEARCH_MAX_CONTEXT", "30")
	setEnv(t, "AI_CB_MAX_REQUESTS", "5")
	setEnv(t, "AI_CB_INTERVAL", "20s")
	setEnv(t, "AI_CB_TIMEOUT", "60s")
	setEnv(t, "AI_TRACING_ENABLED", "true")
	setEnv(t, "AI_TRACING_ENDPOINT", "jaeger:4317")
	setEnv(t, "AI_LOG_LEVEL", "debug")
	setEnv(t, "AI_METRICS_ENABLED", "false")

	config, err := LoadAIConfig()
	require.NoError(t, err)

	assert.Equal(t, "ai-service:9090", config.GRPCAddress)
	assert.False(t, config.Enabled)
	assert.Equal(t, 20*time.Second, config.ConnectionTimeout)
	assert.Equal(t, 45*time.Second, config.Timeouts.EmbedArticle)
	assert.Equal(t, 35*time.Second, config.Timeouts.SearchSimilar)
	assert.Equal(t, 90*time.Second, config.Timeouts.QueryArticles)
	assert.Equal(t, 180*time.Second, config.Timeouts.GenerateSummary)
	assert.Equal(t, int32(20), config.Search.DefaultLimit)
	assert.Equal(t, int32(100), config.Search.MaxLimit)
	assert.Equal(t, float32(0.7), config.Search.DefaultMinSimilarity)
	assert.Equal(t, int32(10), config.Search.DefaultMaxContext)
	assert.Equal(t, int32(30), config.Search.MaxContext)
	assert.Equal(t, uint32(5), config.CircuitBreaker.MaxRequests)
	assert.Equal(t, 20*time.Second, config.CircuitBreaker.Interval)
	assert.Equal(t, 60*time.Second, config.CircuitBreaker.Timeout)
	assert.True(t, config.Observability.EnableTracing)
	assert.Equal(t, "jaeger:4317", config.Observability.TracingEndpoint)
	assert.Equal(t, "debug", config.Observability.LogLevel)
	assert.False(t, config.Observability.EnableMetrics)
}

func TestAIConfig_Validate_EmptyAddress(t *testing.T) {
	config := &AIConfig{
		GRPCAddress:       "",
		ConnectionTimeout: 10 * time.Second,
		Timeouts: TimeoutConfig{
			EmbedArticle:    30 * time.Second,
			SearchSimilar:   30 * time.Second,
			QueryArticles:   60 * time.Second,
			GenerateSummary: 120 * time.Second,
		},
		Search: SearchConfig{
			DefaultLimit:         10,
			MaxLimit:             50,
			DefaultMinSimilarity: 0.7,
			DefaultMaxContext:    5,
			MaxContext:           20,
		},
		CircuitBreaker: CircuitBreakerConfig{
			MaxRequests: 3,
			Interval:    10 * time.Second,
			Timeout:     30 * time.Second,
		},
	}

	err := config.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AI_GRPC_ADDRESS cannot be empty")
}

func TestAIConfig_Validate_InvalidTimeout(t *testing.T) {
	tests := []struct {
		name        string
		modifyFn    func(*AIConfig)
		expectedErr string
	}{
		{
			name: "negative connection timeout",
			modifyFn: func(c *AIConfig) {
				c.ConnectionTimeout = -1 * time.Second
			},
			expectedErr: "AI_CONNECTION_TIMEOUT must be positive",
		},
		{
			name: "zero embed timeout",
			modifyFn: func(c *AIConfig) {
				c.Timeouts.EmbedArticle = 0
			},
			expectedErr: "AI_TIMEOUT_EMBED must be positive",
		},
		{
			name: "negative search timeout",
			modifyFn: func(c *AIConfig) {
				c.Timeouts.SearchSimilar = -5 * time.Second
			},
			expectedErr: "AI_TIMEOUT_SEARCH must be positive",
		},
		{
			name: "zero query timeout",
			modifyFn: func(c *AIConfig) {
				c.Timeouts.QueryArticles = 0
			},
			expectedErr: "AI_TIMEOUT_QUERY must be positive",
		},
		{
			name: "negative summary timeout",
			modifyFn: func(c *AIConfig) {
				c.Timeouts.GenerateSummary = -10 * time.Second
			},
			expectedErr: "AI_TIMEOUT_SUMMARY must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validAIConfig()
			tt.modifyFn(config)

			err := config.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestAIConfig_Validate_InvalidSearchParams(t *testing.T) {
	tests := []struct {
		name        string
		modifyFn    func(*AIConfig)
		expectedErr string
	}{
		{
			name: "default limit zero",
			modifyFn: func(c *AIConfig) {
				c.Search.DefaultLimit = 0
			},
			expectedErr: "AI_SEARCH_DEFAULT_LIMIT must be between 1 and MAX_LIMIT",
		},
		{
			name: "default limit exceeds max",
			modifyFn: func(c *AIConfig) {
				c.Search.DefaultLimit = 100
				c.Search.MaxLimit = 50
			},
			expectedErr: "AI_SEARCH_DEFAULT_LIMIT must be between 1 and MAX_LIMIT",
		},
		{
			name: "max limit exceeds 100",
			modifyFn: func(c *AIConfig) {
				c.Search.MaxLimit = 200
			},
			expectedErr: "AI_SEARCH_MAX_LIMIT must be between 1 and 100",
		},
		{
			name: "similarity below 0",
			modifyFn: func(c *AIConfig) {
				c.Search.DefaultMinSimilarity = -0.1
			},
			expectedErr: "AI_SEARCH_DEFAULT_MIN_SIMILARITY must be between 0.0 and 1.0",
		},
		{
			name: "similarity above 1",
			modifyFn: func(c *AIConfig) {
				c.Search.DefaultMinSimilarity = 1.5
			},
			expectedErr: "AI_SEARCH_DEFAULT_MIN_SIMILARITY must be between 0.0 and 1.0",
		},
		{
			name: "default context zero",
			modifyFn: func(c *AIConfig) {
				c.Search.DefaultMaxContext = 0
			},
			expectedErr: "AI_SEARCH_DEFAULT_MAX_CONTEXT must be between 1 and MAX_CONTEXT",
		},
		{
			name: "max context exceeds 50",
			modifyFn: func(c *AIConfig) {
				c.Search.MaxContext = 100
			},
			expectedErr: "AI_SEARCH_MAX_CONTEXT must be between 1 and 50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validAIConfig()
			tt.modifyFn(config)

			err := config.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestAIConfig_Validate_InvalidCircuitBreaker(t *testing.T) {
	tests := []struct {
		name        string
		modifyFn    func(*AIConfig)
		expectedErr string
	}{
		{
			name: "zero max requests",
			modifyFn: func(c *AIConfig) {
				c.CircuitBreaker.MaxRequests = 0
			},
			expectedErr: "AI_CB_MAX_REQUESTS must be positive",
		},
		{
			name: "zero interval",
			modifyFn: func(c *AIConfig) {
				c.CircuitBreaker.Interval = 0
			},
			expectedErr: "AI_CB_INTERVAL must be positive",
		},
		{
			name: "negative timeout",
			modifyFn: func(c *AIConfig) {
				c.CircuitBreaker.Timeout = -1 * time.Second
			},
			expectedErr: "AI_CB_TIMEOUT must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := validAIConfig()
			tt.modifyFn(config)

			err := config.Validate()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestGetEnvHelpers(t *testing.T) {
	t.Run("getEnvOrDefault with value", func(t *testing.T) {
		setEnv(t, "TEST_VAR", "custom-value")
		assert.Equal(t, "custom-value", getEnvOrDefault("TEST_VAR", "default"))
	})

	t.Run("getEnvOrDefault with default", func(t *testing.T) {
		if err := os.Unsetenv("TEST_VAR_MISSING"); err != nil {
			t.Fatalf("failed to unset env: %v", err)
		}
		assert.Equal(t, "default", getEnvOrDefault("TEST_VAR_MISSING", "default"))
	})

	t.Run("getEnvBool true", func(t *testing.T) {
		setEnv(t, "TEST_BOOL", "true")
		assert.True(t, getEnvBool("TEST_BOOL", false))
	})

	t.Run("getEnvBool false", func(t *testing.T) {
		setEnv(t, "TEST_BOOL", "false")
		assert.False(t, getEnvBool("TEST_BOOL", true))
	})

	t.Run("getEnvBool invalid defaults to default", func(t *testing.T) {
		setEnv(t, "TEST_BOOL", "invalid")
		assert.True(t, getEnvBool("TEST_BOOL", true))
	})

	t.Run("getEnvInt32 with value", func(t *testing.T) {
		setEnv(t, "TEST_INT", "42")
		assert.Equal(t, int32(42), getEnvInt32("TEST_INT", 10))
	})

	t.Run("getEnvInt32 invalid defaults to default", func(t *testing.T) {
		setEnv(t, "TEST_INT", "invalid")
		assert.Equal(t, int32(10), getEnvInt32("TEST_INT", 10))
	})

	t.Run("getEnvUint32 with value", func(t *testing.T) {
		setEnv(t, "TEST_UINT", "42")
		assert.Equal(t, uint32(42), getEnvUint32("TEST_UINT", 10))
	})

	t.Run("getEnvUint32 invalid defaults to default", func(t *testing.T) {
		setEnv(t, "TEST_UINT", "invalid")
		assert.Equal(t, uint32(10), getEnvUint32("TEST_UINT", 10))
	})

	t.Run("getEnvFloat with value", func(t *testing.T) {
		setEnv(t, "TEST_FLOAT", "3.14")
		assert.InDelta(t, 3.14, getEnvFloat("TEST_FLOAT", 1.0), 0.001)
	})

	t.Run("getEnvDuration with value", func(t *testing.T) {
		setEnv(t, "TEST_DURATION", "45s")
		assert.Equal(t, 45*time.Second, getEnvDuration("TEST_DURATION", 30*time.Second))
	})

	t.Run("getEnvDuration invalid defaults to default", func(t *testing.T) {
		setEnv(t, "TEST_DURATION", "invalid")
		assert.Equal(t, 30*time.Second, getEnvDuration("TEST_DURATION", 30*time.Second))
	})
}

// Helper functions

func clearAIEnvVars(t *testing.T) {
	t.Helper()
	envVars := []string{
		"AI_GRPC_ADDRESS",
		"AI_ENABLED",
		"AI_CONNECTION_TIMEOUT",
		"AI_TIMEOUT_EMBED",
		"AI_TIMEOUT_SEARCH",
		"AI_TIMEOUT_QUERY",
		"AI_TIMEOUT_SUMMARY",
		"AI_SEARCH_DEFAULT_LIMIT",
		"AI_SEARCH_MAX_LIMIT",
		"AI_SEARCH_DEFAULT_MIN_SIMILARITY",
		"AI_SEARCH_DEFAULT_MAX_CONTEXT",
		"AI_SEARCH_MAX_CONTEXT",
		"AI_CB_MAX_REQUESTS",
		"AI_CB_INTERVAL",
		"AI_CB_TIMEOUT",
		"AI_TRACING_ENABLED",
		"AI_TRACING_ENDPOINT",
		"AI_LOG_LEVEL",
		"AI_METRICS_ENABLED",
	}
	for _, key := range envVars {
		_ = os.Unsetenv(key) // Ignore error in cleanup
	}
}

func setEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Cleanup(func() {
		_ = os.Unsetenv(key) // Ignore error in cleanup
	})
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("failed to set env %s: %v", key, err)
	}
}

func validAIConfig() *AIConfig {
	return &AIConfig{
		GRPCAddress:       "localhost:50051",
		Enabled:           true,
		ConnectionTimeout: 10 * time.Second,
		Timeouts: TimeoutConfig{
			EmbedArticle:    30 * time.Second,
			SearchSimilar:   30 * time.Second,
			QueryArticles:   60 * time.Second,
			GenerateSummary: 120 * time.Second,
		},
		Search: SearchConfig{
			DefaultLimit:         10,
			MaxLimit:             50,
			DefaultMinSimilarity: 0.7,
			DefaultMaxContext:    5,
			MaxContext:           20,
		},
		CircuitBreaker: CircuitBreakerConfig{
			MaxRequests: 3,
			Interval:    10 * time.Second,
			Timeout:     30 * time.Second,
		},
		Observability: ObservabilityConfig{
			EnableTracing:   false,
			TracingEndpoint: "localhost:4317",
			LogLevel:        "info",
			EnableMetrics:   true,
		},
	}
}
