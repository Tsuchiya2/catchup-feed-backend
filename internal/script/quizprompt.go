package script

import (
	"log/slog"

	"catchup-feed/internal/repository"
	utiltext "catchup-feed/internal/utils/text"
	pkgconfig "catchup-feed/pkg/config"
)

// D-46 (1) の既定値。アウトロのクイズ相乗りセクション(D-19)は当初「その日の
// 全記事のタイトルと要約を丸ごと」埋め込んでいた。8記事 × 要約
// SUMMARIZER_CHAR_LIMIT(既定 900文字)= 約 7,200文字 + ペルソナ + 指示で
// 実測 8,873 トークン要求になり、D-41(2026-08-15)で Groq 無料枠の TPM が
// 12,000 → 8,000 に下がって以降、**8記事日のアウトロは構造的に Groq に
// 載らない**(413 Request too large。429 と違い待っても通らない — 1リクエスト
// が上限そのものを超えているため)。2026-09-25 の欠番がこれである。
//
// そこで相乗りセクションに渡す記事を上位N件に絞り、要約を文字数で切り詰める。
// N=4 / 300文字 の根拠:
//
//   - 8,000 TPM は「1分間の rolling window」なので、アウトロ1本が上限に
//     収まるだけでは足りない。同じ分に先行するニュース段(1本あたり
//     ペルソナ+要約で約 1,700 トークン)が窓を食っている前提で、アウトロ
//     単体は上限の半分以下に収める。
//   - 4件 × (タイトル約60 + 要約300 + ラベル約20) = 約 1,520文字。ペルソナ
//     (約700文字)と指示(約800文字)を足して約 3,000文字 ≒ 3,000トークン
//     (実測の 8,873 トークン / 約9,100文字 から日本語は概ね1文字1トークン)。
//     gpt-oss-120b の reasoning を含む出力見込み(〜1,500)を足しても 8,000 に
//     対して倍以上の余裕がある。
//   - N=4 は既定 QUIZ_ITEMS_PER_DAY=1(D-18 の M)の4倍で、「技術的な学びが
//     最も大きい記事を選ばせる」余地を残す最小線。クイズ候補が8件から4件に
//     減るのは許容する — 放送継続がクイズ品質より優先する(原則2 縮退許容)。
//   - 要約300文字は既定900文字の3分の1。要約は要点から書き出されるため、
//     クイズ1問の素材としては前半で足りる。
const (
	defaultQuizPromptArticles     = 4
	defaultQuizPromptSummaryChars = 300
)

// quizPromptTruncationMark は切り詰めた要約の末尾に付ける記号。「ここで
// 途切れている」ことをモデルに示し、途中で切れた文を事実として補完させない
// ためのもの。放送原稿に出る文言ではないので format.go の対象外 (D-37)。
const quizPromptTruncationMark = "…"

// OutroQuizLimits bounds the piggybacked quiz section of the outro prompt
// (D-46 (1)). Zero values fall back to the built-in defaults, so a partially
// filled struct still behaves sanely (summarizer.Options と同じ流儀)。
type OutroQuizLimits struct {
	// MaxArticles is N — how many of the day's articles are presented to the
	// model as quiz candidates (QUIZ_PROMPT_MAX_ARTICLES).
	MaxArticles int

	// SummaryChars caps each presented summary in Unicode characters
	// (QUIZ_PROMPT_SUMMARY_CHARS).
	SummaryChars int
}

// DefaultOutroQuizLimits returns the built-in D-46 (1) defaults (4 件 / 300文字).
func DefaultOutroQuizLimits() OutroQuizLimits {
	return OutroQuizLimits{
		MaxArticles:  defaultQuizPromptArticles,
		SummaryChars: defaultQuizPromptSummaryChars,
	}
}

// withDefaults fills zero-valued fields with the built-in defaults.
func (l OutroQuizLimits) withDefaults() OutroQuizLimits {
	if l.MaxArticles <= 0 {
		l.MaxArticles = defaultQuizPromptArticles
	}
	if l.SummaryChars <= 0 {
		l.SummaryChars = defaultQuizPromptSummaryChars
	}
	return l
}

