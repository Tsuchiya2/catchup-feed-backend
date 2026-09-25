package script

import (
	"log/slog"

	"catchup-feed/internal/repository"
	utiltext "catchup-feed/internal/utils/text"
	pkgconfig "catchup-feed/pkg/config"
)

// D-46 (1) のプロンプトサイズ制御。アウトロのクイズ相乗りセクション(D-19)は
// 当初「その日の全記事のタイトルと要約を丸ごと」埋め込んでいた。8記事 × 要約
// SUMMARIZER_CHAR_LIMIT(既定 900文字)で実測 8,873 トークン要求になり、
// D-41(2026-08-15)で Groq 無料枠の TPM が 12,000 → 8,000 に下がって以降、
// **8記事日のアウトロは構造的に Groq に載らない**(413 Request too large。
// 429 と違って待っても通らない — 1リクエストが上限そのものを超えているため)。
// 2026-09-25 の欠番がこれである。
//
// **絞るのは要約の長さだけで、記事は絞らない**(2026-09-25 のレビューで改訂)。
// 当初実装は「放送順の上位N件」に絞っていたが、plan.Plan() は featured を
// 「Category 昇順(スラッグの辞書順)→ PublishedAt 昇順 → ID 昇順」に並べ替える
// ため、先頭N件は「主題に近い記事」ではなく**辞書順で最初のカテゴリの古い記事**
// だった。8記事日に N=4 だと後ろのコーナーが構造的に一度も学習項目にならない。
// 編集判断(どの記事が学びとして大きいか)の対象は当日の全コーナーに保つ
// (Phase 3 §5.1)。
const (
	// defaultQuizPromptSummaryChars は1件あたりの要約の文字数上限。
	//
	// 150文字の根拠: 8記事 × 150文字で相乗りセクションが約1,900文字、
	// アウトロプロンプト全体で約2,500トークン = TPM 8,000 の3割。要約は要点
	// から書き出されるため、クイズ1問の素材としては冒頭150文字で足りる
	// (既定の要約は900文字)。クイズ品質より放送継続を優先する
	// (CLAUDE.md 設計原則2 縮退許容)。
	defaultQuizPromptSummaryChars = 150

	// quizPromptListBudgetChars は候補一覧(タイトル + 要約 + ラベル)全体の
	// 文字数予算。**RADIO_MAX_ARTICLES を大きく上げた日の安全弁**であり、
	// 通常運転(既定8記事 × 150文字 ≒ 1,900文字)では一切効かない。
	//
	// 超過した日は**記事を落とさず要約の文字数上限を縮めて**全記事を保つ —
	// 記事を落とすと上に書いた「辞書順で後ろのコーナーが出題されない」構造が
	// 戻ってしまう。2,800文字は、これにアウトロ本体+クイズ指示(約1,300文字)を
	// 足しても見積り4,000トークン(TPM 8,000 の半分)に収まる水準。
	quizPromptListBudgetChars = 2800

	// minQuizPromptSummaryChars は予算圧縮の下限。これ以下に縮めてもクイズの
	// 素材にならない。**下限でも予算に収まらない日は相乗りセクションを出さない**
	// (quizPrompt が nil を返す = D-26 (3) / §5.2 と同じ既存の縮退経路)。
	//
	// 「縮めるだけ縮めて、収まらなくてもそのまま投げる」would not be a safety
	// valve at all: 例えば RADIO_MAX_ARTICLES=200(config.go は <= 0 しか弾か
	// ないので通る)だと、タイトルとラベルだけで 10,400文字あり、下限40文字の
	// 要約を足すと 18,000文字超のプロンプトになる。Groq は確定で 413、Gemini が
	// 枯れていれば Ollama 頼み = **2026-09-25 の欠番と同じ失敗モードの再現**。
	// クイズ1項目を諦めて放送を出すのが D-46 の目的そのものである。
	minQuizPromptSummaryChars = 40

	// quizPromptEntryLabelChars は1件あたりのラベル・改行の概算文字数
	// (「記事N: 」+ 改行 + 「要約: 」+ 改行)。予算計算にだけ使う概算値。
	quizPromptEntryLabelChars = 12
)

// quizPromptTruncationMark は切り詰めた要約の末尾に付ける記号。「ここで
// 途切れている」ことをモデルに示し、途中で切れた文を事実として補完させない
// ためのもの。放送原稿に出る文言ではないので format.go の対象外 (D-37)。
const quizPromptTruncationMark = "…"

// OutroQuizLimits bounds the piggybacked quiz section of the outro prompt
// (D-46 (1)). Zero values fall back to the built-in defaults, so a partially
// filled struct still behaves sanely (summarizer.Options と同じ流儀)。
type OutroQuizLimits struct {
	// SummaryChars caps each presented summary in Unicode characters
	// (QUIZ_PROMPT_SUMMARY_CHARS). 候補に出す記事は絞らない — 上の
	// コメント参照。
	SummaryChars int
}

// DefaultOutroQuizLimits returns the built-in D-46 (1) default (150文字).
func DefaultOutroQuizLimits() OutroQuizLimits {
	return OutroQuizLimits{SummaryChars: defaultQuizPromptSummaryChars}
}

// withDefaults fills zero-valued fields with the built-in defaults.
func (l OutroQuizLimits) withDefaults() OutroQuizLimits {
	if l.SummaryChars <= 0 {
		l.SummaryChars = defaultQuizPromptSummaryChars
	}
	return l
}

