package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"catchup-feed/internal/usecase/notify"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// HealthResponse represents a simple health check response.
type HealthResponse struct {
	Status string `json:"status"`
}

// ChannelHealthResponse represents the health status of all notification channels.
type ChannelHealthResponse struct {
	Healthy  bool            `json:"healthy"`
	Channels []ChannelStatus `json:"channels"`
}

// ChannelStatus represents the status of a single notification channel.
type ChannelStatus struct {
	Name               string     `json:"name"`
	Enabled            bool       `json:"enabled"`
	CircuitBreakerOpen bool       `json:"circuit_breaker_open"`
	DisabledUntil      *time.Time `json:"disabled_until,omitempty"`
}

// startMetricsServer starts the Prometheus metrics HTTP server on the specified port.
// It runs in a separate goroutine and supports graceful shutdown via context.
//
// Parameters:
//   - ctx: Context for graceful shutdown signal
//   - logger: Structured logger for server events
//   - notifyService: Notification service for channel health checks (can be nil)
//
// Returns:
//   - *http.Server: Server instance for external shutdown control (if needed)
//
// The server exposes the following endpoints:
//   - GET /metrics - Prometheus metrics endpoint (scraped by Prometheus server)
//   - GET /health - Simple liveness probe (always returns 200 OK)
//   - GET /health/channels - Detailed channel health status with circuit breaker state
//
// Environment variables:
//   - METRICS_PORT: Port to listen on (default: 9090)
//
// Graceful shutdown:
//   - When ctx is canceled, the server gracefully shuts down within 5 seconds
//   - All in-flight requests are allowed to complete
//   - Shutdown errors are logged but do not block process termination
func startMetricsServer(ctx context.Context, logger *slog.Logger, notifyService notify.Service) *http.Server {
	port := getMetricsPort()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	// Health check endpoints
	mux.HandleFunc("/health", healthHandler)
	if notifyService != nil {
		mux.HandleFunc("/health/channels", channelHealthHandler(notifyService))
	} else {
		mux.HandleFunc("/health/channels", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "notification service not initialized",
			})
		})
	}

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in background goroutine
	go func() {
		logger.Info("metrics server starting", slog.Int("port", port))
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server error", slog.Any("error", err))
		}
	}()

	// Handle graceful shutdown
	go func() {
		<-ctx.Done()
		logger.Info("metrics server shutdown initiated")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("metrics server shutdown error", slog.Any("error", err))
		} else {
			logger.Info("metrics server stopped")
		}
	}()

	return server
}

// getMetricsPort retrieves the metrics server port from environment variable.
// Defaults to 9090 if not set or invalid.
func getMetricsPort() int {
	portStr := os.Getenv("METRICS_PORT")
	if portStr == "" {
		return 9090 // default
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 9090 // default on invalid value
	}

	return port
}

// healthHandler handles GET /health requests (liveness probe).
// Always returns 200 OK with {"status": "healthy"}.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(HealthResponse{Status: "healthy"})
}

// channelHealthHandler creates a handler for GET /health/channels (readiness probe).
// Returns 200 OK if all channels are healthy (circuit breakers closed).
// Returns 503 Service Unavailable if any circuit breaker is open.
func channelHealthHandler(notifyService notify.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get channel health status from notify service
		healthStatuses := notifyService.GetChannelHealth()

		// Convert to API response format
		channels := make([]ChannelStatus, 0, len(healthStatuses))
		healthy := true

		for _, status := range healthStatuses {
			channels = append(channels, ChannelStatus{
				Name:               status.Name,
				Enabled:            status.Enabled,
				CircuitBreakerOpen: status.CircuitBreakerOpen,
				DisabledUntil:      status.DisabledUntil,
			})

			// If any enabled channel has circuit breaker open, mark as unhealthy
			if status.Enabled && status.CircuitBreakerOpen {
				healthy = false
			}
		}

		// Determine HTTP status code
		statusCode := http.StatusOK
		if !healthy {
			statusCode = http.StatusServiceUnavailable
		}

		// Send response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(ChannelHealthResponse{
			Healthy:  healthy,
			Channels: channels,
		})
	}
}
