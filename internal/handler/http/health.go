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

	"catchup-feed/pkg/ratelimit"
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

// RateLimiterHealthInfo contains health information for a rate limiter instance.
type RateLimiterHealthInfo struct {
	ActiveKeys      int    `json:"active_keys"`       // Number of active keys being tracked
	MemoryBytes     int64  `json:"memory_bytes"`      // Estimated memory usage in bytes
	CircuitBreaker  string `json:"circuit_breaker"`   // Circuit breaker state (closed/open/half-open)
	DegradationLevel string `json:"degradation_level"` // Degradation level (normal/relaxed/minimal/disabled)
}

// CSPHealthInfo contains health information for CSP middleware.
type CSPHealthInfo struct {
	Enabled    bool `json:"enabled"`     // Whether CSP is enabled
	ReportOnly bool `json:"report_only"` // Whether CSP is in report-only mode
}

// HealthHandler handles health check endpoint requests.
// It performs database connectivity checks and returns detailed health status.
// It also reports rate limiter and CSP status for operational monitoring.
type HealthHandler struct {
	DB      *sql.DB
	Version string

	// Rate limiter components (optional)
	IPRateLimiterStore       ratelimit.RateLimitStore // IP rate limiter storage
	UserRateLimiterStore     ratelimit.RateLimitStore // User rate limiter storage
	IPCircuitBreaker         *ratelimit.CircuitBreaker // IP rate limiter circuit breaker
	UserCircuitBreaker       *ratelimit.CircuitBreaker // User rate limiter circuit breaker
	IPDegradationManager     DegradationManager // IP rate limiter degradation manager
	UserDegradationManager   DegradationManager // User rate limiter degradation manager
	RateLimiterEnabled       bool // Whether rate limiting is enabled

	// CSP status (optional)
	CSPEnabled    bool // Whether CSP is enabled
	CSPReportOnly bool // Whether CSP is in report-only mode
}

// DegradationManager defines the interface for accessing degradation level information.
// This allows the health check to report degradation status without depending on
// the full degradation manager implementation.
type DegradationManager interface {
	// GetLevel returns the current degradation level.
	GetLevel() DegradationLevel
}

// DegradationLevel represents the current degradation level for rate limiting.
type DegradationLevel interface {
	// String returns a string representation of the degradation level.
	String() string
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

	// レート制限チェック
	if h.RateLimiterEnabled {
		rateLimiterCheck := h.checkRateLimiter(ctx)
		checks["rate_limiter"] = rateLimiterCheck
		// Rate limiter degradation is not considered unhealthy
		// Only include if explicitly unhealthy
	}

	// CSPチェック
	if h.CSPEnabled {
		cspCheck := h.checkCSP()
		checks["csp"] = cspCheck
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

// checkRateLimiter checks the health of rate limiter components.
// It reports the status of both IP and user rate limiters, including:
// - Active key counts
// - Memory usage
// - Circuit breaker state
// - Degradation level
//
// Rate limiter health is always reported as "healthy" because:
// - Circuit breaker open = fail-open behavior (availability prioritized)
// - Degradation = graceful handling of overload (operational)
// - These states are informational, not failures
func (h *HealthHandler) checkRateLimiter(ctx context.Context) CheckStatus {
	details := make(map[string]interface{})

	// Check IP rate limiter
	if h.IPRateLimiterStore != nil {
		ipInfo := RateLimiterHealthInfo{}

		// Get active key count
		if keyCount, err := h.IPRateLimiterStore.KeyCount(ctx); err == nil {
			ipInfo.ActiveKeys = keyCount
		}

		// Get memory usage
		if memUsage, err := h.IPRateLimiterStore.MemoryUsage(ctx); err == nil {
			ipInfo.MemoryBytes = memUsage
		}

		// Get circuit breaker state
		if h.IPCircuitBreaker != nil {
			ipInfo.CircuitBreaker = h.IPCircuitBreaker.State().String()
		} else {
			ipInfo.CircuitBreaker = "not_configured"
		}

		// Get degradation level
		if h.IPDegradationManager != nil {
			ipInfo.DegradationLevel = h.IPDegradationManager.GetLevel().String()
		} else {
			ipInfo.DegradationLevel = "not_configured"
		}

		details["ip"] = ipInfo
	}

	// Check user rate limiter
	if h.UserRateLimiterStore != nil {
		userInfo := RateLimiterHealthInfo{}

		// Get active key count
		if keyCount, err := h.UserRateLimiterStore.KeyCount(ctx); err == nil {
			userInfo.ActiveKeys = keyCount
		}

		// Get memory usage
		if memUsage, err := h.UserRateLimiterStore.MemoryUsage(ctx); err == nil {
			userInfo.MemoryBytes = memUsage
		}

		// Get circuit breaker state
		if h.UserCircuitBreaker != nil {
			userInfo.CircuitBreaker = h.UserCircuitBreaker.State().String()
		} else {
			userInfo.CircuitBreaker = "not_configured"
		}

		// Get degradation level
		if h.UserDegradationManager != nil {
			userInfo.DegradationLevel = h.UserDegradationManager.GetLevel().String()
		} else {
			userInfo.DegradationLevel = "not_configured"
		}

		details["user"] = userInfo
	}

	// Rate limiter is always healthy (degradation and circuit states are operational)
	return CheckStatus{
		Status:  "healthy",
		Details: details,
	}
}

// checkCSP checks the health of CSP middleware.
// It reports the configuration status of Content Security Policy.
func (h *HealthHandler) checkCSP() CheckStatus {
	cspInfo := CSPHealthInfo{
		Enabled:    h.CSPEnabled,
		ReportOnly: h.CSPReportOnly,
	}

	return CheckStatus{
		Status:  "healthy",
		Details: map[string]interface{}{"config": cspInfo},
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
