package learning

// WeeklyReview is the material for the §7.4 週次振り返り segment, read entirely
// from the DB (no LLM — §7.4). It is private-episode content only (§10): the
// concepts and counts must never reach the public feed, a notification, or a
// cloud prompt.
type WeeklyReview struct {
	// Concepts are the concept headings of learning_items created inside the
	// look-back window (WeeklyReviewWindowStart), oldest first. Any kind —
	// article and book items alike (book concepts stay private-only, §10).
	Concepts []string
	// GraduatedCount is how many items were retired via ladder completion
	// inside the window (「学びの成果」). It counts only ladder graduations,
	// not manual archives — see the repository query for how the two are
	// distinguished (stage >= ladder length).
	GraduatedCount int
	// Reintroduced is the concept of one item pulled back to stage 0 by a
	// 'forgot' grade inside the window (§7.4: 忘れて引き戻された項目の再紹介、
	// あれば1件); "" when none.
	Reintroduced string
}

// IsEmpty reports whether there is nothing to say this week — no items
// learned, none graduated, none pulled back. An empty week skips the segment
// entirely (§7.4: 空の振り返りを作らない).
func (w WeeklyReview) IsEmpty() bool {
	return len(w.Concepts) == 0 && w.GraduatedCount == 0 && w.Reintroduced == ""
}
