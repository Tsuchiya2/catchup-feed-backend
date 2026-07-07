package learning

import (
	"log/slog"
	"strconv"
	"strings"
	"time"

	pkgconfig "catchup-feed/pkg/config"
)

// D-18 initial parameters, all overridable via env so parameter changes
// never need a code change (§12-12). The saturation arithmetic (§6.2):
// steady-state load = M × ladder段数 = 3問/日; S=4 covers it plus
// forgot-driven re-asks. Raising M is 配信量最適化への逆戻り — observe
// graduation rate first.
const (
	// defaultItemsPerDay is M — new learning items generated per day.
	defaultItemsPerDay = 1
	// defaultLadderDays is the interval ladder in days.
	defaultLadderDays = "1,7,30"
	// defaultSlots is S — quiz slots per episode (§6.2).
	defaultSlots = 4
	// defaultBackpressureThreshold stops new item generation while the
	// overdue backlog exceeds it (§5.2).
	defaultBackpressureThreshold = 30
	// defaultAutoResolveAfter is the ungraded-log auto-advance delay
	// (D-17: 48h で good 相当の自動前進).
	defaultAutoResolveAfter = 48 * time.Hour
)

// Config holds the learning-loop parameters (D-18/D-17).
type Config struct {
	// ItemsPerDay is M, the number of new items generated per broadcast
	// (QUIZ_ITEMS_PER_DAY).
	ItemsPerDay int
	// Ladder is the spaced-repetition interval ladder in days
	// (QUIZ_LADDER_DAYS, comma-separated positive ints).
	Ladder []int
	// Slots is S, the max quiz questions per episode (QUIZ_SLOTS).
	Slots int
	// BackpressureThreshold suspends new item generation while the count
	// of overdue active items exceeds it (QUIZ_BACKPRESSURE_THRESHOLD).
	BackpressureThreshold int
	// AutoResolveAfter is how long an ungraded review log waits before
	// auto-advancing as ResultAuto (QUIZ_AUTO_RESOLVE_AFTER).
	AutoResolveAfter time.Duration
}

// LoadConfig reads the learning-loop settings from environment variables:
//
//   - QUIZ_ITEMS_PER_DAY: 新規項目の1日あたり生成数 M (default 1, D-18)
//   - QUIZ_LADDER_DAYS: 間隔ラダー、カンマ区切りの日数 (default "1,7,30")
//   - QUIZ_SLOTS: 1エピソードの出題枠 S (default 4)
//   - QUIZ_BACKPRESSURE_THRESHOLD: 期日超過項目数の新規生成停止閾値 (default 30, §5.2)
//   - QUIZ_AUTO_RESOLVE_AFTER: 未採点ログの自動前進までの経過時間 (default 48h, D-17)
//
// Invalid values fall back to the defaults with a warning — a bad env must
// degrade, never stop the broadcast (縮退許容).
func LoadConfig(logger *slog.Logger) Config {
	if logger == nil {
		logger = slog.Default()
	}
	cfg := Config{
		ItemsPerDay:           pkgconfig.GetEnvInt("QUIZ_ITEMS_PER_DAY", defaultItemsPerDay),
		Ladder:                loadLadder(logger),
		Slots:                 pkgconfig.GetEnvInt("QUIZ_SLOTS", defaultSlots),
		BackpressureThreshold: pkgconfig.GetEnvInt("QUIZ_BACKPRESSURE_THRESHOLD", defaultBackpressureThreshold),
		AutoResolveAfter:      pkgconfig.GetEnvDuration("QUIZ_AUTO_RESOLVE_AFTER", defaultAutoResolveAfter),
	}
	if cfg.ItemsPerDay <= 0 {
		logger.Warn("QUIZ_ITEMS_PER_DAY must be positive, using default",
			slog.Int("value", cfg.ItemsPerDay), slog.Int("default", defaultItemsPerDay))
		cfg.ItemsPerDay = defaultItemsPerDay
	}
	if cfg.Slots <= 0 {
		logger.Warn("QUIZ_SLOTS must be positive, using default",
			slog.Int("value", cfg.Slots), slog.Int("default", defaultSlots))
		cfg.Slots = defaultSlots
	}
	if cfg.BackpressureThreshold <= 0 {
		logger.Warn("QUIZ_BACKPRESSURE_THRESHOLD must be positive, using default",
			slog.Int("value", cfg.BackpressureThreshold), slog.Int("default", defaultBackpressureThreshold))
		cfg.BackpressureThreshold = defaultBackpressureThreshold
	}
	if cfg.AutoResolveAfter <= 0 {
		logger.Warn("QUIZ_AUTO_RESOLVE_AFTER must be positive, using default",
			slog.Duration("value", cfg.AutoResolveAfter), slog.Duration("default", defaultAutoResolveAfter))
		cfg.AutoResolveAfter = defaultAutoResolveAfter
	}
	return cfg
}

// loadLadder parses QUIZ_LADDER_DAYS ("1,7,30"). Any malformed or
// non-positive entry rejects the whole value: a half-parsed ladder would
// silently reshape the review schedule.
func loadLadder(logger *slog.Logger) []int {
	raw := pkgconfig.GetEnvString("QUIZ_LADDER_DAYS", defaultLadderDays)
	ladder, err := parseLadder(raw)
	if err != nil {
		logger.Warn("invalid QUIZ_LADDER_DAYS, using default",
			slog.String("value", raw), slog.String("default", defaultLadderDays),
			slog.Any("error", err))
		ladder, _ = parseLadder(defaultLadderDays)
	}
	return ladder
}

func parseLadder(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	ladder := make([]int, 0, len(parts))
	for _, part := range parts {
		days, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		if days <= 0 {
			return nil, strconv.ErrRange
		}
		ladder = append(ladder, days)
	}
	return ladder, nil
}