// LoadOutroQuizLimits reads the D-46 (1) prompt-size knobs from the
// environment:
//
//   - QUIZ_PROMPT_MAX_ARTICLES: クイズ候補として渡す記事数 N (default 4)
//   - QUIZ_PROMPT_SUMMARY_CHARS: 1件あたりの要約の文字数上限 (default 300)
//
// Non-positive or unparsable values fall back to the defaults with a warning:
// a bad tuning knob must degrade, never stop the broadcast (原則2).
func LoadOutroQuizLimits(logger *slog.Logger) OutroQuizLimits {
	if logger == nil {
		logger = slog.Default()
	}
	limits := OutroQuizLimits{
		MaxArticles:  pkgconfig.GetEnvInt("QUIZ_PROMPT_MAX_ARTICLES", defaultQuizPromptArticles),
		SummaryChars: pkgconfig.GetEnvInt("QUIZ_PROMPT_SUMMARY_CHARS", defaultQuizPromptSummaryChars),
	}
	if limits.MaxArticles <= 0 {
		logger.Warn("QUIZ_PROMPT_MAX_ARTICLES must be positive, using default",
			slog.Int("value", limits.MaxArticles), slog.Int("default", defaultQuizPromptArticles))
		limits.MaxArticles = defaultQuizPromptArticles
	}
	if limits.SummaryChars <= 0 {
		logger.Warn("QUIZ_PROMPT_SUMMARY_CHARS must be positive, using default",
			slog.Int("value", limits.SummaryChars), slog.Int("default", defaultQuizPromptSummaryChars))
		limits.SummaryChars = defaultQuizPromptSummaryChars
	}
	return limits
}

// quizPromptScope returns the slice of the day's articles presented to the
// model as quiz candidates (D-46 (1)). count <= 0 (QUIZ_ITEMS_PER_DAY=0 や
// バックプレッシャ、D-26 (3))returns nil: セクション自体を出さない。
//
// 先頭からN件 = 放送順の上位N件。Plan がカテゴリ順・公開日時順に並べた結果の
// 先頭であり、番組の主題に近い記事が前に来る (§6-1)。
//
// N が count を下回ることは許さない: 「M件選べ」と指示しながら候補を M 件
// 未満しか見せないプロンプトは自己矛盾で、モデルを逸脱(= 放送本文の破綻、
// D-26 (1) の再試行経路)へ誘う。この場合は count 件まで広げる。
func quizPromptScope(articles []repository.RadioArticle, count int, limits OutroQuizLimits) []repository.RadioArticle {
	if count <= 0 || len(articles) == 0 {
		return nil
	}
	limits = limits.withDefaults()
	n := limits.MaxArticles
	if n < count {
		n = count
	}
	if n > len(articles) {
		n = len(articles)
	}
	return articles[:n]
}

// quizPrompt builds the learning-item section data for the outro prompt from
// the already-narrowed scope (quizPromptScope). count <= 0 returns nil, which
// renders outro.tmpl exactly as before the Phase 3 extension — this nil is the
// backpressure/duplicate-guard switch (§5.2: プロンプト側で抑止、トークンも
// 消費しない).
//
// Numbers are 1-based over the SCOPE, and the parser is handed the same scope,
// so 記事番号 → article ID の対応はそのまま保たれる(§5.1)。
func quizPrompt(scope []repository.RadioArticle, count int, limits OutroQuizLimits) *quizPromptData {
	if count <= 0 || len(scope) == 0 {
		return nil
	}
	limits = limits.withDefaults()
	entries := make([]quizPromptArticle, len(scope))
	for i, a := range scope {
		summary, truncated := utiltext.TruncateRunes(a.Summary, limits.SummaryChars)
		if truncated {
			summary += quizPromptTruncationMark
		}
		entries[i] = quizPromptArticle{Number: i + 1, Title: a.Title, Summary: summary}
	}
	return &quizPromptData{Count: count, Marker: quizSectionMarker, Articles: entries}
}
