package middleware

import (
	"sync"
	"testing"
	"time"

	"catchup-feed/pkg/ratelimit"
)

// TestNewDegradationManager tests the DegradationManager constructor.
func TestNewDegradationManager(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		config := DegradationConfig{
			AutoAdjust:        true,
			CooldownPeriod:    2 * time.Minute,
			RelaxedMultiplier: 3,
			MinimalMultiplier: 15,
			Clock:             &ratelimit.SystemClock{},
			Metrics:           newMockRateLimitMetrics(),
			LimiterType:       "test",
		}

		dm := NewDegradationManager(config)

		if dm == nil {
			t.Fatal("Expected non-nil degradation manager")
		}
		if dm.config.CooldownPeriod != 2*time.Minute {
			t.Errorf("Expected cooldown 2m, got %s", dm.config.CooldownPeriod)
		}
		if dm.config.RelaxedMultiplier != 3 {
			t.Errorf("Expected relaxed multiplier 3, got %d", dm.config.RelaxedMultiplier)
		}
	})

	t.Run("applies defaults for zero values", func(t *testing.T) {
		config := DegradationConfig{
			CooldownPeriod:    0, // Should apply default
			RelaxedMultiplier: 0, // Should apply default
			MinimalMultiplier: 0, // Should apply default
		}

		dm := NewDegradationManager(config)

		if dm.config.CooldownPeriod != 1*time.Minute {
			t.Errorf("Expected default cooldown 1m, got %s", dm.config.CooldownPeriod)
		}
		if dm.config.RelaxedMultiplier != 2 {
			t.Errorf("Expected default relaxed multiplier 2, got %d", dm.config.RelaxedMultiplier)
		}
		if dm.config.MinimalMultiplier != 10 {
			t.Errorf("Expected default minimal multiplier 10, got %d", dm.config.MinimalMultiplier)
		}
		if dm.config.Clock == nil {
			t.Error("Expected default clock to be set")
		}
		if dm.config.Metrics == nil {
			t.Error("Expected default metrics to be set")
		}
	})

	t.Run("starts at normal level", func(t *testing.T) {
		config := DefaultDegradationConfig()
		dm := NewDegradationManager(config)

		if dm.GetLevel() != LevelNormal {
			t.Errorf("Expected initial level Normal, got %s", dm.GetLevel())
		}
	})
}

// TestDegradationManager_GetLevel tests level retrieval.
func TestDegradationManager_GetLevel(t *testing.T) {
	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config)

	if dm.GetLevel() != LevelNormal {
		t.Errorf("Expected Normal level, got %s", dm.GetLevel())
	}
}

// TestDegradationManager_SetLevel tests manual level override.
func TestDegradationManager_SetLevel(t *testing.T) {
	metrics := newMockRateLimitMetrics()
	config := DegradationConfig{
		AutoAdjust:  true,
		Metrics:     metrics,
		LimiterType: "test",
	}
	dm := NewDegradationManager(config)

	// Set to relaxed level
	dm.SetLevel(LevelRelaxed)

	if dm.GetLevel() != LevelRelaxed {
		t.Errorf("Expected Relaxed level, got %s", dm.GetLevel())
	}

	// Verify metrics recorded
	if len(metrics.degradationLevels) != 2 { // Initial + manual override
		t.Errorf("Expected 2 degradation level records, got %d", len(metrics.degradationLevels))
	}
	if metrics.degradationLevels[1] != int(LevelRelaxed) {
		t.Errorf("Expected degradation level %d, got %d", LevelRelaxed, metrics.degradationLevels[1])
	}
}

// TestDegradationManager_ClearManualOverride tests clearing manual override.
func TestDegradationManager_ClearManualOverride(t *testing.T) {
	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config)

	// Set manual override
	dm.SetLevel(LevelMinimal)

	if dm.GetLevel() != LevelMinimal {
		t.Errorf("Expected Minimal level, got %s", dm.GetLevel())
	}

	// Clear override
	dm.ClearManualOverride()

	// Should return to auto-adjusted level (Normal)
	if dm.GetLevel() != LevelNormal {
		t.Errorf("Expected Normal level after clearing override, got %s", dm.GetLevel())
	}
}

