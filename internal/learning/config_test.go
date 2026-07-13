package learning

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// D-18: M=1、ラダー [1,7,30]、S=4、閾値 30。D-17: 48h。
	cfg := LoadConfig(nil)
	assert.Equal(t, 1, cfg.ItemsPerDay)
	assert.Equal(t, []int{1, 7, 30}, cfg.Ladder)
	assert.Equal(t, 4, cfg.Slots)
	assert.Equal(t, 30, cfg.BackpressureThreshold)
	assert.Equal(t, 48*time.Hour, cfg.AutoResolveAfter)
	assert.Equal(t, time.Saturday, cfg.WeeklyReviewDOW, "D-21: 週次振り返りは土曜")
}

func TestLoadConfig_WeeklyReviewDOW(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Weekday
	}{
		{"english name", "monday", time.Monday},
		{"english name mixed case", "Sunday", time.Sunday},
		{"name with spaces", "  Friday ", time.Friday},
		{"go weekday number 0", "0", time.Sunday},
		{"go weekday number 6", "6", time.Saturday},
		{"invalid name falls back to Saturday", "someday", time.Saturday},
		{"out-of-range number falls back", "7", time.Saturday},
		{"negative number falls back", "-1", time.Saturday},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("QUIZ_WEEKLY_REVIEW_DOW", tt.value)
			cfg := LoadConfig(nil)
			assert.Equal(t, tt.want, cfg.WeeklyReviewDOW)
		})
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	t.Setenv("QUIZ_ITEMS_PER_DAY", "2")
	t.Setenv("QUIZ_LADDER_DAYS", "2, 5,10")
	t.Setenv("QUIZ_SLOTS", "7")
	t.Setenv("QUIZ_BACKPRESSURE_THRESHOLD", "50")
	t.Setenv("QUIZ_AUTO_RESOLVE_AFTER", "72h")

	cfg := LoadConfig(nil)
	assert.Equal(t, 2, cfg.ItemsPerDay)
	assert.Equal(t, []int{2, 5, 10}, cfg.Ladder, "spaces around commas are tolerated")
	assert.Equal(t, 7, cfg.Slots)
	assert.Equal(t, 50, cfg.BackpressureThreshold)
	assert.Equal(t, 72*time.Hour, cfg.AutoResolveAfter)
}

// TestLoadConfig_ItemsPerDay pins D-26 (3): 0 is a legitimate off switch
// (クイズ生成無効・相乗りセクションなし) and must survive LoadConfig
// unclamped; only negative values degrade to the default.
func TestLoadConfig_ItemsPerDay(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{"positive value kept", "2", 2},
		{"zero is a valid off value (D-26)", "0", 0},
		{"negative falls back to default", "-1", 1},
		{"non-numeric falls back to default", "many", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("QUIZ_ITEMS_PER_DAY", tt.value)
			cfg := LoadConfig(nil)
			assert.Equal(t, tt.want, cfg.ItemsPerDay)
		})
	}
}

func TestLoadConfig_InvalidValuesFallBack(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"non-numeric ladder entry", "QUIZ_LADDER_DAYS", "1,x,30"},
		{"zero ladder entry", "QUIZ_LADDER_DAYS", "1,0,30"},
		{"negative ladder entry", "QUIZ_LADDER_DAYS", "-1,7,30"},
		{"empty ladder entry", "QUIZ_LADDER_DAYS", "1,,30"},
		{"negative items per day", "QUIZ_ITEMS_PER_DAY", "-1"},
		{"negative slots", "QUIZ_SLOTS", "-4"},
		{"zero backpressure threshold", "QUIZ_BACKPRESSURE_THRESHOLD", "0"},
		{"negative auto-resolve delay", "QUIZ_AUTO_RESOLVE_AFTER", "-1h"},
	}
	want := LoadConfig(nil) // defaults
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			cfg := LoadConfig(nil)
			assert.Equal(t, want, cfg, "invalid %s=%q must degrade to defaults", tt.key, tt.value)
		})
	}
}
