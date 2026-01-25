package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"catchup-feed/internal/usecase/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAIProvider implements ai.AIProvider for testing.
type mockAIProvider struct {
	healthFn func(ctx context.Context) (*ai.HealthStatus, error)
}

func (m *mockAIProvider) EmbedArticle(ctx context.Context, req ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return &ai.EmbedResponse{Success: true}, nil
}

func (m *mockAIProvider) SearchSimilar(ctx context.Context, req ai.SearchRequest) (*ai.SearchResponse, error) {
	return &ai.SearchResponse{}, nil
}

func (m *mockAIProvider) QueryArticles(ctx context.Context, req ai.QueryRequest) (*ai.QueryResponse, error) {
	return &ai.QueryResponse{}, nil
}

func (m *mockAIProvider) GenerateSummary(ctx context.Context, req ai.SummaryRequest) (*ai.SummaryResponse, error) {
	return &ai.SummaryResponse{}, nil
}

func (m *mockAIProvider) Health(ctx context.Context) (*ai.HealthStatus, error) {
	if m.healthFn != nil {
		return m.healthFn(ctx)
	}
	return &ai.HealthStatus{Healthy: true, Latency: 10 * time.Millisecond}, nil
}

func (m *mockAIProvider) Close() error {
	return nil
}

func TestNewAIHealthHandler(t *testing.T) {
	provider := &mockAIProvider{}
	handler := NewAIHealthHandler(provider)

	assert.NotNil(t, handler)
	assert.Equal(t, provider, handler.provider)
}

func TestAIHealthHandler_Health_Healthy(t *testing.T) {
	provider := &mockAIProvider{
		healthFn: func(ctx context.Context) (*ai.HealthStatus, error) {
			return &ai.HealthStatus{
				Healthy: true,
				Latency: 15 * time.Millisecond,
			}, nil
		},
	}

	handler := NewAIHealthHandler(provider)

	req := httptest.NewRequest(http.MethodGet, "/health/ai", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response AIHealthResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "healthy", response.Status)
	assert.Equal(t, "15ms", response.Latency)
}

func TestAIHealthHandler_Health_Unhealthy(t *testing.T) {
	provider := &mockAIProvider{
		healthFn: func(ctx context.Context) (*ai.HealthStatus, error) {
			return &ai.HealthStatus{
				Healthy:     false,
				Message:     "connection refused",
				CircuitOpen: true,
			}, nil
		},
	}

	handler := NewAIHealthHandler(provider)

	req := httptest.NewRequest(http.MethodGet, "/health/ai", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response AIHealthResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "unhealthy", response.Status)
	assert.Equal(t, "connection refused", response.Message)
	assert.True(t, response.CircuitOpen)
}

func TestAIHealthHandler_Health_Error(t *testing.T) {
	provider := &mockAIProvider{
		healthFn: func(ctx context.Context) (*ai.HealthStatus, error) {
			return &ai.HealthStatus{
				Healthy: false,
				Message: "health check failed",
			}, errors.New("connection error")
		},
	}

	handler := NewAIHealthHandler(provider)

	req := httptest.NewRequest(http.MethodGet, "/health/ai", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	var response AIHealthResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "unhealthy", response.Status)
}

func TestAIHealthHandler_Ready_Ready(t *testing.T) {
	provider := &mockAIProvider{
		healthFn: func(ctx context.Context) (*ai.HealthStatus, error) {
			return &ai.HealthStatus{
				Healthy:     true,
				CircuitOpen: false,
			}, nil
		},
	}

	handler := NewAIHealthHandler(provider)

	req := httptest.NewRequest(http.MethodGet, "/ready/ai", nil)
	w := httptest.NewRecorder()

	handler.Ready(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response AIHealthResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.NotNil(t, response.Ready)
	assert.True(t, *response.Ready)
}

func TestAIHealthHandler_Ready_NotReady_CircuitOpen(t *testing.T) {
	provider := &mockAIProvider{
		healthFn: func(ctx context.Context) (*ai.HealthStatus, error) {
			return &ai.HealthStatus{
				Healthy:     true,
				CircuitOpen: true,
			}, nil
		},
	}

	handler := NewAIHealthHandler(provider)

	req := httptest.NewRequest(http.MethodGet, "/ready/ai", nil)
	w := httptest.NewRecorder()

	handler.Ready(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var response AIHealthResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.NotNil(t, response.Ready)
	assert.False(t, *response.Ready)
	assert.Equal(t, "circuit breaker open", response.Message)
}

func TestAIHealthHandler_Ready_HealthError(t *testing.T) {
	// When health check fails but circuit is not explicitly open,
	// the ready endpoint should still report ready=true
	// because the circuit state is what determines readiness
	provider := &mockAIProvider{
		healthFn: func(ctx context.Context) (*ai.HealthStatus, error) {
			return &ai.HealthStatus{
				Healthy:     false,
				CircuitOpen: false,
			}, errors.New("connection error")
		},
	}

	handler := NewAIHealthHandler(provider)

	req := httptest.NewRequest(http.MethodGet, "/ready/ai", nil)
	w := httptest.NewRecorder()

	handler.Ready(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response AIHealthResponse
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)

	assert.NotNil(t, response.Ready)
	assert.True(t, *response.Ready)
}

func TestAIHealthHandler_Health_RequestContext(t *testing.T) {
	contextReceived := false
	provider := &mockAIProvider{
		healthFn: func(ctx context.Context) (*ai.HealthStatus, error) {
			contextReceived = true
			// Verify context has deadline (should be set by handler)
			_, hasDeadline := ctx.Deadline()
			assert.True(t, hasDeadline, "Context should have deadline")
			return &ai.HealthStatus{Healthy: true}, nil
		},
	}

	handler := NewAIHealthHandler(provider)

	req := httptest.NewRequest(http.MethodGet, "/health/ai", nil)
	w := httptest.NewRecorder()

	handler.Health(w, req)

	assert.True(t, contextReceived)
}

func TestAIHealthResponse_JSONSerialization(t *testing.T) {
	tests := []struct {
		name     string
		response AIHealthResponse
		expected map[string]any
	}{
		{
			name: "healthy response",
			response: AIHealthResponse{
				Status:  "healthy",
				Latency: "10ms",
			},
			expected: map[string]any{
				"status":  "healthy",
				"latency": "10ms",
			},
		},
		{
			name: "unhealthy response with circuit open",
			response: AIHealthResponse{
				Status:      "unhealthy",
				Message:     "connection refused",
				CircuitOpen: true,
			},
			expected: map[string]any{
				"status":       "unhealthy",
				"message":      "connection refused",
				"circuit_open": true,
			},
		},
		{
			name: "ready response",
			response: AIHealthResponse{
				Ready: boolPtr(true),
			},
			expected: map[string]any{
				"ready": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.response)
			require.NoError(t, err)

			var result map[string]any
			err = json.Unmarshal(data, &result)
			require.NoError(t, err)

			for key, expected := range tt.expected {
				assert.Equal(t, expected, result[key], "Field %s mismatch", key)
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}
