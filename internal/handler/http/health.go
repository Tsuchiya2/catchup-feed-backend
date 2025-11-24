// Package http provides HTTP handlers and middleware for the web application.
// It includes request handlers for articles and sources, health check endpoints,
// metrics collection, authentication, and various middleware components.
package http

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// HealthResponse represents the JSON response for health check endpoints.
type HealthResponse struct {
	Status    string                 `json:"status"`    // "healthy" or "unhealthy"
	Timestamp string                 `json:"timestamp"` // ISO 8601 format
	Checks    map[string]CheckStatus `json:"checks"`    // Status of each check item
	Version   string                 `json:"version"`   // Application version
}

// CheckStatus represents the status of a single health check.
type CheckStatus struct {
	Status  string                 `json:"status"`            // "healthy" or "unhealthy"
	Message string                 `json:"message,omitempty"` // Optional status message
	Details map[string]interface{} `json:"details,omitempty"` // Optional additional details
}

// HealthHandler handles health check endpoint requests.
// It performs database connectivity checks and returns detailed health status.
type HealthHandler struct {
	DB      *sql.DB
	Version string
}

// ServeHTTP performs health checks and returns the application health status.
// It checks database connectivity and connection pool statistics.
// Returns 200 OK if healthy, or 503 Service Unavailable if any check fails.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	checks := make(map[string]CheckStatus)
	allHealthy := true

	// データベース接続チェック
	if h.DB != nil {
		dbCheck := h.checkDatabase(ctx)
		checks["database"] = dbCheck
		if dbCheck.Status == "unhealthy" {
			allHealthy = false
		}
	} else {
		checks["database"] = CheckStatus{
			Status:  "unhealthy",
			Message: "not configured",
		}
		allHealthy = false
	}

	// 全体のステータス決定
	// "degraded" is a warning state, not a failure - system is still operational
	status := "healthy"
	statusCode := http.StatusOK
	if !allHealthy {
		status = "unhealthy"
		statusCode = http.StatusServiceUnavailable
	}

	// レスポンス作成
	response := HealthResponse{
		Status:    status,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Checks:    checks,
		Version:   h.Version,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("health: failed to encode response: %v", err)
	}
}

// checkDatabase checks database connectivity and returns connection pool statistics.
func (h *HealthHandler) checkDatabase(ctx context.Context) CheckStatus {
	// Ping database
	if err := h.DB.PingContext(ctx); err != nil {
		return CheckStatus{
			Status:  "unhealthy",
			Message: err.Error(),
		}
	}

	// Get connection pool statistics
	stats := h.DB.Stats()
	details := map[string]interface{}{
		"max_open_connections": stats.MaxOpenConnections,
		"open_connections":     stats.OpenConnections,
		"in_use":               stats.InUse,
		"idle":                 stats.Idle,
		"wait_count":           stats.WaitCount,
		"wait_duration_ms":     stats.WaitDuration.Milliseconds(),
		"max_idle_closed":      stats.MaxIdleClosed,
		"max_idle_time_closed": stats.MaxIdleTimeClosed,
		"max_lifetime_closed":  stats.MaxLifetimeClosed,
	}

	// Check connection pool utilization
	// Guard against zero division when MaxOpenConnections is 0 (unlimited/unconfigured)
	if stats.MaxOpenConnections == 0 {
		return CheckStatus{
			Status:  "degraded",
			Message: "connection pool max connections not configured",
			Details: details,
		}
	}

	// Calculate utilization percentage
	utilizationPercent := float64(stats.InUse) / float64(stats.MaxOpenConnections) * 100
	details["utilization_percent"] = utilizationPercent

	// Check if connection pool is near capacity
	if utilizationPercent >= 80.0 {
		return CheckStatus{
			Status:  "degraded",
			Message: "connection pool utilization above 80%",
			Details: details,
		}
	}

	return CheckStatus{
		Status:  "healthy",
		Details: details,
	}
}

// ReadyHandler handles Kubernetes readiness probe requests.
// It checks if the database connection is established and ready to accept traffic.
type ReadyHandler struct {
	DB *sql.DB
}

// ServeHTTP performs readiness checks and returns 200 OK if ready,
// or 503 Service Unavailable if the database is not ready.
func (h *ReadyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if h.DB == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	if err := h.DB.PingContext(ctx); err != nil {
		http.Error(w, "database not ready: "+err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ready")); err != nil {
		log.Printf("ready: failed to write response: %v", err)
	}
}

// LiveHandler handles Kubernetes liveness probe requests.
// It performs a lightweight check to verify the application is responsive.
type LiveHandler struct{}

// ServeHTTP performs a simple liveness check and always returns 200 OK
// if the application is running and able to respond.
func (h *LiveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("alive")); err != nil {
		log.Printf("alive: failed to write response: %v", err)
	}
}
