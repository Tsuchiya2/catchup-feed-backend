package script

import (
	"log/slog"
	"strconv"
	"strings"

	"catchup-feed/internal/repository"
)

// quizSectionMarker separates the outro script from the learning-item
// section inside the piggybacked LLM output (D-19). The marker style
// follows the Phase 2 gemini_video precedent: JSON was rejected there
// because models routinely break string escaping, while a unique marker
// line fails loudly and lets the caller degrade (progress 2026-07-06).
//
// Everything before the FIRST exact occurrence is the broadcast outro —
// but this split alone does not keep quiz text out of the public episode:
// a model may deviate from the exact marker (whitespace inside it, or
// omitting it and writing the item lines directly — フォールバック末端の
// Ollama で最も起きやすい逸脱). stripQuizLeak is the mandatory second net
// that truncates the body at any surviving learning-item trace; the §12-1
// guarantee (公開エピソードにクイズを載せない) holds only through the two
// combined.
const quizSectionMarker = "===LEARNING_ITEMS==="

// QuizDraft is one learning-item candidate recovered from the outro
// output (Phase 3 §5.1): the source article plus the three read-aloud
// fields. Provider is the LLM that actually answered the piggybacked call
// — it travels with the draft because the fallback chain decides the
// provider per call, not per run.
type QuizDraft struct {
	ArticleID int64
	Concept   string // 1行見出し
	Question  string // ラジオ口調のクイズ文
	Answer    string // 答え+一言解説
	Provider  string
}

// cutQuizSection splits the raw outro output at the first marker line.
// found=false means the model ignored the learning-item instructions —
// the caller keeps the whole text as the outro and skips today's item
// generation (§5.1 縮退: クイズなしに倒して放送を止めない).
func cutQuizSection(out string) (body, section string, found bool) {
	i := strings.Index(out, quizSectionMarker)
	if i < 0 {
		return out, "", false
	}
	return out[:i], out[i+len(quizSectionMarker):], true
}

// quizLeakTokenNormalized is the marker's distinctive core after
// normalizeLeakChars: stripQuizLeak matches it against the alphanumerics of
// each line, so every marker mangling that keeps the letters — whitespace
// inside ("=== LEARNING_ITEMS ==="), a dropped underscore ("LEARNING
// ITEMS"), decorations — is still caught.
const quizLeakTokenNormalized = "LEARNINGITEMS"

// quizLeakLabels are the item labels whose cutLabel match truncates the
// body: any line the parser could read as an item field must never be read
// on air, not just the leading 記事番号 line (a model may omit it and start
// a bare item at 概念/問題/答え).
var quizLeakLabels = [...]string{"記事番号", "概念", "問題", "答え"}

// stripQuizLeak truncates the broadcast body at the start of the first LINE
// carrying a trace of a learning-item section that survived the marker
// split (§12-1 の安全ネット): (1) a line whose normalized alphanumerics
// contain the marker core — any mangled marker variant — or (2) a line
// that, after trimming, matches one of the quizLeakLabels with the same
// cutLabel match the parser uses (パーサが項目と読める行を放送原稿が
// 読み上げることはない). Cutting at the line start (not the token index)
// keeps mangled-marker fragments like a leading "===" out of the body.
//
// A false positive costs at most a truncated outro plus a warning; a false
// negative would read quiz text on the public feed (友人にも配信) — so
// this errs toward cutting. Truncating to nothing is the caller's problem:
// it becomes the empty-script error, i.e. 当日スキップ (§8).
func stripQuizLeak(body string) (string, bool) {
	offset := 0
	for _, line := range strings.Split(body, "\n") {
		if isQuizLeakLine(line) {
			return body[:offset], true
		}
		offset += len(line) + 1
	}
	return body, false
}

// isQuizLeakLine reports whether a single body line is a learning-item
// trace (see stripQuizLeak).
func isQuizLeakLine(line string) bool {
	if strings.Contains(normalizeLeakChars(line), quizLeakTokenNormalized) {
		return true
	}
	trimmed := strings.TrimSpace(line)
	for _, label := range quizLeakLabels {
		if _, ok := cutLabel(trimmed, label); ok {
			return true
		}
	}
	return false
}

