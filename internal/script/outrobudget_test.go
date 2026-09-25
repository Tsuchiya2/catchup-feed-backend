package script

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/repository"
)

// ───────── D-46 (1): アウトロプロンプトのトークン予算 ─────────
//
// 2026-09-25 の欠番は「アウトロ1本が Groq 無料枠の TPM を単体で超えた」こと
// が直接原因である(`413 Request too large ... tokens per minute (TPM):
// Limit 8000, Requested 8873`)。413 は 429 と違って待っても通らない — D-26 の
// 「57秒待って同一プロバイダ1回再試行」では救えない種類の失敗であり、
// プロンプトを小さくするしか手がない。
//
// このテストはその上限を**構造として**固定する。outro.tmpl やクイズ相乗り
// セクションに文章を足したり、既定の N / 文字数を緩めたりした瞬間に落ちる。

// groqFreeTierTPM is the Groq free-tier tokens-per-minute ceiling after D-41
// (2026-08-15: 12,000 -> 8,000). 単一リクエストがこれを超えると 413。
const groqFreeTierTPM = 8000

// outroPromptTokenBudget is the ceiling this test enforces for one rendered
// outro prompt. TPM は「直近1分の rolling window」なので、上限そのものに
// 収まるだけでは足りない: 同じ分に先行するニュース段(1本あたり約1,700
// トークン)が窓を食っており、さらに gpt-oss-120b の reasoning を含む出力も
// 窓に乗る。上限の半分を天井に置く。
const outroPromptTokenBudget = groqFreeTierTPM / 2

// estimateTokens approximates the token count of a prompt for Groq's
// o200k 系トークナイザ(gpt-oss-120b): 日本語などの非 ASCII はほぼ1文字
// 1トークン、ASCII は概ね4文字1トークン。
//
// **実測との突き合わせ**: この式で D-46 以前の形(8記事 × 要約900文字)を
// 見積もると 8,582 トークン(TestOutroPromptWithoutNarrowingExceedsTPM の
// ログで確認できる)で、同じ日に Groq が実際に返した
// `Requested 8873` と 3.3% の差に収まる。トークナイザを持ち込まずに
// (ゼロ円・右サイズ: 新規依存なし)判断するにはこの精度で十分である。
func estimateTokens(prompt string) int {
	ascii, other := 0, 0
	for _, r := range prompt {
		if r < unicode.MaxASCII {
			ascii++
		} else {
			other++
		}
	}
	return other + ascii/4
}

// budgetArticles builds a realistic day: 日本語のタイトルと、指定文字数まで
// 埋めた要約(SUMMARIZER_CHAR_LIMIT の既定は900文字)。
func budgetArticles(t *testing.T, n, summaryChars int) []repository.RadioArticle {
	t.Helper()
	require.Greater(t, summaryChars, 40)
	lead := "この記事は、推論基盤の構成と実測されたコスト削減率について述べている。"
	arts := make([]repository.RadioArticle, n)
	categories := []string{"ai", "dev", "infra", "security"}
	for i := range arts {
		arts[i] = repository.RadioArticle{
			ID:         int64(i + 1),
			Title:      fmt.Sprintf("大規模言語モデルの推論コストを三分の一に下げる量子化手法が公開された その%d", i+1),
			SourceName: "Example Tech Blog",
			Category:   categories[i%len(categories)],
			Summary:    lead + strings.Repeat("あ", summaryChars-len([]rune(lead))),
		}
	}
	return arts
}

func renderBudgetOutro(t *testing.T, articles []repository.RadioArticle, quizCount int, limits OutroQuizLimits) string {
	t.Helper()
	// 候補は当日の全記事(D-46 (1) 改訂: 記事は絞らない)。
	quiz, _, _ := quizPrompt(articles, quizCount, limits)
	prompt, err := renderPrompt("outro.tmpl", outroData{
		Date:         spokenDate(time.Date(2026, 9, 25, 4, 30, 0, 0, time.UTC)),
		Corners:      cornerNames(articles),
		ArticleCount: len(articles),
		SignOff:      closingSignOff,
		Quiz:         quiz,
	})
	require.NoError(t, err)
	return prompt
}

