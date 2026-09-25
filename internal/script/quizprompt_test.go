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
	data, summaryChars, clamped := quizPrompt(articles, 1, limits)
	require.NotNil(t, data)
	assert.False(t, clamped, "8記事日は予算に収まるので圧縮は効かない")
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
// 予算を超える日は**記事を落とさず**要約の文字数上限を縮める。
func TestEffectiveSummaryChars(t *testing.T) {
	tests := []struct {
		name        string
		articles    int
		configured  int
		wantClamped bool
		wantAtMost  int
	}{
		{name: "8記事 × 150文字は予算内(圧縮なし)",
			articles: 8, configured: 150, wantClamped: false, wantAtMost: 150},
		{name: "16記事 × 短いタイトルはまだ予算内",
			articles: 16, configured: 150, wantClamped: false, wantAtMost: 150},
		{name: "記事数を極端に増やすと下限で止まる",
			articles: 200, configured: 150, wantClamped: true, wantAtMost: 150},
		{name: "設定値が小さい日は圧縮しない",
			articles: 200, configured: 40, wantClamped: false, wantAtMost: 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, clamped := effectiveSummaryChars(promptArticles(tt.articles), tt.configured)
			assert.Equal(t, tt.wantClamped, clamped)
			assert.LessOrEqual(t, got, tt.wantAtMost)
			assert.GreaterOrEqual(t, got, minQuizPromptSummaryChars,
				"素材にならない長さまでは縮めない(縮退のまま進む)")
		})
	}

	// タイトルも予算を食う: 同じ記事数でもタイトルが長い日は先に圧縮が効く。
	// 本番のタイトルは 60 文字前後あり、16記事日はこちらに当たる
	// (outrobudget_test.go の 16記事ケースが実測で圧縮されている)。
	t.Run("タイトルが長い日は同じ記事数でも圧縮が効く", func(t *testing.T) {
		long := promptArticles(16)
		for i := range long {
			long[i].Title = strings.Repeat("長", 60)
		}
		got, clamped := effectiveSummaryChars(long, 150)
		assert.True(t, clamped)
		assert.Less(t, got, 150)
		assert.GreaterOrEqual(t, got, minQuizPromptSummaryChars)
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
			if tt.summaryChars != "" {
				t.Setenv("QUIZ_PROMPT_SUMMARY_CHARS", tt.summaryChars)
			}
			got := LoadOutroQuizLimits(nil)
			assert.Equal(t, tt.wantSummaryChars, got.SummaryChars)
		})
	}
}
