package http

import (
	"context"
	"log/slog"
	"time"

	"catchup-feed/internal/handler/http/middleware"
	"catchup-feed/pkg/config"
	"catchup-feed/pkg/ratelimit"
)

// StartRateLimitCleanupLegacy starts a background goroutine that periodically
// cleans up expired entries from the legacy middleware.RateLimiter.
//
// This function prevents memory leaks by removing old timestamps
// that are no longer needed for rate limiting decisions.
//
// The cleanup runs in a loop with the specified interval and stops gracefully
// when the context is cancelled (e.g., during server shutdown).
//
// Parameters:
//   - ctx: Context for cancellation (typically server's context)
//   - limiter: The legacy rate limiter to clean up
//   - interval: How often to run cleanup (e.g., 5 minutes)
//   - limiterType: Type of rate limiter for logging (e.g., "auth" or "search")
func StartRateLimitCleanupLegacy(
	ctx context.Context,
	limiter *middleware.RateLimiter,
	interval time.Duration,
	limiterType string,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("rate limit cleanup started (legacy)",
		slog.String("limiter_type", limiterType),
		slog.Duration("interval", interval))

	for {
		select {
		case <-ctx.Done():
			slog.Info("rate limit cleanup stopped (legacy)",
				slog.String("limiter_type", limiterType))
			return

		case <-ticker.C:
			// Call the cleanup method
			limiter.CleanupExpired()

			slog.Debug("rate limit cleanup completed (legacy)",
				slog.String("limiter_type", limiterType))
		}
	}
}

// StartRateLimitCleanup starts a background goroutine that periodically
// cleans up expired entries from the rate limit store.
//
// This function prevents memory leaks by removing old timestamps from the store
// that are no longer needed for rate limiting decisions.
//
// The cleanup runs in a loop with the specified interval and stops gracefully
// when the context is cancelled (e.g., during server shutdown).
//
// Parameters:
//   - ctx: Context for cancellation (typically server's context)
//   - store: The rate limit store to clean up
//   - interval: How often to run cleanup (e.g., 5 minutes)
//   - windowDuration: The rate limit window duration for calculating cutoff
//   - limiterType: Type of rate limiter for logging (e.g., "ip" or "user")
func StartRateLimitCleanup(
	ctx context.Context,
	store *ratelimit.InMemoryRateLimitStore,
	interval time.Duration,
	windowDuration time.Duration,
	limiterType string,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("rate limit cleanup started",
		slog.String("limiter_type", limiterType),
		slog.Duration("interval", interval),
		slog.Duration("window_duration", windowDuration))

	for {
		select {
		case <-ctx.Done():
			slog.Info("rate limit cleanup stopped",
				slog.String("limiter_type", limiterType))
			return

		case <-ticker.C:
			// Calculate cutoff time: anything older than this should be removed
			// We use 2x window duration to ensure we don't remove data that might
			// still be needed for edge cases (e.g., clock skew, concurrent requests)
			cutoff := time.Now().Add(-2 * windowDuration)

			// Get key count before cleanup
			activeKeysBefore, err := store.KeyCount(ctx)
			if err != nil {
				slog.Error("failed to get key count before cleanup",
					slog.String("limiter_type", limiterType),
					slog.Any("error", err))
				continue
			}

			// Get memory usage before cleanup
			memoryBefore, err := store.MemoryUsage(ctx)
			if err != nil {
				slog.Error("failed to get memory usage before cleanup",
					slog.String("limiter_type", limiterType),
					slog.Any("error", err))
				continue
			}

			// Perform cleanup
			if err := store.Cleanup(ctx, cutoff); err != nil {
				slog.Error("rate limit cleanup failed",
					slog.String("limiter_type", limiterType),
					slog.Any("error", err))
				continue
			}

			// Get key count after cleanup
			activeKeysAfter, err := store.KeyCount(ctx)
			if err != nil {
				slog.Error("failed to get key count after cleanup",
					slog.String("limiter_type", limiterType),
					slog.Any("error", err))
				continue
			}

			// Get memory usage after cleanup
			memoryAfter, err := store.MemoryUsage(ctx)
			if err != nil {
				slog.Error("failed to get memory usage after cleanup",
					slog.String("limiter_type", limiterType),
					slog.Any("error", err))
				continue
			}

			// Calculate stats
			keysRemoved := activeKeysBefore - activeKeysAfter
			memoryFreed := memoryBefore - memoryAfter
			memoryFreedMB := float64(memoryFreed) / (1024 * 1024)

			// Log cleanup statistics
			slog.Debug("rate limit cleanup completed",
				slog.String("limiter_type", limiterType),
				slog.Int("active_keys_before", activeKeysBefore),
				slog.Int("active_keys_after", activeKeysAfter),
				slog.Int("keys_removed", keysRemoved),
				slog.Int64("memory_freed_bytes", memoryFreed),
				slog.Float64("memory_freed_mb", memoryFreedMB),
				slog.Time("cutoff_time", cutoff))

			// Warn if memory usage is high
			const warningThresholdMB = 80
			currentMemoryMB := float64(memoryAfter) / (1024 * 1024)
			if currentMemoryMB > warningThresholdMB {
				slog.Warn("rate limit store memory usage is high",
					slog.String("limiter_type", limiterType),
					slog.Float64("memory_usage_mb", currentMemoryMB),
					slog.Int("active_keys", activeKeysAfter))
			}
		}
	}
}

// CleanupConfig holds configuration for rate limit cleanup.
type CleanupConfig struct {
	// Interval specifies how often to run cleanup.
	// Default: 5 minutes
	Interval time.Duration

	// WindowDuration specifies the rate limit window duration.
	// Cutoff time is calculated as 2x this value to ensure safety.
	WindowDuration time.Duration

	// LimiterType identifies the type of rate limiter for logging.
	// Examples: "ip", "user"
	LimiterType string
}

// DefaultCleanupInterval is the default cleanup interval if not specified.
const DefaultCleanupInterval = 5 * time.Minute

// LoadCleanupConfigFromEnv loads cleanup configuration from environment variables.
//
// Environment variables:
//   - RATELIMIT_CLEANUP_INTERVAL: Cleanup interval (e.g., "5m", "10m")
//     Default: 5 minutes
//
// If parsing fails or values are invalid, defaults are used instead of failing.
// This implements graceful degradation for operational robustness.
func LoadCleanupConfigFromEnv() CleanupConfig {
	cfg := CleanupConfig{
		Interval: DefaultCleanupInterval,
	}

	// Parse cleanup interval from environment
	cfg.Interval = config.GetEnvDuration("RATELIMIT_CLEANUP_INTERVAL", DefaultCleanupInterval)

	return cfg
}
