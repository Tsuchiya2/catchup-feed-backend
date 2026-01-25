package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"catchup-feed/internal/usecase/ai"
)

// AIHealthHandler provides health check endpoints for AI integration.
type AIHealthHandler struct {
	provider ai.AIProvider
}

// NewAIHealthHandler creates a new AI health check handler.
func NewAIHealthHandler(provider ai.AIProvider) *AIHealthHandler {
	return &AIHealthHandler{
		provider: provider,
	}
}

// AIHealthResponse represents the response structure for AI health endpoints.
type AIHealthResponse struct {
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	Latency     string `json:"latency,omitempty"`
	CircuitOpen bool   `json:"circuit_open,omitempty"`
	Ready       *bool  `json:"ready,omitempty"`
}

// Health returns basic health status of the AI service.
// GET /health/ai
// Returns 200 if healthy, 503 if unavailable.
func (h *AIHealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	status, err := h.provider.Health(ctx)

	w.Header().Set("Content-Type", "application/json")

	// Handle error or nil status
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		response := AIHealthResponse{
			Status:  "unhealthy",
			Message: err.Error(),
		}
		if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
			slog.Error("failed to encode AI health response", slog.Any("error", encErr))
		}
		return
	}

	// Handle unhealthy status
	if status == nil || !status.Healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		response := AIHealthResponse{
			Status: "unhealthy",
		}
		if status != nil {
			response.Message = status.Message
			response.CircuitOpen = status.CircuitOpen
		}
		if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
			slog.Error("failed to encode AI health response", slog.Any("error", encErr))
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	response := AIHealthResponse{
		Status:  "healthy",
		Latency: status.Latency.String(),
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.Error("failed to encode AI health response", slog.Any("error", err))
	}
}

// Ready returns readiness for traffic (checks circuit breaker state).
// GET /ready/ai
// Returns 200 if ready to serve traffic, 503 if circuit breaker is open.
// Note: Ready only checks circuit breaker state, not overall health.
// A service can be unhealthy but still ready if circuit is closed.
func (h *AIHealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	status, err := h.provider.Health(ctx)

	w.Header().Set("Content-Type", "application/json")

	// Handle nil status (error with no status returned)
	if status == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		ready := false
		msg := "health check failed"
		if err != nil {
			msg = err.Error()
		}
		response := AIHealthResponse{
			Ready:   &ready,
			Message: msg,
		}
		if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
			slog.Error("failed to encode AI ready response", slog.Any("error", encErr))
		}
		return
	}

	// Check circuit breaker state (determines readiness, not health)
	if status.CircuitOpen {
		w.WriteHeader(http.StatusServiceUnavailable)
		ready := false
		response := AIHealthResponse{
			Ready:   &ready,
			Message: "circuit breaker open",
		}
		if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
			slog.Error("failed to encode AI ready response", slog.Any("error", encErr))
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	ready := true
	response := AIHealthResponse{
		Ready: &ready,
	}
	if encErr := json.NewEncoder(w).Encode(response); encErr != nil {
		slog.Error("failed to encode AI ready response", slog.Any("error", encErr))
	}
}
