package middleware

import (
	"log/slog"
	"sync"
	"time"

	"catchup-feed/pkg/ratelimit"
)

// DegradationLevel represents the current degradation level for rate limiting.
//
// The rate limiter supports multi-level degradation to maintain availability
// during system stress or failures. Higher degradation levels apply increasingly
// relaxed rate limits.
type DegradationLevel int

const (
	// LevelNormal indicates normal rate limiting with standard limits (1x).
	// This is the default operating mode under healthy conditions.
	LevelNormal DegradationLevel = iota

	// LevelRelaxed indicates relaxed rate limiting with doubled limits (2x).
	// Activated when:
	//   - Circuit breaker opens
	//   - Memory pressure is detected
	//   - Error rate or latency increases
	LevelRelaxed

	// LevelMinimal indicates minimal rate limiting with 10x limits.
	// Activated when:
	//   - Circuit breaker remains open for extended period
	//   - High memory pressure persists
	//   - System is under significant stress
	LevelMinimal

	// LevelDisabled indicates rate limiting is completely disabled.
	// Activated when:
	//   - Circuit breaker fails repeatedly
	//   - Critical system failures
	//   - Manual override
	// WARNING: This prioritizes availability over security.
	LevelDisabled
)

// String returns a string representation of the degradation level.
func (l DegradationLevel) String() string {
	switch l {
	case LevelNormal:
		return "normal"
	case LevelRelaxed:
		return "relaxed"
	case LevelMinimal:
		return "minimal"
	case LevelDisabled:
		return "disabled"
	default:
		return "unknown"
	}
}

// DegradationConfig holds configuration for the degradation manager.
type DegradationConfig struct {
	// AutoAdjust enables automatic degradation level adjustment based on system health.
	// Default: true
	AutoAdjust bool

	// CooldownPeriod is the minimum time between level changes to prevent flapping.
	// Default: 1 minute
	CooldownPeriod time.Duration

	// RelaxedMultiplier is the rate limit multiplier for relaxed level (default: 2).
	RelaxedMultiplier int

	// MinimalMultiplier is the rate limit multiplier for minimal level (default: 10).
	MinimalMultiplier int

	// Clock provides time abstraction for testing.
	// Default: ratelimit.SystemClock
	Clock ratelimit.Clock

	// Metrics for recording degradation level changes.
	Metrics ratelimit.RateLimitMetrics

	// LimiterType identifies which rate limiter this degradation manager protects.
	// Examples: "ip", "user"
	LimiterType string
}

// DefaultDegradationConfig returns default configuration for degradation manager.
func DefaultDegradationConfig() DegradationConfig {
	return DegradationConfig{
		AutoAdjust:        true,
		CooldownPeriod:    1 * time.Minute,
		RelaxedMultiplier: 2,
		MinimalMultiplier: 10,
		Clock:             &ratelimit.SystemClock{},
		Metrics:           &ratelimit.NoOpMetrics{},
	}
}

// DegradationManager manages multi-level graceful degradation of rate limiting.
//
// This component monitors system health indicators and automatically adjusts
// rate limit strictness to maintain availability during stress or failures.
//
// Key features:
//   - Four degradation levels: Normal, Relaxed, Minimal, Disabled
//   - Automatic adjustment based on circuit breaker state and memory pressure
//   - Manual override capability for operational control
//   - Cooldown period to prevent level flapping
//   - Thread-safe concurrent access
//
// Health Indicators:
//   - Circuit breaker state (open/closed)
//   - Memory pressure (high/normal)
//   - Error rate (tracked externally)
//   - Latency (tracked externally)
//
// Degradation Rules:
//   - Circuit breaker open → Move to Relaxed or higher
//   - High memory pressure → Move to Minimal or Disabled
//   - Circuit breaker closed + Normal memory → Move back to Normal
//   - Cooldown period enforced between level changes
type DegradationManager struct {
	config DegradationConfig

	mu              sync.RWMutex
	currentLevel    DegradationLevel
	lastLevelChange time.Time
	circuitOpen     bool
	memoryPressure  bool
	manualOverride  *DegradationLevel
}

// NewDegradationManager creates a new degradation manager with the given configuration.
//
// If config values are invalid or zero, defaults are applied:
//   - CooldownPeriod: 1 minute
//   - RelaxedMultiplier: 2
//   - MinimalMultiplier: 10
//   - Clock: SystemClock
//   - Metrics: NoOpMetrics
func NewDegradationManager(config DegradationConfig) *DegradationManager {
	// Apply defaults
	if config.CooldownPeriod <= 0 {
		config.CooldownPeriod = 1 * time.Minute
	}
	if config.RelaxedMultiplier <= 0 {
		config.RelaxedMultiplier = 2
	}
	if config.MinimalMultiplier <= 0 {
		config.MinimalMultiplier = 10
	}
	if config.Clock == nil {
		config.Clock = &ratelimit.SystemClock{}
	}
	if config.Metrics == nil {
		config.Metrics = &ratelimit.NoOpMetrics{}
	}

	dm := &DegradationManager{
		config:          config,
		currentLevel:    LevelNormal,
		lastLevelChange: config.Clock.Now(),
		circuitOpen:     false,
		memoryPressure:  false,
	}

	// Record initial level
	config.Metrics.RecordDegradationLevel(config.LimiterType, int(LevelNormal))

	return dm
}

