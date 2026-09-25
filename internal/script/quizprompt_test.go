package script

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/repository"
)

func promptArticles(n int) []repository.RadioArticle {
	arts := make([]repository.RadioArticle, n)
	for i := range arts {
		arts[i] = repository.RadioArticle{
			ID:      int64(100 + i),
			Title:   "記事のタイトルです",
			Summary: strings.Repeat("要", 900),
		}
	}
	return arts
}

// TestQuizPromptKeepsEveryArticle pins the D-46 (1) 改訂: 候補から記事を
// 落とさない。plan.Plan() は featured をカテゴリのスラッグ辞書順に並べ替える
// ため、先頭N件に絞ると後ろのコーナーが構造的に一度も出題されない。編集判断の
// 対象は当日の全コーナーに保つ (Phase 3 §5.1)。
func TestQuizPromptKeepsEveryArticle(t *testing.T) {
	tests := []struct {
		name     string
		articles int
		count    int
	}{
		{name: "8記事日(本番既定)", articles: 8, count: 1},
		{name: "M を上げても候補は全記事のまま", articles: 8, count: 4},
		{name: "M が記事数と同じ", articles: 8, count: 8},
		{name: "1記事日", articles: 1, count: 1},
		{name: "記事数を倍にしても全件", articles: 16, count: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			articles := promptArticles(tt.articles)
			data, _, _ := quizPrompt(articles, tt.count, DefaultOutroQuizLimits())
			require.NotNil(t, data)
			require.Len(t, data.Articles, tt.articles, "候補から記事は落とさない")
			for i, entry := range data.Articles {
				assert.Equal(t, i+1, entry.Number, "番号は放送順の1始まり")
			}
			assert.Equal(t, tt.count, data.Count)
			assert.Equal(t, quizSectionMarker, data.Marker)
		})
	}

	t.Run("count<=0 は相乗りセクションごと出さない (D-26 (3) / §5.2)", func(t *testing.T) {
		data, _, _ := quizPrompt(promptArticles(8), 0, DefaultOutroQuizLimits())
		assert.Nil(t, data)
	})
	t.Run("記事ゼロ", func(t *testing.T) {
		data, _, _ := quizPrompt(nil, 1, DefaultOutroQuizLimits())
		assert.Nil(t, data)
	})
}

// TestQuizPromptTruncatesSummaries pins 要約の文字数切り詰め (D-46 (1))。
func TestQuizPromptTruncatesSummaries(t *testing.T) {
	limits := DefaultOutroQuizLimits()
	require.Equal(t, 150, limits.SummaryChars, "既定は150文字 (D-46 (1) 改訂)")

	articles := promptArticles(8)
	data, summaryChars, outcome := quizPrompt(articles, 1, limits)
	require.NotNil(t, data)
	assert.Equal(t, quizPromptFull, outcome, "8記事日は予算に収まるので圧縮は効かない")
	assert.Equal(t, limits.SummaryChars, summaryChars)

	for _, entry := range data.Articles {
		assert.True(t, strings.HasSuffix(entry.Summary, quizPromptTruncationMark),
			"切り詰めた要約には途切れを示す記号を付ける")
		assert.Equal(t, limits.SummaryChars+1, len([]rune(entry.Summary)),
			"要約は %d 文字 + 記号1文字に収まる", limits.SummaryChars)
	}

	t.Run("上限以内の要約は素通し", func(t *testing.T) {
		short := []repository.RadioArticle{{ID: 1, Title: "T", Summary: "短い要約。"}}
		data, _, _ := quizPrompt(short, 1, limits)
		require.NotNil(t, data)
		assert.Equal(t, "短い要約。", data.Articles[0].Summary)
	})

	t.Run("ゼロ値の limits は既定値", func(t *testing.T) {
		data, chars, _ := quizPrompt(promptArticles(2), 1, OutroQuizLimits{})
		require.NotNil(t, data)
		assert.Equal(t, defaultQuizPromptSummaryChars, chars)
	})
}

