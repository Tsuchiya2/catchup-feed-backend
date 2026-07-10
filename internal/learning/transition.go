package learning

import (
	"fmt"
	"time"
)

// Grading results (review_logs.result, §4 / §6.1). NULL (未採点) has no
// constant — it is the absence of a result.
const (
	ResultGood   = "good"   // ○ わかった
	ResultFuzzy  = "fuzzy"  // △ あいまい
	ResultForgot = "forgot" // × 忘れた
	// ResultAuto is the 48h auto-advance for ungraded logs (D-17). It
	// transitions exactly like ResultGood; only the recorded result string
	// differs, so the tracker can tell self-graded from auto-drained.
	// graded_at stays NULL for auto resolutions (§4).
	ResultAuto = "auto"
)

// Next is the outcome of applying one grading result to an item (§6.1).
type Next struct {
	// Stage is the new ladder stage. On retirement it equals len(ladder)
	// (the number of completed rungs); it is never read again once the
	// item is retired.
	Stage int
	// DueOn is the next asking day (JST broadcast day, see BroadcastDay).
	// On retirement it is the grading day itself — inert, because retired
	// items are excluded from selection, but kept deterministic so the
	// UPDATE can write stage/due_on unconditionally.
	DueOn time.Time
	// Retired reports ladder completion (卒業): the caller sets
	// learning_items.retired_at.
	Retired bool
}

// Transition is THE spaced-repetition state transition (§6.1) — a pure
// function shared by the server grading API (manual ○△×) and the radio
// batch (48h auto-resolve). Neither side may reimplement it.
//
//	good   : stage+1, due = gradedOn + ladder[新 stage]日。最終段完走で卒業
//	fuzzy  : stage 据え置き, due = gradedOn + ladder[stage]日
//	forgot : stage=0, due = gradedOn + 1日(ladder[0] ではなく文字どおり翌日)
//	auto   : good と同一遷移(D-17)
//
// stage is the item's current stage, gradedOn the grading/resolution day
// (a BroadcastDay value), ladder the interval ladder in days (D-18 initial
// [1, 7, 30], parameterized via Config).
//
// A stage beyond the ladder (possible after shortening QUIZ_LADDER_DAYS in
// env) is clamped to the last rung instead of erroring: the system heals
// itself on the next grading (縮退許容). A negative stage or an empty
// ladder is data corruption and returns an error.
func Transition(stage int, result string, gradedOn time.Time, ladder []int) (Next, error) {
	if len(ladder) == 0 {
		return Next{}, fmt.Errorf("learning: transition: empty ladder")
	}
	if stage < 0 {
		return Next{}, fmt.Errorf("learning: transition: negative stage %d", stage)
	}
	if stage > len(ladder)-1 {
		stage = len(ladder) - 1
	}

	switch result {
	case ResultGood, ResultAuto:
		next := stage + 1
		if next >= len(ladder) {
			return Next{Stage: next, DueOn: gradedOn, Retired: true}, nil
		}
		return Next{Stage: next, DueOn: gradedOn.AddDate(0, 0, ladder[next])}, nil
	case ResultFuzzy:
		return Next{Stage: stage, DueOn: gradedOn.AddDate(0, 0, ladder[stage])}, nil
	case ResultForgot:
		return Next{Stage: 0, DueOn: gradedOn.AddDate(0, 0, 1)}, nil
	default:
		return Next{}, fmt.Errorf("learning: transition: unknown result %q", result)
	}
}