// normalizeLeakChars reduces a line to its uppercased ASCII alphanumerics,
// making the marker-core match immune to separators, casing and spacing.
func normalizeLeakChars(line string) string {
	var sb strings.Builder
	for _, r := range line {
		switch {
		case r >= 'a' && r <= 'z':
			sb.WriteRune(r - ('a' - 'A'))
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// parseQuizItems extracts up to max drafts from the learning-item section.
// It is deliberately forgiving — full-width colons, wrapped lines and
// preamble noise are absorbed, malformed blocks are skipped with a warning
// — because every parse failure here only costs the day's items, never the
// broadcast (§5.1/§9). The 記事番号 maps 1-based into articles (the on-air
// order the prompt numbered); out-of-range or repeated numbers are dropped,
// and anything beyond max is discarded to keep the §6.2 saturation
// arithmetic (負荷 = M×ラダー段数) intact no matter what the model emits.
func parseQuizItems(section string, articles []repository.RadioArticle, max int, provider string, logger *slog.Logger) []QuizDraft {
	type block struct {
		number   string
		concept  string
		question string
		answer   string
	}
	var drafts []QuizDraft
	used := make(map[int]bool)
	skipped := 0
	var cur *block
	var curField *string

	flush := func() {
		if cur == nil {
			return
		}
		b := *cur
		cur, curField = nil, nil
		n, err := strconv.Atoi(strings.TrimSpace(b.number))
		if err != nil || n < 1 || n > len(articles) {
			logger.Warn("learning item block has an invalid article number, skipped",
				slog.String("number", b.number))
			skipped++
			return
		}
		if b.concept == "" || b.question == "" || b.answer == "" {
			logger.Warn("learning item block has empty fields, skipped",
				slog.Int("article_number", n))
			skipped++
			return
		}
		if used[n] {
			logger.Warn("learning item block repeats an article, skipped",
				slog.Int("article_number", n))
			skipped++
			return
		}
		if len(drafts) >= max {
			skipped++
			return
		}
		used[n] = true
		drafts = append(drafts, QuizDraft{
			ArticleID: articles[n-1].ID,
			Concept:   b.concept,
			Question:  b.question,
			Answer:    b.answer,
			Provider:  provider,
		})
	}

	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, quizSectionMarker) {
			// 空行と、逸脱応答が重複させたマーカー行は読み上げ原稿に
			// 混ぜない。
			continue
		}
		if v, ok := cutLabel(line, "記事番号"); ok {
			flush()
			cur = &block{number: v}
			curField = &cur.number
			continue
		}
		if cur == nil {
			continue // マーカー直後の前置きなどは無視
		}
		if v, ok := cutLabel(line, "概念"); ok {
			cur.concept = v
			curField = &cur.concept
			continue
		}
		if v, ok := cutLabel(line, "問題"); ok {
			cur.question = v
			curField = &cur.question
			continue
		}
		if v, ok := cutLabel(line, "答え"); ok {
			cur.answer = v
			curField = &cur.answer
			continue
		}
		// ラベルのない行はモデルの折り返しとみなして直前のフィールドに
		// 連結する(読み上げ原稿なので改行連結で問題ない)。
		if curField != nil {
			if *curField == "" {
				*curField = line
			} else {
				*curField += "\n" + line
			}
		}
	}
	flush()

	if skipped > 0 {
		logger.Warn("some learning item blocks were dropped",
			slog.Int("skipped", skipped), slog.Int("kept", len(drafts)))
	}
	return drafts
}

// cutLabel matches a "ラベル: 値" line, tolerating a full-width colon and
// spaces around the separator (LLM 出力の揺れの吸収).
func cutLabel(line, label string) (string, bool) {
	rest, ok := strings.CutPrefix(line, label)
	if !ok {
		return "", false
	}
	rest = strings.TrimLeft(rest, " 　\t")    // ASCII と全角スペース両方
	for _, sep := range []string{":", "："} { // ASCII コロンと全角コロン(LLM 出力の揺れ)
		if v, ok := strings.CutPrefix(rest, sep); ok {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}