// TestDegradationManager_AdjustLimits tests rate limit adjustment based on level.
func TestDegradationManager_AdjustLimits(t *testing.T) {
	config := DegradationConfig{
		RelaxedMultiplier: 2,
		MinimalMultiplier: 10,
	}
	dm := NewDegradationManager(config)

	baseLimit := 100

	testCases := []struct {
		name          string
		level         DegradationLevel
		expectedLimit int
	}{
		{
			name:          "normal level",
			level:         LevelNormal,
			expectedLimit: 100, // 1x
		},
		{
			name:          "relaxed level",
			level:         LevelRelaxed,
			expectedLimit: 200, // 2x
		},
		{
			name:          "minimal level",
			level:         LevelMinimal,
			expectedLimit: 1000, // 10x
		},
		{
			name:          "disabled level",
			level:         LevelDisabled,
			expectedLimit: 0, // Unlimited
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dm.SetLevel(tc.level)
			adjusted := dm.AdjustLimits(baseLimit)

			if adjusted != tc.expectedLimit {
				t.Errorf("Expected adjusted limit %d, got %d", tc.expectedLimit, adjusted)
			}
		})
	}
}

// TestDegradationManager_OnCircuitOpen tests degradation on circuit breaker open.
func TestDegradationManager_OnCircuitOpen(t *testing.T) {
	clock := newMockClock(time.Now())
	config := DegradationConfig{
		AutoAdjust:     true,
		CooldownPeriod: 1 * time.Minute,
		Clock:          clock,
		Metrics:        newMockRateLimitMetrics(),
		LimiterType:    "test",
	}
	dm := NewDegradationManager(config)

	// Initially at Normal level
	if dm.GetLevel() != LevelNormal {
		t.Fatalf("Expected initial level Normal, got %s", dm.GetLevel())
	}

	// Advance time past cooldown
	clock.Advance(2 * time.Minute)

	// Circuit opens
	dm.OnCircuitOpen()

	// Should move to Relaxed level (circuit open only → graduated response)
	// Design: Circuit open alone is less severe than memory pressure
	if dm.GetLevel() != LevelRelaxed {
		t.Errorf("Expected Relaxed level after circuit open (graduated response), got %s", dm.GetLevel())
	}
}

// TestDegradationManager_OnCircuitClose tests recovery on circuit breaker close.
func TestDegradationManager_OnCircuitClose(t *testing.T) {
	clock := newMockClock(time.Now())
	config := DegradationConfig{
		AutoAdjust:     true,
		CooldownPeriod: 1 * time.Minute,
		Clock:          clock,
		Metrics:        newMockRateLimitMetrics(),
		LimiterType:    "test",
	}
	dm := NewDegradationManager(config)

	// Open circuit
	clock.Advance(2 * time.Minute)
	dm.OnCircuitOpen()

	if dm.GetLevel() != LevelRelaxed {
		t.Fatalf("Expected Relaxed level after circuit open (graduated response), got %s", dm.GetLevel())
	}

	// Advance time past cooldown
	clock.Advance(2 * time.Minute)

	// Close circuit
	dm.OnCircuitClose()

	// Should return to Normal level
	if dm.GetLevel() != LevelNormal {
		t.Errorf("Expected Normal level after circuit close, got %s", dm.GetLevel())
	}
}

// TestDegradationManager_OnHighMemoryPressure tests degradation on high memory pressure.
func TestDegradationManager_OnHighMemoryPressure(t *testing.T) {
	clock := newMockClock(time.Now())
	config := DegradationConfig{
		AutoAdjust:     true,
		CooldownPeriod: 1 * time.Minute,
		Clock:          clock,
		Metrics:        newMockRateLimitMetrics(),
		LimiterType:    "test",
	}
	dm := NewDegradationManager(config)

	// Advance time past cooldown
	clock.Advance(2 * time.Minute)

	// High memory pressure
	dm.OnHighMemoryPressure()

	// Should move to Minimal level (high memory, circuit closed)
	if dm.GetLevel() != LevelMinimal {
		t.Errorf("Expected Minimal level after high memory pressure, got %s", dm.GetLevel())
	}
}

// TestDegradationManager_OnNormalMemoryPressure tests recovery from memory pressure.
func TestDegradationManager_OnNormalMemoryPressure(t *testing.T) {
	clock := newMockClock(time.Now())
	config := DegradationConfig{
		AutoAdjust:     true,
		CooldownPeriod: 1 * time.Minute,
		Clock:          clock,
		Metrics:        newMockRateLimitMetrics(),
		LimiterType:    "test",
	}
	dm := NewDegradationManager(config)

	// Trigger high memory pressure
	clock.Advance(2 * time.Minute)
	dm.OnHighMemoryPressure()

	if dm.GetLevel() != LevelMinimal {
		t.Fatalf("Expected Minimal level, got %s", dm.GetLevel())
	}

	// Advance time past cooldown
	clock.Advance(2 * time.Minute)

	// Memory pressure normalizes
	dm.OnNormalMemoryPressure()

	// Should return to Normal level
	if dm.GetLevel() != LevelNormal {
		t.Errorf("Expected Normal level after normal memory pressure, got %s", dm.GetLevel())
	}
}