// TestEffectiveSummaryChars pins the safety valve (D-46 (1)): 候補一覧が
// 予算を超える日は**記事を落とさず**要約の文字数上限を縮め、下限まで縮めても
// 収まらない日は fits=false(= 相乗りセクション自体を出さない)。
func TestEffectiveSummaryChars(t *testing.T) {
	tests := []struct {
		name       string
		articles   int
		titleRunes int
		configured int
		wantFits   bool
		wantChars  int // fits のときの期待値(0 = 厳密比較しない)
	}{
		{name: "8記事 × 150文字は予算内(圧縮なし)",
			articles: 8, titleRunes: 9, configured: 150, wantFits: true, wantChars: 150},
		{name: "16記事 × 短いタイトルはまだ予算内",
			articles: 16, titleRunes: 9, configured: 150, wantFits: true, wantChars: 150},
		{
			// 本番のタイトルは60文字前後。タイトルも予算を食うので、同じ
			// 記事数でもタイトルが長い日は先に圧縮が効く。
			name:     "16記事 × 長いタイトルは圧縮される",
			articles: 16, titleRunes: 60, configured: 150, wantFits: true,
		},
		{
			// CodeRabbit 指摘のケース。config.go は MaxArticles <= 0 しか
			// 弾かないので RADIO_MAX_ARTICLES=200 は通る。タイトルとラベル
			// だけで予算を食い潰すので、下限40文字でも収まらない。
			name:     "200記事はタイトルとラベルだけで予算超過 → 出さない",
			articles: 200, titleRunes: 9, configured: 150, wantFits: false,
		},
		{
			// 下限より短い設定は勝手に上書きしない(その値を下限として扱う)
			// が、200記事はそれでも収まらない。
			name:     "200記事 × 極小設定でも収まらない",
			articles: 200, titleRunes: 9, configured: 10, wantFits: false,
		},
		{name: "記事ゼロ", articles: 0, configured: 150, wantFits: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			articles := promptArticles(tt.articles)
			for i := range articles {
				articles[i].Title = strings.Repeat("長", tt.titleRunes)
			}

			got, fits := effectiveSummaryChars(articles, tt.configured)
			require.Equal(t, tt.wantFits, fits)
			if !fits {
				return
			}
			assert.LessOrEqual(t, got, tt.configured, "設定値を超えて長くはしない")
			assert.GreaterOrEqual(t, got, minQuizPromptSummaryChars,
				"fits なら下限以上に収まっている")
			if tt.wantChars > 0 {
				assert.Equal(t, tt.wantChars, got)
			} else {
				assert.Less(t, got, tt.configured, "圧縮が効いていること")
			}
		})
	}

	// 境界: 下限ぎりぎりで収まる記事数と、その1つ上。
	t.Run("下限で収まる境界", func(t *testing.T) {
		// タイトル9文字 + ラベル12文字 + 要約40文字 = 61文字/件。
		// 予算 2800 / 61 = 45 件まで収まる。
		perArticle := 9 + quizPromptEntryLabelChars + minQuizPromptSummaryChars
		fit := quizPromptListBudgetChars / perArticle

		got, fits := effectiveSummaryChars(promptArticles(fit), 150)
		require.True(t, fits, "%d 件はまだ収まる", fit)
		assert.GreaterOrEqual(t, got, minQuizPromptSummaryChars)

		_, fits = effectiveSummaryChars(promptArticles(fit+1), 150)
		assert.False(t, fits, "%d 件は下限でも収まらない", fit+1)
	})
}

// TestQuizPromptOmittedWhenBudgetCannotFit pins the D-46 (1) 縮退: 下限まで
// 縮めても予算に収まらない日は、巨大なプロンプトを投げて 413 でエピソードごと
// 落とすのではなく、相乗りセクションを出さずに放送を出す(§5.2 と同じ
// 「クイズなし」方向。2026-09-25 の欠番と同じ失敗モードの再現を防ぐ)。
func TestQuizPromptOmittedWhenBudgetCannotFit(t *testing.T) {
	tests := []struct {
		name        string
		articles    int
		wantNil     bool
		wantOutcome quizPromptOutcome
	}{
		{name: "8記事(本番既定)はそのまま出す",
			articles: 8, wantOutcome: quizPromptFull},
		{name: "16記事は圧縮して出す(回帰防止: nil にしてはいけない)",
			articles: 16, wantOutcome: quizPromptShortened},
		{name: "200記事は出さない",
			articles: 200, wantNil: true, wantOutcome: quizPromptOmitted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			articles := promptArticles(tt.articles)
			for i := range articles {
				articles[i].Title = strings.Repeat("長", 60) // 本番相当の長さ
			}

			data, _, outcome := quizPrompt(articles, 1, DefaultOutroQuizLimits())
			assert.Equal(t, tt.wantOutcome, outcome)
			if tt.wantNil {
				assert.Nil(t, data, "セクションごと出さない(既存の nil 経路に合流)")
				return
			}
			require.NotNil(t, data)
			assert.Len(t, data.Articles, tt.articles, "出すなら全記事")
		})
	}

	t.Run("count<=0 は omitted ではなく disabled", func(t *testing.T) {
		data, _, outcome := quizPrompt(promptArticles(8), 0, DefaultOutroQuizLimits())
		assert.Nil(t, data)
		assert.Equal(t, quizPromptDisabled, outcome)
	})
}

func TestLoadOutroQuizLimits(t *testing.T) {
	tests := []struct {
		name             string
		summaryChars     string
		wantSummaryChars int
	}{
		{name: "未設定は既定値", wantSummaryChars: 150},
		{name: "上書き", summaryChars: "250", wantSummaryChars: 250},
		{name: "0 は既定へフォールバック(縮退、放送は止めない)",
			summaryChars: "0", wantSummaryChars: 150},
		{name: "負値も既定へ", summaryChars: "-1", wantSummaryChars: 150},
		{name: "数値でない値も既定へ", summaryChars: "long", wantSummaryChars: 150},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 条件分岐なしで常に設定する: 空文字 = 未設定相当
			// (pkg/config.GetEnvInt は os.Getenv が "" を返したら既定値)。
			// if で囲むと、テストプロセスに QUIZ_PROMPT_SUMMARY_CHARS が
			// 残っている環境(~/pulse/.env を source した shell 等)で
			// 「未設定は既定値」のケースがその値を継承して落ちる。
			// t.Setenv はサブテスト終了時に元の値へ戻すので後始末も要らない。
			t.Setenv("QUIZ_PROMPT_SUMMARY_CHARS", tt.summaryChars)

			got := LoadOutroQuizLimits(nil)
			assert.Equal(t, tt.wantSummaryChars, got.SummaryChars)
		})
	}
}
