// Package learning is the Phase 3 learning-loop core (設計書
// docs/pulse-phase3-design.md): the spaced-repetition transition function
// (§6.1), the broadcast-day date helpers (§12-10), the learning-item
// domain types (§4) and the quiz parameters (D-18).
//
// This package is shared by the radio batch (item generation, quiz
// selection, auto-resolve) and the server (grading API). It therefore
// depends on nothing but the standard library and pkg/config — never on
// radio, server, handlers or repositories.
package learning

import "time"

// jst is the broadcast-day timezone. asked_on / due_on mean "the JST
// broadcast day" by definition (§4), independent of host or DB timezone —
// the Pi's PostgreSQL runs in UTC and a naive-datetime timestamptz shift
// has bitten this project before (§12-10). JST has no DST, so a fixed zone
// is exact and needs no tzdata on the host.
var jst = time.FixedZone("JST", 9*60*60)

// BroadcastDay returns the JST calendar date of now, normalized to
// midnight UTC. This is THE date-extraction helper (§12-10: 日付切り出しを
// 1箇所に集約) — asked_on / due_on values must originate here.
//
// Why midnight UTC: database/sql with the pgx driver scans a PostgreSQL
// `date` column into a time.Time at 00:00 UTC. Producing the same shape
// makes dates round-trip and compare with Equal. When binding a day to SQL,
// use FormatDay (text + ::date cast) instead of passing the time.Time, so
// no driver timezone conversion can move the date.
func BroadcastDay(now time.Time) time.Time {
	j := now.In(jst)
	return time.Date(j.Year(), j.Month(), j.Day(), 0, 0, 0, 0, time.UTC)
}

// FirstDueDay is the initial due_on of a freshly generated item: the day
// after its broadcast day (§5.1: 当日のクイズコーナーには出さない、初回
// 想起は翌日).
func FirstDueDay(now time.Time) time.Time {
	return BroadcastDay(now).AddDate(0, 0, 1)
}

// weeklyReviewWindowDays is the §7.4 look-back: the review summarizes the 7
// broadcast days ending on (and including) the review day. Weekly cadence and
// a 7-day window tile the calendar exactly — no week is summarized twice.
const weeklyReviewWindowDays = 7

// BroadcastWeekday is the JST weekday of the broadcast day (§7.4/D-21 gating).
// Derived from BroadcastDay so it is immune to the host/DB timezone the same
// way asked_on / due_on are (§12-10).
func BroadcastWeekday(now time.Time) time.Weekday {
	return BroadcastDay(now).Weekday()
}

// WeeklyReviewWindowStart is the inclusive first day of the §7.4 look-back for
// a review broadcast on now: BroadcastDay(now) minus 6 days, so the window is
// the 7 days [start, broadcastDay]. Returned as a BroadcastDay-shaped value
// (midnight UTC) for FormatDay binding.
func WeeklyReviewWindowStart(now time.Time) time.Time {
	return BroadcastDay(now).AddDate(0, 0, -(weeklyReviewWindowDays - 1))
}

// FormatDay renders a day (as returned by BroadcastDay) for SQL binding.
// Repositories pass this string and cast with ::date, which is immune to
// driver-side timezone interpretation.
func FormatDay(day time.Time) string {
	return day.Format("2006-01-02")
}