// TestDegradationManager_CriticalState tests degradation to Disabled level.
func TestDegradationManager_CriticalState(t *testing.T) {
	clock := newMockClock(time.Now())
	config := DegradationConfig{
		AutoAdjust:     true,
		CooldownPeriod: 1 * time.Minute,
		Clock:          clock,
		Metrics:        newMockRateLimitMetrics(),
		LimiterType:    "test",
	}
	dm := NewDegradationManager(config)

	// Advance time past cooldown
	clock.Advance(2 * time.Minute)

	// Circuit opens - moves to Relaxed
	dm.OnCircuitOpen()
	if dm.GetLevel() != LevelRelaxed {
		t.Errorf("Expected Relaxed level after circuit open, got %s", dm.GetLevel())
	}

	// Advance time past cooldown again
	clock.Advance(2 * time.Minute)

	// High memory pressure - should move to Disabled (both conditions now true)
	dm.OnHighMemoryPressure()

	// Should move to Disabled level (critical state: both circuit open and memory pressure)
	if dm.GetLevel() != LevelDisabled {
		t.Errorf("Expected Disabled level in critical state, got %s", dm.GetLevel())
	}
}

// TestDegradationManager_CooldownPreventsFlapping tests cooldown prevents rapid level changes.
func TestDegradationManager_CooldownPreventsFlapping(t *testing.T) {
	clock := newMockClock(time.Now())
	config := DegradationConfig{
		AutoAdjust:     true,
		CooldownPeriod: 1 * time.Minute,
		Clock:          clock,
		Metrics:        newMockRateLimitMetrics(),
		LimiterType:    "test",
	}
	dm := NewDegradationManager(config)

	// Circuit opens
	dm.OnCircuitOpen()

	// Level should not change (within cooldown period)
	if dm.GetLevel() != LevelNormal {
		t.Errorf("Expected level to remain Normal (cooldown), got %s", dm.GetLevel())
	}

	// Advance time past cooldown
	clock.Advance(2 * time.Minute)

	// Circuit opens again
	dm.OnCircuitOpen()

	// Now level should change (circuit open only → Relaxed)
	if dm.GetLevel() != LevelRelaxed {
		t.Errorf("Expected Relaxed level after cooldown (graduated response), got %s", dm.GetLevel())
	}
}

// TestDegradationManager_ManualOverrideIgnoresAutoAdjust tests manual override takes precedence.
func TestDegradationManager_ManualOverrideIgnoresAutoAdjust(t *testing.T) {
	clock := newMockClock(time.Now())
	config := DegradationConfig{
		AutoAdjust:     true,
		CooldownPeriod: 1 * time.Minute,
		Clock:          clock,
		Metrics:        newMockRateLimitMetrics(),
		LimiterType:    "test",
	}
	dm := NewDegradationManager(config)

	// Set manual override to Normal
	dm.SetLevel(LevelNormal)

	// Advance time past cooldown
	clock.Advance(2 * time.Minute)

	// Circuit opens (should trigger auto-adjustment, but manual override prevents it)
	dm.OnCircuitOpen()

	// Level should remain Normal (manual override)
	if dm.GetLevel() != LevelNormal {
		t.Errorf("Expected Normal level (manual override), got %s", dm.GetLevel())
	}

	// Clear override
	dm.ClearManualOverride()

	// Advance time past cooldown
	clock.Advance(2 * time.Minute)

	// Trigger another circuit open
	dm.OnCircuitOpen()

	// Now auto-adjustment should work (circuit open only → Relaxed)
	if dm.GetLevel() != LevelRelaxed {
		t.Errorf("Expected Relaxed level after clearing override (graduated response), got %s", dm.GetLevel())
	}
}

// TestDegradationManager_AutoAdjustDisabled tests behavior when auto-adjust is disabled.
func TestDegradationManager_AutoAdjustDisabled(t *testing.T) {
	config := DegradationConfig{
		AutoAdjust:  false, // Disabled
		Metrics:     newMockRateLimitMetrics(),
		LimiterType: "test",
	}
	dm := NewDegradationManager(config)

	// Circuit opens
	dm.OnCircuitOpen()

	// Level should not change (auto-adjust disabled)
	if dm.GetLevel() != LevelNormal {
		t.Errorf("Expected Normal level (auto-adjust disabled), got %s", dm.GetLevel())
	}
}

