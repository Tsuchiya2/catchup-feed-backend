package learning

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// day is a test shorthand for a BroadcastDay-shaped date.
func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// TestTransition_Table drives the full §6.1 matrix with the D-18 ladder
// [1,7,30]: every result × every stage, plus the boundaries the design
// calls out (最終段の good=卒業、stage 0 の forgot、auto=good 同遷移).
func TestTransition_Table(t *testing.T) {
	ladder := []int{1, 7, 30}
	graded := day(2026, 7, 7)

	tests := []struct {
		name   string
		stage  int
		result string
		want   Next
	}{
		// --- good: stage+1, due = 採点日 + ladder[新 stage] ---
		{"good at stage 0 -> stage 1, +7d", 0, ResultGood,
			Next{Stage: 1, DueOn: day(2026, 7, 14)}},
		{"good at stage 1 -> stage 2, +30d", 1, ResultGood,
			Next{Stage: 2, DueOn: day(2026, 8, 6)}},
		{"good at final stage -> retired (卒業)", 2, ResultGood,
			Next{Stage: 3, DueOn: graded, Retired: true}},

		// --- auto: good と同一遷移 (D-17) ---
		{"auto at stage 0 -> stage 1, +7d", 0, ResultAuto,
			Next{Stage: 1, DueOn: day(2026, 7, 14)}},
		{"auto at stage 1 -> stage 2, +30d", 1, ResultAuto,
			Next{Stage: 2, DueOn: day(2026, 8, 6)}},
		{"auto at final stage -> retired (採点ゼロでも卒業)", 2, ResultAuto,
			Next{Stage: 3, DueOn: graded, Retired: true}},

		// --- fuzzy: stage 据え置き, due = 採点日 + ladder[stage] ---
		{"fuzzy at stage 0 stays, +1d", 0, ResultFuzzy,
			Next{Stage: 0, DueOn: day(2026, 7, 8)}},
		{"fuzzy at stage 1 stays, +7d", 1, ResultFuzzy,
			Next{Stage: 1, DueOn: day(2026, 7, 14)}},
		{"fuzzy at final stage stays, +30d (never retires)", 2, ResultFuzzy,
			Next{Stage: 2, DueOn: day(2026, 8, 6)}},

		// --- forgot: stage=0, due = 採点日 + 1日 (文字どおり翌日) ---
		{"forgot at stage 0 resets to itself, +1d", 0, ResultForgot,
			Next{Stage: 0, DueOn: day(2026, 7, 8)}},
		{"forgot at stage 1 -> stage 0, +1d", 1, ResultForgot,
			Next{Stage: 0, DueOn: day(2026, 7, 8)}},
		{"forgot at final stage -> stage 0, +1d (引き戻し)", 2, ResultForgot,
			Next{Stage: 0, DueOn: day(2026, 7, 8)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Transition(tt.stage, tt.result, graded, ladder)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestTransition_LadderIsParameterized pins that the ladder really is an
// argument (D-18: env で運用調整), not baked-in [1,7,30].
func TestTransition_LadderIsParameterized(t *testing.T) {
	graded := day(2026, 7, 7)

	// Two-rung ladder [2,5]: good at stage 0 lands +5d, good at stage 1
	// graduates.
	got, err := Transition(0, ResultGood, graded, []int{2, 5})
	require.NoError(t, err)
	assert.Equal(t, Next{Stage: 1, DueOn: day(2026, 7, 12)}, got)

	got, err = Transition(1, ResultGood, graded, []int{2, 5})
	require.NoError(t, err)
	assert.True(t, got.Retired)

	// Single-rung ladder: the very first good graduates.
	got, err = Transition(0, ResultGood, graded, []int{3})
	require.NoError(t, err)
	assert.Equal(t, Next{Stage: 1, DueOn: graded, Retired: true}, got)

	// forgot stays "+1 day" regardless of ladder[0] (§6.1 の表のとおり).
	got, err = Transition(0, ResultForgot, graded, []int{9, 9})
	require.NoError(t, err)
	assert.Equal(t, Next{Stage: 0, DueOn: day(2026, 7, 8)}, got)
}

// TestTransition_StageBeyondLadderClamps covers the env-shrunk-ladder case:
// an item at stage 2 with a shortened ladder [1,7] is treated as sitting on
// the last rung instead of erroring (縮退許容 — the system heals itself).
func TestTransition_StageBeyondLadderClamps(t *testing.T) {
	graded := day(2026, 7, 7)
	ladder := []int{1, 7}

	// good from the (clamped) last rung graduates.
	got, err := Transition(5, ResultGood, graded, ladder)
	require.NoError(t, err)
	assert.True(t, got.Retired)

	// fuzzy holds at the clamped last rung.
	got, err = Transition(5, ResultFuzzy, graded, ladder)
	require.NoError(t, err)
	assert.Equal(t, Next{Stage: 1, DueOn: day(2026, 7, 14)}, got)
}

func TestTransition_Errors(t *testing.T) {
	graded := day(2026, 7, 7)
	ladder := []int{1, 7, 30}

	tests := []struct {
		name   string
		stage  int
		result string
		ladder []int
	}{
		{"unknown result", 0, "meh", ladder},
		{"empty result (NULL は遷移対象外)", 0, "", ladder},
		{"negative stage", -1, ResultGood, ladder},
		{"empty ladder", 0, ResultGood, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Transition(tt.stage, tt.result, graded, tt.ladder)
			assert.Error(t, err)
		})
	}
}
