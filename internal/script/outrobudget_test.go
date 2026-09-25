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
// 見積もると 8,586 トークンで、同じ日に Groq が実際に返した
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
	scope := quizPromptScope(articles, quizCount, limits)
	prompt, err := renderPrompt("outro.tmpl", outroData{
		Date:         spokenDate(time.Date(2026, 9, 25, 4, 30, 0, 0, time.UTC)),
		Corners:      cornerNames(articles),
		ArticleCount: len(articles),
		SignOff:      closingSignOff,
		Quiz:         quizPrompt(scope, quizCount, limits),
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
			name:     "8記事日で M を上げた日(N は M まで広がる)",
			articles: 8, quizCount: 4, limits: DefaultOutroQuizLimits(),
		},
		{
			// RADIO_MAX_ARTICLES を上げてもアウトロのサイズは N で決まる。
			// 「記事数を増やしたら欠番」という 2026-09-25 の構造を断つ。
			name:     "記事数を倍にしてもアウトロは N 件で有界",
			articles: 16, quizCount: 1, limits: DefaultOutroQuizLimits(),
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

// TestOutroPromptWithoutNarrowingExceedsTPM pins the cause of the 2026-09-25
// 欠番: 縮小しない形(全8記事 × 要約900文字を丸ごと)は TPM 8,000 を単体で
// 超える。この行が落ちたら「もう縮小は不要」になった可能性があるので、
// 前提(D-41 の TPM 8,000)ごと再確認する。
func TestOutroPromptWithoutNarrowingExceedsTPM(t *testing.T) {
	articles := budgetArticles(t, 8, 900)
	preD46 := OutroQuizLimits{MaxArticles: len(articles), SummaryChars: 900}
	prompt := renderBudgetOutro(t, articles, 1, preD46)

	got := estimateTokens(prompt)
	t.Logf("pre-D-46 shape: runes=%d est_tokens=%d (Groq 実測 Requested 8873)",
		len([]rune(prompt)), got)
	assert.Greater(t, got, groqFreeTierTPM,
		"縮小前の形は TPM を超えるはず(2026-09-25 実測 8873)")

	narrowed := estimateTokens(renderBudgetOutro(t, articles, 1, DefaultOutroQuizLimits()))
	assert.Less(t, narrowed*2, got, "縮小で半分以下になっていること")
}