// TestDegradationManager_Stats tests statistics retrieval.
func TestDegradationManager_Stats(t *testing.T) {
	clock := newMockClock(time.Now())
	config := DegradationConfig{
		AutoAdjust:     true,
		CooldownPeriod: 1 * time.Minute,
		Clock:          clock,
		Metrics:        newMockRateLimitMetrics(),
		LimiterType:    "test",
	}
	dm := NewDegradationManager(config)

	// Get initial stats
	stats := dm.Stats()

	if stats.EffectiveLevel != LevelNormal {
		t.Errorf("Expected effective level Normal, got %s", stats.EffectiveLevel)
	}
	if stats.ManualOverride {
		t.Error("Expected manual override to be false")
	}
	if stats.CircuitOpen {
		t.Error("Expected circuit open to be false")
	}
	if stats.MemoryPressure {
		t.Error("Expected memory pressure to be false")
	}

	// Set manual override
	dm.SetLevel(LevelMinimal)

	// Open circuit
	clock.Advance(2 * time.Minute)
	dm.OnCircuitOpen()

	// Trigger memory pressure
	dm.OnHighMemoryPressure()

	// Get updated stats
	stats = dm.Stats()

	if stats.EffectiveLevel != LevelMinimal {
		t.Errorf("Expected effective level Minimal (manual override), got %s", stats.EffectiveLevel)
	}
	if !stats.ManualOverride {
		t.Error("Expected manual override to be true")
	}
	if !stats.CircuitOpen {
		t.Error("Expected circuit open to be true")
	}
	if !stats.MemoryPressure {
		t.Error("Expected memory pressure to be true")
	}
}

// TestDegradationLevel_String tests string representation of degradation levels.
func TestDegradationLevel_String(t *testing.T) {
	testCases := []struct {
		level    DegradationLevel
		expected string
	}{
		{LevelNormal, "normal"},
		{LevelRelaxed, "relaxed"},
		{LevelMinimal, "minimal"},
		{LevelDisabled, "disabled"},
		{DegradationLevel(999), "unknown"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			if tc.level.String() != tc.expected {
				t.Errorf("Expected %s, got %s", tc.expected, tc.level.String())
			}
		})
	}
}

// TestDefaultDegradationConfig tests default configuration values.
func TestDefaultDegradationConfig(t *testing.T) {
	config := DefaultDegradationConfig()

	if !config.AutoAdjust {
		t.Error("Expected auto-adjust to be true")
	}
	if config.CooldownPeriod != 1*time.Minute {
		t.Errorf("Expected cooldown 1m, got %s", config.CooldownPeriod)
	}
	if config.RelaxedMultiplier != 2 {
		t.Errorf("Expected relaxed multiplier 2, got %d", config.RelaxedMultiplier)
	}
	if config.MinimalMultiplier != 10 {
		t.Errorf("Expected minimal multiplier 10, got %d", config.MinimalMultiplier)
	}
	if config.Clock == nil {
		t.Error("Expected clock to be set")
	}
	if config.Metrics == nil {
		t.Error("Expected metrics to be set")
	}
}

// TestDegradationManager_ConcurrentAccess tests thread-safety.
func TestDegradationManager_ConcurrentAccess(t *testing.T) {
	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config)

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrent reads
	for i := 0; i < numGoroutines/2; i++ {
		go func() {
			defer wg.Done()
			_ = dm.GetLevel()
			_ = dm.Stats()
		}()
	}

	// Concurrent writes
	for i := 0; i < numGoroutines/2; i++ {
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				dm.SetLevel(LevelRelaxed)
			} else {
				dm.OnCircuitOpen()
				dm.OnHighMemoryPressure()
			}
		}(i)
	}

	wg.Wait()

	// Verify manager is still functional
	level := dm.GetLevel()
	if level < LevelNormal || level > LevelDisabled {
		t.Errorf("Invalid level after concurrent access: %d", level)
	}
}

