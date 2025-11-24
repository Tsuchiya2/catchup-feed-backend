package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// HealthServer provides HTTP endpoints for health checks.
// It implements two endpoints:
//   - /health: Liveness probe (always returns 200 OK)
//   - /health/ready: Readiness probe (returns 200 if ready, 503 if not)
//
// The server supports graceful shutdown via context cancellation.
//
// Example usage:
//
//	healthServer := NewHealthServer(":9091", logger)
//	go func() {
//	    if err := healthServer.Start(ctx); err != nil && err != http.ErrServerClosed {
//	        logger.Error("health server failed", slog.Any("error", err))
//	    }
//	}()
//	healthServer.SetReady(true)  // Mark as ready after initialization
type HealthServer struct {
	addr    string
	logger  *slog.Logger
	isReady *atomic.Bool
	server  *http.Server
}

// healthResponse is the JSON response format for health check endpoints.
type healthResponse struct {
	Status string `json:"status"`
}

// NewHealthServer creates a new health check server.
//
// Parameters:
//   - addr: Server listen address (e.g., ":9091", "localhost:9091")
//   - logger: Structured logger for logging server events
//
// Returns:
//   - *HealthServer: Initialized health server (not started yet)
//
// Example:
//
//	server := NewHealthServer(":9091", logger)
//	// Call Start() to begin serving requests
func NewHealthServer(addr string, logger *slog.Logger) *HealthServer {
	isReady := &atomic.Bool{}
	isReady.Store(false) // Start as not ready

	return &HealthServer{
		addr:    addr,
		logger:  logger,
		isReady: isReady,
	}
}

// Start starts the health check HTTP server.
// This is a blocking call that runs until the context is cancelled or an error occurs.
// It supports graceful shutdown with a 5-second timeout.
//
// Endpoints:
//   - GET /health: Liveness probe (always 200 OK)
//   - GET /health/ready: Readiness probe (200 if ready, 503 if not)
//
// Parameters:
//   - ctx: Context for cancellation and shutdown
//
// Returns:
//   - error: http.ErrServerClosed on graceful shutdown, other errors on failure
//
// Example:
//
//	healthServer := NewHealthServer(":9091", logger)
//	go func() {
//	    if err := healthServer.Start(ctx); err != nil && err != http.ErrServerClosed {
//	        logger.Error("health server failed", slog.Any("error", err))
//	    }
//	}()
func (h *HealthServer) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.handleLiveness)
	mux.HandleFunc("/health/ready", h.handleReadiness)

	h.server = &http.Server{
		Addr:         h.addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		h.logger.Info("health server starting", slog.String("addr", h.addr))
		if err := h.server.ListenAndServe(); err != nil {
			errChan <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		// Graceful shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		h.logger.Info("health server shutting down")
		if err := h.server.Shutdown(shutdownCtx); err != nil {
			h.logger.Error("health server shutdown failed", slog.Any("error", err))
			return err
		}
		h.logger.Info("health server stopped")
		return http.ErrServerClosed

	case err := <-errChan:
		if err == http.ErrServerClosed {
			return err
		}
		h.logger.Error("health server failed", slog.Any("error", err))
		return err
	}
}

// SetReady sets the readiness state of the server.
// This affects the response of the /health/ready endpoint.
//
// Parameters:
//   - ready: true to mark as ready, false to mark as not ready
//
// Example:
//
//	// After initialization is complete
//	healthServer.SetReady(true)
//
//	// Before shutdown
//	healthServer.SetReady(false)
func (h *HealthServer) SetReady(ready bool) {
	h.isReady.Store(ready)
	h.logger.Info("health server readiness changed", slog.Bool("ready", ready))
}

// handleLiveness handles the /health endpoint (liveness probe).
// Always returns 200 OK with {"status":"ok"}.
//
// This endpoint is used by Kubernetes liveness probes to determine if the
// container should be restarted. It always returns success unless the server
// is completely dead (in which case it won't respond at all).
func (h *HealthServer) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(healthResponse{Status: "ok"}); err != nil {
		h.logger.Error("failed to encode liveness response", slog.Any("error", err))
	}
}

// handleReadiness handles the /health/ready endpoint (readiness probe).
// Returns 200 OK if ready, 503 Service Unavailable if not ready.
//
// This endpoint is used by Kubernetes readiness probes to determine if the
// container should receive traffic. It returns success only when the worker
// is fully initialized and ready to process jobs.
func (h *HealthServer) handleReadiness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if h.isReady.Load() {
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(healthResponse{Status: "ok"}); err != nil {
			h.logger.Error("failed to encode readiness response", slog.Any("error", err))
		}
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(healthResponse{Status: "not ready"}); err != nil {
			h.logger.Error("failed to encode not ready response", slog.Any("error", err))
		}
	}
}
