package learning

import (
	"fmt"
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
	// defaultWeeklyReviewDOW is the weekly-review broadcast day (D-21: 土曜).
	// Go's time.Weekday numbering (Sunday=0 … Saturday=6).
	defaultWeeklyReviewDOW = time.Saturday
)

// Config holds the learning-loop parameters (D-18/D-17).
type Config struct {
	// ItemsPerDay is M, the number of new items generated per broadcast
	// (QUIZ_ITEMS_PER_DAY). 0 is a valid off switch (D-26 (3)): item
	// generation is disabled and the outro prompt carries no piggybacked
	// quiz section at all.
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
	// WeeklyReviewDOW is the JST broadcast weekday the §7.4 週次振り返り
	// segment is injected on (QUIZ_WEEKLY_REVIEW_DOW, default Saturday /
	// D-21). Compared against learning.BroadcastWeekday(now).
	WeeklyReviewDOW time.Weekday
}

// LoadConfig reads the learning-loop settings from environment variables:
//
//   - QUIZ_ITEMS_PER_DAY: 新規項目の1日あたり生成数 M (default 1, D-18)。
//     0 は正当な off 値 — クイズ生成無効、アウトロへの相乗りセクション
//     なし (D-26 (3))。負値のみ警告してデフォルトに戻す
//   - QUIZ_LADDER_DAYS: 間隔ラダー、カンマ区切りの日数 (default "1,7,30")
//   - QUIZ_SLOTS: 1エピソードの出題枠 S (default 4)
//   - QUIZ_BACKPRESSURE_THRESHOLD: 期日超過項目数の新規生成停止閾値 (default 30, §5.2)
//   - QUIZ_AUTO_RESOLVE_AFTER: 未採点ログの自動前進までの経過時間 (default 48h, D-17)
//   - QUIZ_WEEKLY_REVIEW_DOW: 週次振り返りの曜日 (default "saturday", D-21)。
//     曜日名(英語、大小無視)または Go の time.Weekday 番号 0=日〜6=土
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
		WeeklyReviewDOW:       loadWeeklyReviewDOW(logger),
	}
	if cfg.ItemsPerDay < 0 {
		logger.Warn("QUIZ_ITEMS_PER_DAY must not be negative (0 disables item generation), using default",
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

// weekdayNames maps lowercase English weekday names to time.Weekday. Both a
// name and a Go weekday number (0=Sunday … 6=Saturday) are accepted so the
// deploy env can read either "saturday" or "6".
var weekdayNames = map[string]time.Weekday{
	"sunday": time.Sunday, "monday": time.Monday, "tuesday": time.Tuesday,
	"wednesday": time.Wednesday, "thursday": time.Thursday,
	"friday": time.Friday, "saturday": time.Saturday,
}

// loadWeeklyReviewDOW parses QUIZ_WEEKLY_REVIEW_DOW. An empty or malformed
// value falls back to the default (Saturday, D-21): a bad weekday must never
// stop the broadcast — worst case the 週次振り返り lands on the default day.
func loadWeeklyReviewDOW(logger *slog.Logger) time.Weekday {
	raw := pkgconfig.GetEnvString("QUIZ_WEEKLY_REVIEW_DOW", "")
	if raw == "" {
		return defaultWeeklyReviewDOW
	}
	dow, err := parseWeekday(raw)
	if err != nil {
		logger.Warn("invalid QUIZ_WEEKLY_REVIEW_DOW, using default",
			slog.String("value", raw), slog.String("default", defaultWeeklyReviewDOW.String()),
			slog.Any("error", err))
		return defaultWeeklyReviewDOW
	}
	return dow
}

func parseWeekday(raw string) (time.Weekday, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if dow, ok := weekdayNames[s]; ok {
		return dow, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n < int(time.Sunday) || n > int(time.Saturday) {
			return 0, fmt.Errorf("weekday number %d out of range 0..6", n)
		}
		return time.Weekday(n), nil
	}
	return 0, fmt.Errorf("unrecognized weekday %q", raw)
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
