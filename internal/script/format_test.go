package script

import (
	"fmt"
	"os"
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

// fixedWording is one entry of the single 番組の文言 inventory shared by
// TestFixedPhrases and TestSpokenWordingLivesOnlyInFormatGo.
type fixedWording struct {
	name     string // 失敗メッセージ用の識別子
	whole    string // 代表値で組んだ全文(承認済み文案そのもの)
	got      string // format.go が実際に返した文字列
	fragment string // 可変部を除いた不変部分。format.go 以外に現れてはいけない
	noScan   string // 空でなければ走査から除外する理由
}

// fixedWordings enumerates every phrase format.go puts into the broadcast
// (音声) or the show notes (番組情報欄・通知メール). ONE list, two jobs:
//
//   - TestFixedPhrases … whole を全文一致で固定する。承認済み文案からの逸脱と
//     口調のぶれ (D-37 (1)(2)) を検知する。
//   - TestSpokenWordingLivesOnlyInFormatGo … fragment が format.go 以外の
//     ソースに現れないことを走査する。集約先が2つに割れる退行 (D-37 (9)) を
//     検知する。
//
// **format.go に文言を足したら、ここに1行足すこと。** 逆に、走査が偽陽性を
// 出したときにアサーションを緩めたり fragment を短くしたりして黙らせては
// いけない — 正しい直し方は、引っかかった文言を format.go の変数にして
// 呼び出し側へ渡すことである(それがこのテストの目的そのもの)。
func fixedWordings() []fixedWording {
	date := time.Date(2026, 8, 12, 4, 30, 0, 0, time.UTC) // 水曜日
	return []fixedWording{
		{name: "spokenShowName", whole: "キャッチアップフィード", got: spokenShowName,
			fragment: "キャッチアップフィード"},
		{name: "openingLead",
			whole: "おはようございます。キャッチアップフィード、8月12日水曜日の放送です。",
			got:   openingLead(date), fragment: "おはようございます。"},
		{name: "openingHandoff", whole: "それでは、最初のニュースです。", got: openingHandoff,
			fragment: "それでは、最初のニュースです。"},
		{name: "newsLeadContinued", whole: "続いてのニュースです。", got: newsLeadContinued,
			fragment: "続いてのニュースです。"},
		{name: "cornerLead", whole: "ここからは、開発のコーナーです。", got: cornerLead("開発"),
			fragment: "のコーナーです。"},
		{name: "bookReviewLead", whole: "ここからは、いま読んでいる本のコーナーです。", got: bookReviewLead(),
			fragment: "いま読んでいる本のコーナー"},
		{name: "signOff", whole: "キャッチアップフィード、また明日お会いしましょう。", got: signOff,
			fragment: "また明日お会いしましょう。"},
		{name: "closingSignOff",
			whole: "詳しいリンクは番組情報欄に掲載しています。キャッチアップフィード、また明日お会いしましょう。",
			got:   closingSignOff, fragment: "詳しいリンクは番組情報欄に掲載しています。"},
		{name: "quizCornerLead",
			whole: "ここからは、復習のコーナーです。これまでの放送でお伝えした内容から、今日は3問おさらいします。" +
				"問題のあとに少し間をあけますので、頭の中で答えてみてください。",
			got: quizCornerLead(3), fragment: "これまでの放送でお伝えした内容から、"},
		// 「第N問。」は番号が可変なので、走査に使えるのは「問。」まで。
		{name: "quizReadQuestion", whole: "第1問。設問。", got: quizReadQuestion(1, "設問。"),
			fragment: "問。"},
		{name: "quizReadAnswer", whole: "答え。解答。", got: quizReadAnswer("解答。"),
			fragment: "答え。"},
		{name: "quizOnlyIntroScript",
			whole: "おはようございます。キャッチアップフィード、8月12日水曜日の放送です。" +
				"今朝は新しい記事のお届けはありません。かわりに、これまでの放送のおさらいをお届けします。",
			got: QuizOnlyIntro(date), fragment: "今朝は新しい記事のお届けはありません。"},
		{name: "quizOnlyOutroScript",
			whole: "今日のおさらいは以上です。キャッチアップフィード、また明日お会いしましょう。",
			got:   QuizOnlyOutro(), fragment: "今日のおさらいは以上です。"},
		{name: "quizOnlyShowNotesBase", whole: "今日は新しい記事がなかったため、復習のみお届けしました。",
			got: QuizOnlyShowNotesBase(), fragment: "今日は新しい記事がなかったため、"},

		// ショーノート(音声にはならないが番組情報欄と通知メールに出る)。
		{name: "notesFeaturedHeading", whole: "今日紹介した記事:\n", got: notesFeaturedHeading,
			fragment: "今日紹介した記事:"},
		{name: "notesOverflowHeading", whole: "\n紹介しきれなかった記事:\n", got: notesOverflowHeading,
			fragment: "紹介しきれなかった記事:"},
		{name: "notesVoicevoxCreditPrefix", whole: "\n\n音声合成: VOICEVOX:", got: notesVoicevoxCreditPrefix,
			fragment: "音声合成: VOICEVOX:"},
		{name: "quizNotesHeading", whole: "\n\n今日の復習:\n", got: quizNotesHeading,
			fragment: "今日の復習:"},
		{name: "quizNotesGradePrefix", whole: "\n採点はこちら: ", got: quizNotesGradePrefix,
			fragment: "採点はこちら:"},
		{name: "weeklyNotesHeading", whole: "\n\n今週の学び:\n", got: weeklyNotesHeading,
			fragment: "今週の学び:"},
		{name: "weeklyNotesGraduatedFormat", whole: "卒業した項目: %d件", got: weeklyNotesGraduatedFormat,
			fragment: "卒業した項目:"},
		{name: "weeklyNotesReintroducePrefix", whole: "\nもう一度おさらい: ", got: weeklyNotesReintroducePrefix,
			fragment: "もう一度おさらい:"},

		// 週次振り返り(§7.4)。
		{name: "weeklyReviewLead", whole: "ここで、今週の学びを振り返ります。", got: weeklyReviewLead,
			fragment: "ここで、今週の学びを振り返ります。"},
		{name: "weeklyReviewClosing", whole: "来週も少しずつ続けていきましょう。", got: weeklyReviewClosing,
			fragment: "来週も少しずつ続けていきましょう。"},
		{name: "weeklyReviewConcepts", whole: "今週学んだ項目は、A、B、C の3つです。",
			got: weeklyReviewConcepts([]string{"A", "B", "C"}), fragment: "今週学んだ項目は、"},
		{name: "weeklyReviewGraduated(afterConcepts)",
			whole:    "また、2つの項目が繰り返しの復習を経て定着し、復習リストを卒業しました。",
			got:      weeklyReviewGraduated(2, true),
			fragment: "の項目が繰り返しの復習を経て定着し、復習リストを卒業しました。"},
		{name: "weeklyReviewGraduated(first sentence)",
			whole:  "今週は2つの項目が繰り返しの復習を経て定着し、復習リストを卒業しました。",
			got:    weeklyReviewGraduated(2, false),
			noScan: "本文は afterConcepts 版と同一で、その fragment が既に走査対象"},
		{name: "weeklyReviewReintroduced(afterOther)",
			whole:    "いっぽうで「難しい概念」は一度忘れてしまったため、もう一度おさらいのリストに戻しています。",
			got:      weeklyReviewReintroduced("難しい概念", true),
			fragment: "は一度忘れてしまったため、もう一度おさらいのリストに戻しています。"},
		{name: "weeklyReviewReintroduced(first sentence)",
			whole:  "「難しい概念」は一度忘れてしまったため、もう一度おさらいのリストに戻しています。",
			got:    weeklyReviewReintroduced("難しい概念", false),
			noScan: "本文は afterOther 版と同一で、その fragment が既に走査対象"},
	}
}

// TestFixedPhrases pins the exact wording of every entry in the inventory.
// They are passed to the LLM today and may be prepended mechanically later
// (D-37 (10)) — the wording must survive that move unchanged, so it is pinned
// here rather than in whichever caller currently uses it.
func TestFixedPhrases(t *testing.T) {
	for _, w := range fixedWordings() {
		t.Run(w.name, func(t *testing.T) {
			assert.Equal(t, w.whole, w.got)
			if w.noScan == "" {
				assert.Contains(t, w.whole, w.fragment,
					"fragment は全文の一部でなければ走査の意味が無い")
			}
		})
	}

	// 個々の文言だけでなく、回どうしの揃いも固定する。
	assert.True(t, strings.HasSuffix(QuizOnlyOutro(), signOff),
		"記事ゼロの日の締めも通常回と同じ署名で終わる")
}

// TestCornerLeadsShareOneWording pins D-37 (9) for corner openings: news,
// 復習, 書籍 のどのコーナーも cornerLead ただ1つを通る。テンプレート側や
// 別ファイルに同じ文を書き写すと、ここが落ちる前に静かに旧文言が残る。
func TestCornerLeadsShareOneWording(t *testing.T) {
	assert.Equal(t, "ここからは、いま読んでいる本のコーナーです。", bookReviewLead())
	assert.Equal(t, cornerLead("いま読んでいる本"), bookReviewLead())
	assert.True(t, strings.HasPrefix(quizCornerLead(3), cornerLead("復習")),
		"復習コーナーの入りも同じ定型句から作る")
}

// TestSpokenWordingLivesOnlyInFormatGo pins D-37 (9) structurally: 番組の文言は
// format.go にしか存在しない。走査対象は
//
//   - prompts/*.tmpl … テンプレートが持つのは「指示」だけで、台本に出る文言は
//     format.go からテンプレート変数として渡す
//   - パッケージ内の .go(format.go 自身とテストを除く)… D-37 (9) が挙げた
//     6箇所のうち Go 側の半分。文言を持ってよいのは format.go だけ
//
// 文言を書き写した瞬間に集約先が2つになり、format.go だけ直したときに片方が
// 静かに旧文言で残る — 実際に起きた退行(旧 bookreview.tmpl のコーナー導入文、
// shownotes.go の見出し)がこれである。
//
// **偽陽性が出たらアサーションを緩めない。** fragment を短くするのでも、
// ファイルを除外リストに足すのでもなく、引っかかった文言を format.go の変数に
// して呼び出し側へ渡すこと — それがこのテストが守っている不変条件である。
func TestSpokenWordingLivesOnlyInFormatGo(t *testing.T) {
	sources := map[string]string{}

	entries, err := promptFS.ReadDir("prompts")
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		body, err := promptFS.ReadFile("prompts/" + e.Name())
		require.NoError(t, err)
		sources["prompts/"+e.Name()] = string(body)
	}

	goFiles, err := os.ReadDir(".")
	require.NoError(t, err)
	for _, e := range goFiles {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") || name == "format.go" {
			continue
		}
		body, err := os.ReadFile(name)
		require.NoError(t, err)
		sources[name] = string(body)
	}
	require.Greater(t, len(sources), len(entries), "パッケージの .go も走査対象に入っていること")

	for _, w := range fixedWordings() {
		if w.noScan != "" {
			continue
		}
		t.Run(w.name, func(t *testing.T) {
			for path, body := range sources {
				assert.NotContains(t, body, w.fragment,
					"%s: 番組の文言は format.go だけが持つ (D-37 (9))。"+
						"この文言を変数にして呼び出し側へ渡すこと", path)
			}
		})
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
