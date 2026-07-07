package learning

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestBroadcastDay pins the §12-10 contract: the broadcast day is the JST
// calendar date regardless of the input's zone (Pi の Postgres は UTC 運用、
// Mac はローカル時刻 — どこから来た time.Time でも同じ日に落ちること).
func TestBroadcastDay(t *testing.T) {
	tests := []struct {
		name string
		now  time.Time
		want time.Time
	}{
		{
			// 04:30 JST の放送時刻はもちろんその日。
			"radio batch time in JST",
			time.Date(2026, 7, 7, 4, 30, 0, 0, time.FixedZone("JST", 9*3600)),
			time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
		},
		{
			// UTC 19:30 前日 = JST 04:30 当日。naive-UTC 前歴の再現ケース。
			"UTC evening is already the next JST day",
			time.Date(2026, 7, 6, 19, 30, 0, 0, time.UTC),
			time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
		},
		{
			// JST 深夜 0 時ちょうど(境界)。
			"JST midnight boundary",
			time.Date(2026, 7, 6, 15, 0, 0, 0, time.UTC), // = 2026-07-07 00:00 JST
			time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC),
		},
		{
			// その 1 ナノ秒前はまだ前日。
			"one nanosecond before JST midnight",
			time.Date(2026, 7, 6, 14, 59, 59, 999999999, time.UTC),
			time.Date(2026, 7, 6, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BroadcastDay(tt.now)
			assert.True(t, got.Equal(tt.want), "got %v, want %v", got, tt.want)
			assert.Equal(t, time.UTC, got.Location(), "days normalize to midnight UTC")
		})
	}
}

func TestFirstDueDay(t *testing.T) {
	// 生成日の翌日 (§5.1: 当日のクイズコーナーには出さない)。
	now := time.Date(2026, 7, 7, 4, 30, 0, 0, time.FixedZone("JST", 9*3600))
	assert.Equal(t, time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC), FirstDueDay(now))
}

func TestFormatDay(t *testing.T) {
	assert.Equal(t, "2026-07-07", FormatDay(time.Date(2026, 7, 7, 0, 0, 0, 0, time.UTC)))
}
