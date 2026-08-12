package script

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"catchup-feed/internal/repository"
)

// asciiShowName is what RADIO_SHOW_NAME defaults to. It must never appear in
// anything that gets synthesized (D-37 (3)): it is for episode titles and ID3
// tags only. This test file pins that separation from the script side.
const asciiShowName = "catchup-feed"

func TestSpokenDate(t *testing.T) {
	tests := []struct {
		name string
		date time.Time
		want string
	}{
		{"weekday is derived in Go, not by the model", time.Date(2026, 8, 12, 4, 30, 0, 0, time.UTC), "8月12日水曜日"},
		{"sunday (weekday table lower bound)", time.Date(2026, 8, 9, 4, 30, 0, 0, time.UTC), "8月9日日曜日"},
		{"saturday (weekday table upper bound)", time.Date(2026, 8, 15, 4, 30, 0, 0, time.UTC), "8月15日土曜日"},
		{"single-digit month keeps no leading zero", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "1月1日木曜日"},
		{"december", time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC), "12月31日木曜日"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, spokenDate(tt.date))
		})
	}

	t.Run("the caller's location decides the broadcast day", func(t *testing.T) {
		jst := time.FixedZone("JST", 9*60*60)
		utc := time.Date(2026, 8, 11, 19, 30, 0, 0, time.UTC) // = 8/12 04:30 JST
		assert.Equal(t, "8月12日水曜日", spokenDate(utc.In(jst)),
			"Pipeline.Run passes now.In(Config.Location) — JST の放送日で読む")
	})
}

func TestCornerName(t *testing.T) {
	tests := []struct {
		slug  string
		want  string
		known bool
	}{
		{slug: "ai", want: "AI", known: true},
		{slug: "community", want: "コミュニティ", known: true},
		{slug: "dev", want: "開発", known: true},
		{slug: "infra", want: "インフラ", known: true},
		{slug: "security", want: "セキュリティ", known: true},
		// 未知のスラッグはそのまま返す(format.go の cornerName 参照:
		// 「そのほか」等に寄せると対応表の追従漏れが可聴にならないため)。
		{slug: "product", want: "product", known: false},
		{slug: "", want: "", known: false},
	}
	for _, tt := range tests {
		t.Run("slug="+tt.slug, func(t *testing.T) {
			assert.Equal(t, tt.want, cornerName(tt.slug))
			assert.Equal(t, tt.known, isKnownCorner(tt.slug))
		})
	}
}

func TestCornerNames(t *testing.T) {
	articles := []repository.RadioArticle{
		{Category: "ai"}, {Category: "ai"}, {Category: "dev"}, {Category: "infra"}, {Category: "dev"},
	}
	assert.Equal(t, []string{"AI", "開発", "インフラ"}, cornerNames(articles),
		"重複を畳んで放送順のまま日本語名に変換する")
	assert.Nil(t, cornerNames(nil))
}

// TestNewsLead pins the corner continuation/switch decision (D-37 (6)):
// 1本目は必ずコーナー切替、以降は直前の記事とのカテゴリ比較で決まる。
func TestNewsLead(t *testing.T) {
	tests := []struct {
		name       string
		categories []string
		want       []string
	}{
		{
			name:       "first article always opens a corner",
			categories: []string{"ai"},
			want:       []string{"ここからは、AIのコーナーです。"},
		},
		{
			name:       "same corner continues",
			categories: []string{"ai", "ai"},
			want:       []string{"ここからは、AIのコーナーです。", "続いてのニュースです。"},
		},
		{
			name:       "corner switch announces the new corner",
			categories: []string{"ai", "ai", "dev", "infra", "infra"},
			want: []string{
				"ここからは、AIのコーナーです。",
				"続いてのニュースです。",
				"ここからは、開発のコーナーです。",
				"ここからは、インフラのコーナーです。",
				"続いてのニュースです。",
			},
		},
		{
			name:       "unknown slug still switches, spoken as the raw slug",
			categories: []string{"product", "product"},
			want:       []string{"ここからは、productのコーナーです。", "続いてのニュースです。"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			articles := make([]repository.RadioArticle, len(tt.categories))
			for i, c := range tt.categories {
				articles[i] = repository.RadioArticle{Category: c}
			}
			got := make([]string, len(articles))
			for i := range articles {
				got[i] = newsLead(articles, i, cornerName(articles[i].Category))
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestSpokenTextUsesTheKanaShowName pins D-37 (3): every fixed phrase that
// becomes audio names the program in kana, and none of them can pick up
// RADIO_SHOW_NAME's ASCII value (it is not even reachable from this package).
func TestSpokenTextUsesTheKanaShowName(t *testing.T) {
	date := time.Date(2026, 8, 12, 4, 30, 0, 0, time.UTC)
	texts := map[string]string{
		"openingLead":         openingLead(date),
		"closingSignOff":      closingSignOff,
		"quizOnlyIntro":       QuizOnlyIntro(date),
		"quizOnlyOutro":       QuizOnlyOutro(),
		"programFormat.Show":  programFormat{}.Show(),
		"quizCornerLead":      quizCornerLead(3),
		"weeklyReviewLead":    weeklyReviewLead,
		"quizOnlyShowNotes":   QuizOnlyShowNotesBase(),
		"weeklyReviewClosing": weeklyReviewClosing,
	}
	for name, text := range texts {
		t.Run(name, func(t *testing.T) {
			assert.NotContains(t, text, asciiShowName,
				"読み上げ・プロンプトに ASCII の番組名を出さない (D-37 (3))")
		})
	}
	assert.Equal(t, "キャッチアップフィード", programFormat{}.Show())
	assert.Contains(t, texts["openingLead"], "キャッチアップフィード")
	assert.Contains(t, texts["closingSignOff"], "キャッチアップフィード")
	assert.Contains(t, texts["quizOnlyIntro"], "キャッチアップフィード")
	assert.Contains(t, texts["quizOnlyOutro"], "キャッチアップフィード")
}

// TestFixedPhrases pins the exact wording of the D-37 定型句. They are passed
// to the LLM today and may be prepended mechanically later (D-37 (10)) — the
// wording must survive that move unchanged, so it is pinned here rather than
// in whichever caller currently uses it.
func TestFixedPhrases(t *testing.T) {
	date := time.Date(2026, 8, 12, 4, 30, 0, 0, time.UTC)
	assert.Equal(t, "おはようございます。キャッチアップフィード、8月12日水曜日の放送です。", openingLead(date))
	assert.Equal(t, "それでは、最初のニュースです。", openingHandoff)
	assert.Equal(t, "続いてのニュースです。", newsLeadContinued)
	assert.Equal(t, "ここからは、開発のコーナーです。", newsLeadCorner("開発"))
	assert.Equal(t,
		"詳しいリンクは番組情報欄に掲載しています。キャッチアップフィード、また明日お会いしましょう。",
		closingSignOff)
	assert.Equal(t,
		"おはようございます。キャッチアップフィード、8月12日水曜日の放送です。今朝は新しい記事のお届けはありません。かわりに、これまでの放送のおさらいをお届けします。",
		QuizOnlyIntro(date), "記事ゼロの日も通常回と同じ冒頭定型句で始まる")
	assert.True(t, strings.HasSuffix(QuizOnlyOutro(), signOff),
		"記事ゼロの日の締めも通常回と同じ署名で終わる")
}

func TestItemCount(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "1つ"},
		{9, "9つ"},
		{10, "10個"}, // 「10つ」は日本語にならないので助数詞を切り替える
		{21, "21個"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, itemCount(tt.n))
		})
	}
}