// TestOutroPromptFitsGroqFreeTierTPM pins D-46 (1): 8記事日のアウトロ
// プロンプトが Groq 無料枠 TPM 8,000 の内側に、余裕をもって収まること。
func TestOutroPromptFitsGroqFreeTierTPM(t *testing.T) {
	tests := []struct {
		name      string
		articles  int
		quizCount int
		limits    OutroQuizLimits
	}{
		{
			name:     "8記事日(本番既定: RADIO_MAX_ARTICLES=8 / QUIZ_ITEMS_PER_DAY=1)",
			articles: 8, quizCount: 1, limits: DefaultOutroQuizLimits(),
		},
		{
			// M(QUIZ_ITEMS_PER_DAY)はプロンプトの大きさに影響しない —
			// 候補は常に当日の全記事で、M は「何件選ぶか」の指示だけ。
			// 当初実装では M が候補件数の下限になっていて上限を突破できた
			// (レビュー B-3)。ここはその経路が消えたことの固定でもある。
			name:     "M を記事数まで上げた日(候補件数は M に引っ張られない)",
			articles: 8, quizCount: 8, limits: DefaultOutroQuizLimits(),
		},
		{
			// レビュー B-3 が 413 再発を外挿したケース。記事数と M の両方を
			// 16 にしても、要約の文字数予算(quizPromptListBudgetChars)が
			// 効いて天井の内側に収まること。
			name:     "記事16件 × M=16(B-3 の外挿ケース)",
			articles: 16, quizCount: 16, limits: DefaultOutroQuizLimits(),
		},
		{
			name:     "記事数を倍にした日(M は既定)",
			articles: 16, quizCount: 1, limits: DefaultOutroQuizLimits(),
		},
		{
			// CodeRabbit 指摘のケース。RADIO_MAX_ARTICLES=200 は config.go を
			// 通る(<= 0 しか弾かない)。下限40文字でも予算に収まらないので
			// 相乗りセクションは出さず、クイズなしと同じサイズに落ちる。
			// 落とさないと約18,000文字 = Groq 確定 413 で 2026-09-25 の欠番が
			// 再現する。
			name:     "記事200件(相乗りセクションを出さない縮退)",
			articles: 200, quizCount: 1, limits: DefaultOutroQuizLimits(),
		},
		{
			name:     "クイズなし(QUIZ_ITEMS_PER_DAY=0 / D-26 (3) の再試行プロンプト)",
			articles: 8, quizCount: 0, limits: DefaultOutroQuizLimits(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			articles := budgetArticles(t, tt.articles, 900)
			prompt := renderBudgetOutro(t, articles, tt.quizCount, tt.limits)
			got := estimateTokens(prompt)
			t.Logf("runes=%d bytes=%d est_tokens=%d", len([]rune(prompt)), len(prompt), got)
			assert.LessOrEqual(t, got, outroPromptTokenBudget,
				"アウトロ1本が TPM %d の半分を超えた。outro.tmpl を足したか既定の "+
					"N / 要約文字数を緩めたか。413 は待っても通らない (D-46 (1))",
				groqFreeTierTPM)
		})
	}
}

// TestOutroPromptOmitsQuizSectionAtExtremeArticleCounts pins that the 200記事
// ケースは「小さくなった」のではなく**相乗りセクションが消えた**結果である
// ことを固定する(D-46 (1))。クイズなしプロンプトとバイト単位で一致する。
func TestOutroPromptOmitsQuizSectionAtExtremeArticleCounts(t *testing.T) {
	articles := budgetArticles(t, 200, 900)

	withQuiz := renderBudgetOutro(t, articles, 1, DefaultOutroQuizLimits())
	quizless := renderBudgetOutro(t, articles, 0, DefaultOutroQuizLimits())

	assert.Equal(t, quizless, withQuiz,
		"下限でも予算に収まらない日は相乗りセクションを出さない(既存の nil 経路)")
	assert.NotContains(t, withQuiz, quizSectionMarker)
	assert.LessOrEqual(t, estimateTokens(withQuiz), outroPromptTokenBudget)
	t.Logf("200記事: est_tokens=%d (quizless と同一)", estimateTokens(withQuiz))
}

// TestOutroPromptWithoutNarrowingExceedsTPM pins the cause of the 2026-09-25
// 欠番: 縮小しない形(全8記事 × 要約900文字を丸ごと)は TPM 8,000 を単体で
// 超える。この行が落ちたら「もう縮小は不要」になった可能性があるので、
// 前提(D-41 の TPM 8,000)ごと再確認する。
func TestOutroPromptWithoutNarrowingExceedsTPM(t *testing.T) {
	articles := budgetArticles(t, 8, 900)

	// 旧実装の形は quizPrompt では組めない(要約の文字数予算が必ず効く)ので、
	// 相乗りセクションのデータを直接組んで当時のプロンプトを再現する。
	entries := make([]quizPromptArticle, len(articles))
	for i, a := range articles {
		entries[i] = quizPromptArticle{Number: i + 1, Title: a.Title, Summary: a.Summary}
	}
	prompt, err := renderPrompt("outro.tmpl", outroData{
		Date:         spokenDate(time.Date(2026, 9, 25, 4, 30, 0, 0, time.UTC)),
		Corners:      cornerNames(articles),
		ArticleCount: len(articles),
		SignOff:      closingSignOff,
		Quiz:         &quizPromptData{Count: 1, Marker: quizSectionMarker, Articles: entries},
	})
	require.NoError(t, err)

	got := estimateTokens(prompt)
	t.Logf("pre-D-46 shape: runes=%d est_tokens=%d (Groq 実測 Requested 8873)",
		len([]rune(prompt)), got)
	assert.Greater(t, got, groqFreeTierTPM,
		"縮小前の形は TPM を超えるはず(2026-09-25 実測 8873)")

	narrowed := estimateTokens(renderBudgetOutro(t, articles, 1, DefaultOutroQuizLimits()))
	assert.Less(t, narrowed*3, got, "縮小で3分の1以下になっていること")
	t.Logf("narrowed (既定150文字): est_tokens=%d", narrowed)
}