// GetLevel returns the current degradation level.
//
// If manual override is set, it returns the override level.
// Otherwise, returns the automatically adjusted level.
func (dm *DegradationManager) GetLevel() DegradationLevel {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	if dm.manualOverride != nil {
		return *dm.manualOverride
	}

	return dm.currentLevel
}

// SetLevel manually sets the degradation level, overriding automatic adjustment.
//
// This is useful for:
//   - Emergency operational control
//   - Testing degradation behavior
//   - Forcing strict rate limiting during security incidents
//
// Call ClearManualOverride() to resume automatic adjustment.
//
// Parameters:
//   - level: Desired degradation level
func (dm *DegradationManager) SetLevel(level DegradationLevel) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.manualOverride = &level
	dm.config.Metrics.RecordDegradationLevel(dm.config.LimiterType, int(level))

	slog.Info("Degradation level manually set",
		slog.String("limiter_type", dm.config.LimiterType),
		slog.String("level", level.String()),
	)
}

// ClearManualOverride clears the manual override and resumes automatic adjustment.
func (dm *DegradationManager) ClearManualOverride() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	if dm.manualOverride != nil {
		dm.manualOverride = nil

		slog.Info("Degradation manual override cleared, resuming auto-adjustment",
			slog.String("limiter_type", dm.config.LimiterType),
			slog.String("current_level", dm.currentLevel.String()),
		)

		dm.config.Metrics.RecordDegradationLevel(dm.config.LimiterType, int(dm.currentLevel))
	}
}

// AdjustLimits adjusts the rate limit based on the current degradation level.
//
// This method calculates the effective rate limit by applying the appropriate
// multiplier for the current degradation level:
//   - Normal: 1x (baseLimit)
//   - Relaxed: 2x (baseLimit * RelaxedMultiplier)
//   - Minimal: 10x (baseLimit * MinimalMultiplier)
//   - Disabled: 0 (no limit, effectively unlimited)
//
// Parameters:
//   - baseLimit: The base rate limit under normal conditions
//
// Returns:
//   - int: Adjusted rate limit (0 means disabled)
func (dm *DegradationManager) AdjustLimits(baseLimit int) int {
	level := dm.GetLevel()

	switch level {
	case LevelNormal:
		return baseLimit

	case LevelRelaxed:
		return baseLimit * dm.config.RelaxedMultiplier

	case LevelMinimal:
		return baseLimit * dm.config.MinimalMultiplier

	case LevelDisabled:
		// Return 0 to indicate rate limiting is disabled
		return 0

	default:
		// Unknown level, fallback to normal
		return baseLimit
	}
}

// OnCircuitOpen is called when the circuit breaker opens.
//
// This indicates the rate limiter is experiencing sustained failures.
// Response: Move to Relaxed level to reduce rate limiting load.
// Note: The circuitOpen state is always updated for observability,
// even if manual override is set or auto-adjust is disabled.
func (dm *DegradationManager) OnCircuitOpen() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Always track the circuit state for observability
	dm.circuitOpen = true

	// Skip auto-adjustment if disabled
	if !dm.config.AutoAdjust {
		return
	}

	// Skip level change if manual override is set
	if dm.manualOverride != nil {
		return
	}

	// Attempt to degrade to Relaxed level
	dm.adjustLevel()
}

// OnCircuitClose is called when the circuit breaker closes.
//
// This indicates the rate limiter has recovered from failures.
// Response: If no other issues, move back toward Normal level.
// Note: The circuitOpen state is always updated for observability,
// even if manual override is set or auto-adjust is disabled.
func (dm *DegradationManager) OnCircuitClose() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Always track the circuit state for observability
	dm.circuitOpen = false

	// Skip auto-adjustment if disabled
	if !dm.config.AutoAdjust {
		return
	}

	// Skip level change if manual override is set
	if dm.manualOverride != nil {
		return
	}

	// Attempt to recover to Normal level
	dm.adjustLevel()
}

// OnHighMemoryPressure is called when high memory pressure is detected.
//
// This indicates the in-memory rate limit store is approaching capacity.
// Response: Move to Minimal or Disabled level to reduce memory usage.
// Note: The memoryPressure state is always updated for observability,
// even if manual override is set or auto-adjust is disabled.
func (dm *DegradationManager) OnHighMemoryPressure() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Always track memory pressure for observability
	dm.memoryPressure = true

	// Skip auto-adjustment if disabled
	if !dm.config.AutoAdjust {
		return
	}

	// Skip level change if manual override is set
	if dm.manualOverride != nil {
		return
	}

	// Attempt to degrade to Minimal level
	dm.adjustLevel()
}