// TestDegradationManager_LevelTransitions tests all possible level transitions.
func TestDegradationManager_LevelTransitions(t *testing.T) {
	testCases := []struct {
		name           string
		circuitOpen    bool
		memoryPressure bool
		expectedLevel  DegradationLevel
	}{
		{
			name:           "normal state",
			circuitOpen:    false,
			memoryPressure: false,
			expectedLevel:  LevelNormal,
		},
		{
			name:           "circuit open only",
			circuitOpen:    true,
			memoryPressure: false,
			expectedLevel:  LevelRelaxed, // Graduated response: circuit open only → Relaxed
		},
		{
			name:           "memory pressure only",
			circuitOpen:    false,
			memoryPressure: true,
			expectedLevel:  LevelMinimal,
		},
		{
			name:           "critical state",
			circuitOpen:    true,
			memoryPressure: true,
			expectedLevel:  LevelDisabled,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			clock := newMockClock(time.Now())
			config := DegradationConfig{
				AutoAdjust:     true,
				CooldownPeriod: 1 * time.Minute,
				Clock:          clock,
				Metrics:        newMockRateLimitMetrics(),
				LimiterType:    "test",
			}
			dm := NewDegradationManager(config)

			// Advance past cooldown
			clock.Advance(2 * time.Minute)

			// Set up conditions
			if tc.circuitOpen {
				dm.OnCircuitOpen()
			} else {
				dm.OnCircuitClose()
			}

			// Advance past cooldown again for second condition change
			// This is needed because the first condition change resets the cooldown timer
			clock.Advance(2 * time.Minute)

			if tc.memoryPressure {
				dm.OnHighMemoryPressure()
			} else {
				dm.OnNormalMemoryPressure()
			}

			// Verify level
			if dm.GetLevel() != tc.expectedLevel {
				t.Errorf("Expected level %s, got %s", tc.expectedLevel, dm.GetLevel())
			}
		})
	}
}

// TestDegradationManager_MetricsRecording tests metrics are recorded correctly.
func TestDegradationManager_MetricsRecording(t *testing.T) {
	metrics := newMockRateLimitMetrics()
	clock := newMockClock(time.Now())

	config := DegradationConfig{
		AutoAdjust:     true,
		CooldownPeriod: 1 * time.Minute,
		Clock:          clock,
		Metrics:        metrics,
		LimiterType:    "test",
	}
	dm := NewDegradationManager(config)

	// Initial level should be recorded
	if len(metrics.degradationLevels) != 1 {
		t.Errorf("Expected 1 initial degradation level record, got %d", len(metrics.degradationLevels))
	}

	// Advance past cooldown
	clock.Advance(2 * time.Minute)

	// Trigger level change
	dm.OnCircuitOpen()

	// Verify metrics recorded
	if len(metrics.degradationLevels) != 2 {
		t.Errorf("Expected 2 degradation level records, got %d", len(metrics.degradationLevels))
	}
	if metrics.degradationLevels[1] != int(LevelRelaxed) {
		t.Errorf("Expected degradation level %d (Relaxed), got %d", LevelRelaxed, metrics.degradationLevels[1])
	}
}

// TestDegradationManager_AdjustLimits_EdgeCases tests edge cases in limit adjustment.
func TestDegradationManager_AdjustLimits_EdgeCases(t *testing.T) {
	testCases := []struct {
		name          string
		baseLimit     int
		level         DegradationLevel
		expectedLimit int
	}{
		{
			name:          "zero base limit",
			baseLimit:     0,
			level:         LevelNormal,
			expectedLimit: 0,
		},
		{
			name:          "negative base limit (invalid)",
			baseLimit:     -100,
			level:         LevelRelaxed,
			expectedLimit: -200, // 2x multiplier still applies
		},
		{
			name:          "large base limit",
			baseLimit:     1000000,
			level:         LevelMinimal,
			expectedLimit: 10000000, // 10x multiplier
		},
		{
			name:          "disabled level returns zero",
			baseLimit:     100,
			level:         LevelDisabled,
			expectedLimit: 0,
		},
	}

	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dm.SetLevel(tc.level)
			adjusted := dm.AdjustLimits(tc.baseLimit)

			if adjusted != tc.expectedLimit {
				t.Errorf("Expected adjusted limit %d, got %d", tc.expectedLimit, adjusted)
			}
		})
	}
}

// BenchmarkDegradationManager_GetLevel benchmarks level retrieval.
func BenchmarkDegradationManager_GetLevel(b *testing.B) {
	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dm.GetLevel()
	}
}

// BenchmarkDegradationManager_AdjustLimits benchmarks limit adjustment.
func BenchmarkDegradationManager_AdjustLimits(b *testing.B) {
	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dm.AdjustLimits(100)
	}
}

// BenchmarkDegradationManager_OnCircuitOpen benchmarks circuit open event handling.
func BenchmarkDegradationManager_OnCircuitOpen(b *testing.B) {
	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dm.OnCircuitOpen()
	}
}

// BenchmarkDegradationManager_Stats benchmarks statistics retrieval.
func BenchmarkDegradationManager_Stats(b *testing.B) {
	config := DefaultDegradationConfig()
	dm := NewDegradationManager(config)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = dm.Stats()
	}
}
