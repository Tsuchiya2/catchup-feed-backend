package script

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		{
			// newsLead は「同じカテゴリが飛び飛びに現れない」— つまり Plan が
			// カテゴリ順に並べていること — を前提に、直前の1件だけを見る。
			// GenerateEpisode は任意順の記事を受け取れるので、前提が崩れた
			// ときの挙動をここで固定しておく: 同じコーナーを2度宣言するが、
			// 放送は壊れない(誤りは並び順の側にある)。
			name:       "ungrouped input re-announces a corner (Plan のカテゴリ順への依存)",
			categories: []string{"ai", "dev", "ai"},
			want: []string{
				"ここからは、AIのコーナーです。",
				"ここからは、開発のコーナーです。",
				"ここからは、AIのコーナーです。",
			},
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

// TestCornerLeadsShareOneWording pins D-37 (9) for corner openings: news,
// 復習, 書籍 のどのコーナーも newsLeadCorner ただ1つを通る。テンプレート側や
// 別ファイルに同じ文を書き写すと、ここが落ちる前に静かに旧文言が残る。
func TestCornerLeadsShareOneWording(t *testing.T) {
	assert.Equal(t, "ここからは、いま読んでいる本のコーナーです。", bookReviewLead())
	assert.Equal(t, newsLeadCorner("いま読んでいる本"), bookReviewLead())
	assert.True(t, strings.HasPrefix(quizCornerLead(3), newsLeadCorner("復習")),
		"復習コーナーの入りも同じ定型句から作る")
}

// TestPromptTemplatesHoldNoSpokenWording pins D-37 (9) structurally: prompts/
// が持つのは「指示」だけで、台本に出る言い回しは format.go からテンプレート
// 変数として渡す。定型句をテンプレートに書き写した瞬間に集約先が2つになり、
// format.go だけ直したときに片方が静かに旧文言で残る — その退行をここで止める。
func TestPromptTemplatesHoldNoSpokenWording(t *testing.T) {
	entries, err := promptFS.ReadDir("prompts")
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	// 台本に出る言い回しの断片。テンプレートに現れてはいけない。
	banned := []string{
		spokenShowName,
		"のコーナーです",
		"おはようございます",
		"続いてのニュースです",
		"また明日お会いしましょう",
		"番組情報欄",
	}
	for _, e := range entries {
		body, err := promptFS.ReadFile("prompts/" + e.Name())
		require.NoError(t, err)
		for _, phrase := range banned {
			assert.NotContains(t, string(body), phrase,
				"%s: 定型句はテンプレートに書かず format.go から渡す (D-37 (9))", e.Name())
		}
	}
}

// TestQuizCornerLead pins the 復習コーナー introduction in full. 固定文なので
// 全文一致で留める — 「さて、」のような口調のぶれ (D-37 (2)) や、承認済み文案
// からの逸脱を戻せないようにするため。
func TestQuizCornerLead(t *testing.T) {
	tests := []struct {
		items int
		want  string
	}{
		{
			items: 1,
			want: "ここからは、復習のコーナーです。これまでの放送でお伝えした内容から、今日は1問おさらいします。" +
				"問題のあとに少し間をあけますので、頭の中で答えてみてください。",
		},
		{
			items: 3,
			want: "ここからは、復習のコーナーです。これまでの放送でお伝えした内容から、今日は3問おさらいします。" +
				"問題のあとに少し間をあけますので、頭の中で答えてみてください。",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d問", tt.items), func(t *testing.T) {
			got := quizCornerLead(tt.items)
			assert.Equal(t, tt.want, got)
			assert.NotContains(t, got, "！", "アナウンサー調: 感嘆符を使わない (D-37 (2))")
			assert.NotContains(t, got, asciiShowName)
		})
	}
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