// LoadOutroQuizLimits reads the D-46 (1) prompt-size knob from the environment:
//
//   - QUIZ_PROMPT_SUMMARY_CHARS: 1件あたりの要約の文字数上限 (default 150)
//
// Non-positive or unparsable values fall back to the default with a warning:
// a bad tuning knob must degrade, never stop the broadcast (原則2).
func LoadOutroQuizLimits(logger *slog.Logger) OutroQuizLimits {
	if logger == nil {
		logger = slog.Default()
	}
	limits := OutroQuizLimits{
		SummaryChars: pkgconfig.GetEnvInt("QUIZ_PROMPT_SUMMARY_CHARS", defaultQuizPromptSummaryChars),
	}
	if limits.SummaryChars <= 0 {
		logger.Warn("QUIZ_PROMPT_SUMMARY_CHARS must be positive, using default",
			slog.Int("value", limits.SummaryChars), slog.Int("default", defaultQuizPromptSummaryChars))
		limits.SummaryChars = defaultQuizPromptSummaryChars
	}
	return limits
}

// effectiveSummaryChars decides the per-article summary cap actually used for
// this day's candidate list (D-46 (1) の安全弁)。configured を上限に、候補
// 一覧が quizPromptListBudgetChars に収まるところまで縮める。
//
// 2番目の返り値 fits が false なら、**下限まで縮めても予算に収まらない** ため
// 相乗りセクション自体を出すべきではない(呼び出し側が nil に倒す)。
//
// 記事を落とす選択肢は採らない: 落とすと plan の並び(カテゴリ辞書順)により
// 後ろのコーナーが構造的に出題対象から消える。だから「全記事を短く載せる」か
// 「1件も載せない(クイズなしに縮退)」の二択になる。
func effectiveSummaryChars(scope []repository.RadioArticle, configured int) (chars int, fits bool) {
	if len(scope) == 0 {
		return configured, false
	}
	fixed := quizPromptEntryLabelChars * len(scope)
	for _, a := range scope {
		fixed += utiltext.CountRunes(a.Title)
	}

	// 下限は「40文字」だが、operator が意図的にそれより短く設定した日は
	// その値を下限として扱う(設定を勝手に上書きしない)。
	floor := minQuizPromptSummaryChars
	if configured < floor {
		floor = configured
	}
	// 下限でも収まらない = タイトルとラベルだけで予算を食い潰している日を
	// 含む(available が負になるケース)。
	if fixed+floor*len(scope) > quizPromptListBudgetChars {
		return 0, false
	}

	perArticle := (quizPromptListBudgetChars - fixed) / len(scope)
	if perArticle > configured {
		perArticle = configured
	}
	return perArticle, true
}

// quizPromptOutcome tells the caller what happened to the piggybacked quiz
// section, so it can log the right line (D-46 (1)). すべて既存の縮退方向に
// 沿っており、放送本文には一切影響しない。
type quizPromptOutcome int

const (
	// quizPromptDisabled: count <= 0 か記事ゼロ。相乗りセクションを出さない
	// (QUIZ_ITEMS_PER_DAY=0 / バックプレッシャ / 同日重複 — D-26 (3)、§5.2)。
	quizPromptDisabled quizPromptOutcome = iota
	// quizPromptFull: 設定どおりの要約文字数で出した(通常運転)。
	quizPromptFull
	// quizPromptShortened: 予算に収めるため要約の文字数上限を縮めて出した。
	quizPromptShortened
	// quizPromptOmitted: 下限まで縮めても予算に収まらないので、セクションを
	// 出さなかった(クイズなしへ縮退。放送は止めない — §5.2)。
	quizPromptOmitted
)

// quizPrompt builds the learning-item section data for the outro prompt.
// A nil return renders outro.tmpl exactly as before the Phase 3 extension —
// the backpressure/duplicate-guard switch (§5.2: プロンプト側で抑止、
// トークンも消費しない)と、予算に収まらない日の縮退が合流する。
//
// 候補は当日の全記事(scope = articles)で、番号は1始まりの放送順。パーサへ
// 渡すのも同じスライスなので 記事番号 → article ID の対応はそのまま (§5.1)。
//
// 返り値の2番目は実際に使った要約の文字数上限、3番目は何が起きたか。
func quizPrompt(scope []repository.RadioArticle, count int, limits OutroQuizLimits) (*quizPromptData, int, quizPromptOutcome) {
	limits = limits.withDefaults()
	if count <= 0 || len(scope) == 0 {
		return nil, limits.SummaryChars, quizPromptDisabled
	}
	summaryChars, fits := effectiveSummaryChars(scope, limits.SummaryChars)
	if !fits {
		return nil, limits.SummaryChars, quizPromptOmitted
	}
	entries := make([]quizPromptArticle, len(scope))
	for i, a := range scope {
		summary, truncated := utiltext.TruncateRunes(a.Summary, summaryChars)
		if truncated {
			summary += quizPromptTruncationMark
		}
		entries[i] = quizPromptArticle{Number: i + 1, Title: a.Title, Summary: summary}
	}
	outcome := quizPromptFull
	if summaryChars < limits.SummaryChars {
		outcome = quizPromptShortened
	}
	return &quizPromptData{Count: count, Marker: quizSectionMarker, Articles: entries},
		summaryChars, outcome
}
