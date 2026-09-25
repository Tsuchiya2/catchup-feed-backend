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
			Title:   "タイトル",
			Summary: strings.Repeat("要", 900),
		}
	}
	return arts
}

// TestQuizPromptScope pins D-46 (1) の絞り込み規則。
func TestQuizPromptScope(t *testing.T) {
	tests := []struct {
		name     string
		articles int
		count    int
		limits   OutroQuizLimits
		wantLen  int
	}{
		{name: "count<=0 はセクションごと出さない (D-26 (3) / §5.2)",
			articles: 8, count: 0, limits: DefaultOutroQuizLimits(), wantLen: 0},
		{name: "8記事日は上位4件に絞る",
			articles: 8, count: 1, limits: DefaultOutroQuizLimits(), wantLen: 4},
		{name: "記事数が N 未満ならそのまま全件",
			articles: 3, count: 1, limits: DefaultOutroQuizLimits(), wantLen: 3},
		{name: "M が N を超える日は N を M まで広げる(自己矛盾プロンプト防止)",
			articles: 8, count: 6, limits: DefaultOutroQuizLimits(), wantLen: 6},
		{name: "M が記事数を超えても記事数で止まる",
			articles: 3, count: 6, limits: DefaultOutroQuizLimits(), wantLen: 3},
		{name: "ゼロ値の limits は既定値 (N=4)",
			articles: 8, count: 1, limits: OutroQuizLimits{}, wantLen: 4},
		{name: "N を明示的に広げれば広がる",
			articles: 8, count: 1, limits: OutroQuizLimits{MaxArticles: 8, SummaryChars: 900}, wantLen: 8},
		{name: "記事ゼロ",
			articles: 0, count: 1, limits: DefaultOutroQuizLimits(), wantLen: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			articles := promptArticles(tt.articles)
			scope := quizPromptScope(articles, tt.count, tt.limits)
			require.Len(t, scope, tt.wantLen)
			// 放送順の先頭から: 記事番号 -> article ID の対応が保たれること。
			for i := range scope {
				assert.Equal(t, articles[i].ID, scope[i].ID, "放送順の上位N件であること")
			}
		})
	}
}

// TestQuizPromptTruncatesSummaries pins 要約の文字数切り詰め (D-46 (1))。
func TestQuizPromptTruncatesSummaries(t *testing.T) {
	articles := promptArticles(8)
	limits := DefaultOutroQuizLimits()
	scope := quizPromptScope(articles, 1, limits)

	data := quizPrompt(scope, 1, limits)
	require.NotNil(t, data)
	require.Len(t, data.Articles, limits.MaxArticles)
	assert.Equal(t, 1, data.Count)
	assert.Equal(t, quizSectionMarker, data.Marker)

	for i, entry := range data.Articles {
		assert.Equal(t, i+1, entry.Number, "番号は scope 上の1始まり")
		assert.True(t, strings.HasSuffix(entry.Summary, quizPromptTruncationMark),
			"切り詰めた要約には途切れを示す記号を付ける")
		assert.Equal(t, limits.SummaryChars+1, len([]rune(entry.Summary)),
			"要約は %d 文字 + 記号1文字に収まる", limits.SummaryChars)
	}

	t.Run("上限以内の要約は素通し", func(t *testing.T) {
		short := []repository.RadioArticle{{ID: 1, Title: "T", Summary: "短い要約。"}}
		data := quizPrompt(short, 1, limits)
		require.NotNil(t, data)
		assert.Equal(t, "短い要約。", data.Articles[0].Summary)
	})

	t.Run("count<=0 は nil (相乗りセクションなし)", func(t *testing.T) {
		assert.Nil(t, quizPrompt(scope, 0, limits))
		assert.Nil(t, quizPrompt(nil, 1, limits))
	})
}

func TestLoadOutroQuizLimits(t *testing.T) {
	tests := []struct {
		name             string
		maxArticles      string
		summaryChars     string
		wantMaxArticles  int
		wantSummaryChars int
	}{
		{name: "未設定は既定値", wantMaxArticles: 4, wantSummaryChars: 300},
		{name: "上書き", maxArticles: "6", summaryChars: "500", wantMaxArticles: 6, wantSummaryChars: 500},
		{name: "0 や負値は既定へフォールバック(縮退、放送は止めない)",
			maxArticles: "0", summaryChars: "-1", wantMaxArticles: 4, wantSummaryChars: 300},
		{name: "数値でない値も既定へ", maxArticles: "many", summaryChars: "long",
			wantMaxArticles: 4, wantSummaryChars: 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.maxArticles != "" {
				t.Setenv("QUIZ_PROMPT_MAX_ARTICLES", tt.maxArticles)
			}
			if tt.summaryChars != "" {
				t.Setenv("QUIZ_PROMPT_SUMMARY_CHARS", tt.summaryChars)
			}
			got := LoadOutroQuizLimits(nil)
			assert.Equal(t, tt.wantMaxArticles, got.MaxArticles)
			assert.Equal(t, tt.wantSummaryChars, got.SummaryChars)
		})
	}
}
