package script

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/learning"
)

// BuildWeeklyReview renders the §7.4 週次振り返り read-aloud script from the
// DB-sourced material. It is TEMPLATE-ONLY — no LLM call (§7.4: クオータ消費
// ゼロ、§10 により学習内容はクラウドに送れない). ok is false for an empty
// week (§7.4: 空の振り返りを作らない); the caller then omits the segment.
//
// The sentences are assembled adaptively so the line reads naturally whichever
// of the three materials is present:
//
//	lead → (concepts) → (graduations) → (reintroduction) → closing
//
// 文言はすべて format.go にあり、このファイルは組み立て順だけを持つ
// (D-37 (9))。
//
// Future work could feed the same material to a local LLM for a livelier
// script (§7.4: 品質不満が出たら LLM 化を検討) — deliberately NOT done here;
// the口 stays a plain template until quality demands otherwise.
func BuildWeeklyReview(ctx context.Context, m learning.WeeklyReview, logger *slog.Logger) (string, bool) {
	if m.IsEmpty() {
		return "", false
	}
	var sb strings.Builder
	sb.WriteString(weeklyReviewLead)

	hasConcepts := len(m.Concepts) > 0
	if hasConcepts {
		sb.WriteString(weeklyReviewConcepts(m.Concepts))
	}
	if m.GraduatedCount > 0 {
		sb.WriteString(weeklyReviewGraduated(m.GraduatedCount, hasConcepts))
	}
	if m.Reintroduced != "" {
		sb.WriteString(weeklyReviewReintroduced(m.Reintroduced, hasConcepts || m.GraduatedCount > 0))
	}

	sb.WriteString(weeklyReviewClosing)
	// D-41 改訂: 固定文は format.go 由来で安全だが、concept は学習項目由来
	// (生成元はクラウドモデル)なので、組み立て後の台本にサニタイズを掛ける。
	// 固定文はそれぞれ独立した文なので、落ちうるのは concept を含む文だけ。
	return sanitizeSegmentScript(ctx, logger, entity.SegmentKindReview, sb.String()), true
}

// AppendWeeklyReviewShowNotes appends the §7.5 週次振り返りセクション to the
// PRIVATE episode's show notes: the week's concepts and the graduation count
// as text (§7.4: ショーノートにも同じ内容). Empty material leaves the notes
// unchanged. PRIVATE only — never call this for the public episode (§10).
func AppendWeeklyReviewShowNotes(notes string, m learning.WeeklyReview) string {
	if m.IsEmpty() {
		return notes
	}
	var sb strings.Builder
	sb.WriteString(notes)
	sb.WriteString(weeklyNotesHeading)
	for _, concept := range m.Concepts {
		sb.WriteString("- ")
		sb.WriteString(concept)
		sb.WriteString("\n")
	}
	fmt.Fprintf(&sb, weeklyNotesGraduatedFormat, m.GraduatedCount)
	if m.Reintroduced != "" {
		sb.WriteString(weeklyNotesReintroducePrefix)
		sb.WriteString(m.Reintroduced)
	}
	return sb.String()
}
