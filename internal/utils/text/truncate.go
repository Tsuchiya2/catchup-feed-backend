package text

import "unicode/utf8"

// TruncateBytes returns s limited to at most limit bytes. The cut is backed
// off to a rune boundary so multi-byte text (Japanese article bodies) is
// never split into invalid UTF-8. The bool reports whether anything was cut.
//
// A non-positive limit means "no limit" and returns s unchanged.
//
// This is the byte-budget flavour used to keep an article body under a
// provider's input ceiling (internal/infra/summarizer: maxInputChars).
func TruncateBytes(s string, limit int) (string, bool) {
	if limit <= 0 || len(s) <= limit {
		return s, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// TruncateRunes returns s limited to at most limit Unicode characters
// (runes). The bool reports whether anything was cut.
//
// A non-positive limit means "no limit" and returns s unchanged.
//
// This is the character-budget flavour used where the budget is expressed in
// 文字数 rather than bytes — e.g. the summaries embedded in the outro prompt's
// quiz section, which must fit a free-tier TPM ceiling (D-46 (1)). Counting
// runes keeps the knob meaningful for Japanese text, where one character is
// three bytes.
func TruncateRunes(s string, limit int) (string, bool) {
	if limit <= 0 {
		return s, false
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s, false
	}
	return string(runes[:limit]), true
}