// OnNormalMemoryPressure is called when memory pressure returns to normal.
//
// This indicates the in-memory rate limit store has sufficient capacity.
// Response: If no other issues, move back toward Normal level.
// Note: The memoryPressure state is always updated for observability,
// even if manual override is set or auto-adjust is disabled.
func (dm *DegradationManager) OnNormalMemoryPressure() {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Always track memory pressure for observability
	dm.memoryPressure = false

	// Skip auto-adjustment if disabled
	if !dm.config.AutoAdjust {
		return
	}

	// Skip level change if manual override is set
	if dm.manualOverride != nil {
		return
	}

	// Attempt to recover to Normal level
	dm.adjustLevel()
}

// adjustLevel adjusts the degradation level based on current health indicators.
//
// This method is called internally when health indicators change.
// It enforces cooldown period to prevent flapping.
//
// Degradation Rules (graduated response):
//  1. Circuit open + High memory → Disabled (critical state, prioritize availability)
//  2. High memory only → Minimal (10x limits to reduce memory usage)
//  3. Circuit open only → Relaxed (2x limits to reduce load on failing component)
//  4. Both indicators normal → Normal (standard rate limiting)
func (dm *DegradationManager) adjustLevel() {
	now := dm.config.Clock.Now()

	// Enforce cooldown period to prevent flapping
	if now.Sub(dm.lastLevelChange) < dm.config.CooldownPeriod {
		return
	}

	oldLevel := dm.currentLevel
	var newLevel DegradationLevel

	// Determine target level based on health indicators (graduated response)
	if dm.circuitOpen && dm.memoryPressure {
		// Critical: Both circuit open and memory pressure → Disabled
		newLevel = LevelDisabled
	} else if dm.memoryPressure {
		// High memory pressure (more severe) → Minimal (10x)
		newLevel = LevelMinimal
	} else if dm.circuitOpen {
		// Circuit open only → Relaxed (2x)
		newLevel = LevelRelaxed
	} else {
		// Healthy: Both indicators normal → Normal
		newLevel = LevelNormal
	}

	// Skip if level unchanged
	if newLevel == oldLevel {
		return
	}

	// Update level
	dm.currentLevel = newLevel
	dm.lastLevelChange = now
	dm.config.Metrics.RecordDegradationLevel(dm.config.LimiterType, int(newLevel))

	// Determine reason for degradation
	var reason string
	if dm.circuitOpen && dm.memoryPressure {
		reason = "circuit_open,memory_pressure"
	} else if dm.circuitOpen {
		reason = "circuit_open"
	} else if dm.memoryPressure {
		reason = "memory_pressure"
	} else {
		reason = "recovery"
	}

	// Log degradation level change at WARN level
	slog.Warn("degradation level changed",
		slog.String("limiter_type", dm.config.LimiterType),
		slog.String("previous_level", oldLevel.String()),
		slog.String("new_level", newLevel.String()),
		slog.String("reason", reason),
		slog.Bool("circuit_open", dm.circuitOpen),
		slog.Bool("memory_pressure", dm.memoryPressure),
	)
}

// DegradationStats contains current degradation manager statistics.
type DegradationStats struct {
	// EffectiveLevel is the current effective degradation level (respects manual override).
	// This is the level that AdjustLimits() uses.
	EffectiveLevel DegradationLevel

	// InternalLevel is the automatically calculated level (ignores manual override).
	// This shows what the level would be based on health indicators.
	InternalLevel DegradationLevel

	// ManualOverride indicates whether a manual override is active.
	ManualOverride bool

	// CircuitOpen indicates whether the circuit breaker is currently open.
	CircuitOpen bool

	// MemoryPressure indicates whether high memory pressure is detected.
	MemoryPressure bool

	// LastLevelChange is the timestamp of the last automatic level change.
	LastLevelChange time.Time
}

// Stats returns current degradation manager statistics for monitoring.
//
// The EffectiveLevel field returns the level that is actually in use,
// which respects manual override. The InternalLevel field shows the
// automatically calculated level based on health indicators.
func (dm *DegradationManager) Stats() DegradationStats {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	// Calculate effective level (same logic as GetLevel)
	effectiveLevel := dm.currentLevel
	if dm.manualOverride != nil {
		effectiveLevel = *dm.manualOverride
	}

	return DegradationStats{
		EffectiveLevel:  effectiveLevel,
		InternalLevel:   dm.currentLevel,
		ManualOverride:  dm.manualOverride != nil,
		CircuitOpen:     dm.circuitOpen,
		MemoryPressure:  dm.memoryPressure,
		LastLevelChange: dm.lastLevelChange,
	}
}
