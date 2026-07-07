package script_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
	"catchup-feed/internal/script"
)

// fakeLLM records every prompt and returns scripted responses in order.
type fakeLLM struct {
	prompts   []string
	responses []string
	errAt     int // 1-based call index that fails; 0 = never
	calls     int
}

func (f *fakeLLM) Generate(_ context.Context, prompt string) (string, string, error) {
	f.calls++
	f.prompts = append(f.prompts, prompt)
	if f.errAt != 0 && f.calls == f.errAt {
		return "", "", errors.New("all generate providers failed")
	}
	if len(f.responses) >= f.calls {
		return f.responses[f.calls-1], "gemini", nil
	}
	return fmt.Sprintf("原稿%d", f.calls), "gemini", nil
}

func radioArticles() []repository.RadioArticle {
	return []repository.RadioArticle{
		{ID: 10, Title: "Go 1.26 リリース", URL: "https://example.com/go", Category: "golang",
			SourceName: "Go Blog", Summary: "Go 1.26 の要約テキスト。", PublishedAt: day(1)},
		{ID: 20, Title: "新しい推論モデル", URL: "https://example.com/ai", Category: "ai",
			SourceName: "AI News", Summary: "推論モデルの要約テキスト。", PublishedAt: day(2)},
	}
}

func TestGenerator_GenerateEpisode_SegmentStructure(t *testing.T) {
	llm := &fakeLLM{}
	gen := script.NewGenerator(llm, "pulse", nil)

	segments, drafts, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles(), 0)
	require.NoError(t, err)
	require.Len(t, segments, 4, "intro + 2 news + outro")

	assert.Equal(t, entity.SegmentKindIntro, segments[0].Kind)
	assert.Equal(t, 1, segments[0].Position)
	assert.Nil(t, segments[0].ArticleID)

	assert.Equal(t, entity.SegmentKindNews, segments[1].Kind)
	assert.Equal(t, 2, segments[1].Position)
	require.NotNil(t, segments[1].ArticleID)
	assert.Equal(t, int64(10), *segments[1].ArticleID)

	assert.Equal(t, entity.SegmentKindNews, segments[2].Kind)
	require.NotNil(t, segments[2].ArticleID)
	assert.Equal(t, int64(20), *segments[2].ArticleID)

	assert.Equal(t, entity.SegmentKindOutro, segments[3].Kind)
	assert.Equal(t, 4, segments[3].Position)
	assert.Nil(t, segments[3].ArticleID)

	// LLM output is the script verbatim (parse-free design).
	assert.Equal(t, "原稿1", segments[0].Script)
	assert.Equal(t, "原稿2", segments[1].Script)
	assert.Nil(t, drafts, "quizCount=0 must never produce learning items")
}

// TestGenerator_PromptContainsSummaryOnly pins C-12: the news prompt embeds
// the summary body (and public metadata: title / source / category) — and
// nothing else article-derived. RadioArticle carries no content field, so
// the article text cannot leak into a cloud prompt by construction.
func TestGenerator_PromptContainsSummaryOnly(t *testing.T) {
	llm := &fakeLLM{}
	gen := script.NewGenerator(llm, "pulse", nil)

	_, _, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles(), 0)
	require.NoError(t, err)
	require.Len(t, llm.prompts, 4)

	newsPrompt := llm.prompts[1]
	assert.Contains(t, newsPrompt, "Go 1.26 の要約テキスト。", "summary body must be in the prompt")
	assert.Contains(t, newsPrompt, "Go 1.26 リリース")
	assert.Contains(t, newsPrompt, "Go Blog")
	assert.Contains(t, newsPrompt, "golang")

	// Structural pin: the script input type has no article-content field.
	typ := reflect.TypeOf(repository.RadioArticle{})
	_, hasContent := typ.FieldByName("Content")
	assert.False(t, hasContent,
		"repository.RadioArticle must not carry article content (C-12)")
}

func TestGenerator_TransitionReferencesPreviousCorner(t *testing.T) {
	llm := &fakeLLM{}
	gen := script.NewGenerator(llm, "pulse", nil)

	_, _, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles(), 0)
	require.NoError(t, err)

	assert.NotContains(t, llm.prompts[1], "直前のコーナー",
		"first news segment has no previous corner")
	assert.Contains(t, llm.prompts[2], "直前のコーナーでは「Go 1.26 リリース」",
		"second news segment must reference the previous article (つなぎ文)")
}

func TestGenerator_IntroAndOutroPrompts(t *testing.T) {
	llm := &fakeLLM{}
	gen := script.NewGenerator(llm, "pulse", nil)

	_, _, err := gen.GenerateEpisode(context.Background(), time.Date(2026, 7, 5, 4, 30, 0, 0, time.UTC), radioArticles(), 0)
	require.NoError(t, err)

	intro := llm.prompts[0]
	assert.Contains(t, intro, "2026年7月5日")
	assert.Contains(t, intro, "pulse")
	assert.Contains(t, intro, "golang")
	assert.Contains(t, intro, "ai")
	assert.Contains(t, intro, "2件")

	outro := llm.prompts[3]
	assert.Contains(t, outro, "Go 1.26 リリース")
	assert.Contains(t, outro, "新しい推論モデル")
}

func TestGenerator_Errors(t *testing.T) {
	tests := []struct {
		name    string
		llm     *fakeLLM
		wantSub string
	}{
		{name: "intro generation fails", llm: &fakeLLM{errAt: 1}, wantSub: "intro segment"},
		{name: "news generation fails", llm: &fakeLLM{errAt: 2}, wantSub: "news segment"},
		{name: "outro generation fails", llm: &fakeLLM{errAt: 4}, wantSub: "outro segment"},
		{name: "empty script is an error", llm: &fakeLLM{responses: []string{"   "}}, wantSub: "empty script"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := script.NewGenerator(tt.llm, "pulse", nil)
			segments, drafts, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles(), 0)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSub)
			assert.Nil(t, segments)
			assert.Nil(t, drafts)
		})
	}
}

func TestGenerator_NoArticles(t *testing.T) {
	gen := script.NewGenerator(&fakeLLM{}, "pulse", nil)
	_, _, err := gen.GenerateEpisode(context.Background(), day(4), nil, 0)
	assert.Error(t, err)
}

// TestGenerator_OutroPromptUnchangedWithoutQuiz pins Phase 3 §12-1(公開
// 版の回帰なし): with quizCount=0 — the only mode the public pipeline used
// before Phase 3, and every backpressure/dedupe/disabled day after — the
// outro prompt renders byte-identically to the pre-Phase 3 template. A
// golden string, not Contains: any drift in the shared template shows up
// here first.
func TestGenerator_OutroPromptUnchangedWithoutQuiz(t *testing.T) {
	llm := &fakeLLM{}
	gen := script.NewGenerator(llm, "pulse", nil)

	_, _, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles(), 0)
	require.NoError(t, err)
	require.Len(t, llm.prompts, 4)

	const golden = `あなたは技術ニュースを毎朝届けるラジオ番組「pulse」のパーソナリティです。
2026年7月4日放送分のクロージング原稿を書いてください。

今日紹介した記事:
- Go 1.26 リリース
- 新しい推論モデル

条件:
- 今日の内容の短い振り返りと締めの挨拶を100文字程度で。次回への一言も添える。
- 出力は読み上げ原稿の本文のみ。見出し・箇条書き・記号・注釈は書かない。
- 音声合成でそのまま読み上げるため、URL や英数字の羅列を避け、自然な日本語にする。
`
	assert.Equal(t, golden, llm.prompts[3],
		"quizCount=0 must render the exact pre-Phase 3 outro prompt (§12-1)")
}

// TestGenerator_QuizPiggyback covers the D-19 happy path: one extra
// section on the outro call (LLM 呼び出し回数は不変), the marker split
// keeping the broadcast outro clean, and the 記事番号 → article_id
// recovery with the actually-responding provider attached.
func TestGenerator_QuizPiggyback(t *testing.T) {
	llm := &fakeLLM{responses: []string{
		"イントロ。", "ニュース1。", "ニュース2。",
		"アウトロ本文。\n\n===LEARNING_ITEMS===\n" +
			"記事番号: 2\n概念: 蒸留による推論モデルの小型化\n" +
			"問題: 昨日のニュースで触れた新しい推論モデルですが、小型化の鍵は何だったでしょうか。\n" +
			"答え: 蒸留です。大きなモデルの知識を小さなモデルに移して計算資源を節約するのがポイントでした。",
	}}
	gen := script.NewGenerator(llm, "pulse", nil)

	segments, drafts, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles(), 1)
	require.NoError(t, err)
	require.Len(t, segments, 4, "学習項目セクションはセグメントを増やさない")
	assert.Equal(t, 4, llm.calls, "D-19: 相乗り — no extra LLM call (§12-3)")

	// The broadcast outro carries neither the marker nor the quiz text.
	outro := segments[3]
	assert.Equal(t, entity.SegmentKindOutro, outro.Kind)
	assert.Equal(t, "アウトロ本文。", outro.Script)

	require.Len(t, drafts, 1)
	assert.Equal(t, int64(20), drafts[0].ArticleID, "記事番号2 → 2件目の article_id")
	assert.Equal(t, "gemini", drafts[0].Provider, "provider is the LLM that actually answered")
	assert.Equal(t, "蒸留による推論モデルの小型化", drafts[0].Concept)
	assert.Contains(t, drafts[0].Question, "昨日のニュースで触れた")
	assert.Contains(t, drafts[0].Answer, "蒸留です")

	// The section rides on the outro prompt only; intro/news are untouched.
	outroPrompt := llm.prompts[3]
	assert.Contains(t, outroPrompt, "===LEARNING_ITEMS===")
	assert.Contains(t, outroPrompt, "記事1: Go 1.26 リリース")
	assert.Contains(t, outroPrompt, "要約: Go 1.26 の要約テキスト。")
	assert.Contains(t, outroPrompt, "記事2: 新しい推論モデル")
	assert.Contains(t, outroPrompt, "1件選び")
	assert.Contains(t, outroPrompt, "放送済みであることを前提にしたラジオ口調")
	for i, name := range []string{"intro", "news1", "news2"} {
		assert.NotContains(t, llm.prompts[i], "学習項目", "%s prompt must stay unchanged (§12-1)", name)
	}
}

// TestGenerator_QuizDegradation pins §5.1: every quiz-side deviation of
// the model output degrades to "no items today" — never to an error, and
// never into the broadcast script.
func TestGenerator_QuizDegradation(t *testing.T) {
	tests := []struct {
		name      string
		outro     string
		wantOutro string
	}{
		{
			name:      "marker missing — whole output is the outro",
			outro:     "マーカーを忘れたアウトロ。",
			wantOutro: "マーカーを忘れたアウトロ。",
		},
		{
			name:      "section present but unparseable",
			outro:     "アウトロ。\n===LEARNING_ITEMS===\nろくでもない自由文だけが続く。",
			wantOutro: "アウトロ。",
		},
		{
			name:      "section with out-of-range article number",
			outro:     "アウトロ。\n===LEARNING_ITEMS===\n記事番号: 9\n概念: c\n問題: q\n答え: a",
			wantOutro: "アウトロ。",
		},
		{
			// レビュー差し戻し B-1: マーカー表記崩れ(空白入り)。分割は
			// found=false に倒れるが、クイズ本文が公開 outro に残っては
			// ならない — stripQuizLeak が切り落とす。
			name:      "marker mangled with whitespace, items present — body truncated",
			outro:     "アウトロ。\n=== LEARNING_ITEMS ===\n記事番号: 1\n概念: c\n問題: q\n答え: a",
			wantOutro: "アウトロ。",
		},
		{
			// レビュー差し戻し B-1: マーカー省略で項目直書き(フォール
			// バック末端の Ollama で最も起きやすい逸脱)。
			name:      "marker omitted, items written directly — body truncated",
			outro:     "アウトロ。\n記事番号: 1\n概念: c\n問題: q\n答え: a",
			wantOutro: "アウトロ。",
		},
		{
			name:      "marker omitted, items with full-width colon — body truncated",
			outro:     "アウトロ。\n記事番号:1\n概念:c\n問題:q\n答え:a",
			wantOutro: "アウトロ。",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := &fakeLLM{responses: []string{"イントロ。", "ニュース1。", "ニュース2。", tt.outro}}
			gen := script.NewGenerator(llm, "pulse", nil)

			segments, drafts, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles(), 1)
			require.NoError(t, err, "quiz-side failures must not abort the episode (§5.1)")
			require.Len(t, segments, 4)
			assert.Equal(t, tt.wantOutro, segments[3].Script)
			assert.Empty(t, drafts)
		})
	}
}

// TestGenerator_QuizEmptyOutroBodyFails: a response with no closing script
// at all — it starts with the marker, or the §12-1 leak truncation removed
// everything. That is a script generation failure (empty script と同類),
// not a quiz degradation — the day is skipped rather than shipping a
// truncated show.
func TestGenerator_QuizEmptyOutroBodyFails(t *testing.T) {
	tests := []struct {
		name  string
		outro string
	}{
		{
			name:  "response starts with the marker",
			outro: "===LEARNING_ITEMS===\n記事番号: 1\n概念: c\n問題: q\n答え: a",
		},
		{
			name:  "marker omitted and the whole response is item lines",
			outro: "記事番号: 1\n概念: c\n問題: q\n答え: a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := &fakeLLM{responses: []string{"イントロ。", "ニュース1。", "ニュース2。", tt.outro}}
			gen := script.NewGenerator(llm, "pulse", nil)

			segments, drafts, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles(), 1)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "empty script")
			assert.Nil(t, segments)
			assert.Nil(t, drafts)
		})
	}
}

// TestGenerator_QuizLeakBeforeValidMarker: a model that writes item lines
// BEFORE a well-formed marker section. The section still parses (items are
// kept), but the leaked copy is truncated out of the broadcast body — the
// two nets operate independently (§12-1).
func TestGenerator_QuizLeakBeforeValidMarker(t *testing.T) {
	llm := &fakeLLM{responses: []string{"イントロ。", "ニュース1。", "ニュース2。",
		"アウトロ。\n記事番号: 1\n概念: c1\n問題: q1\n答え: a1\n" +
			"===LEARNING_ITEMS===\n記事番号: 2\n概念: c2\n問題: q2\n答え: a2"}}
	gen := script.NewGenerator(llm, "pulse", nil)

	segments, drafts, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles(), 1)
	require.NoError(t, err)
	require.Len(t, segments, 4)
	assert.Equal(t, "アウトロ。", segments[3].Script, "leaked item lines must not reach the broadcast")
	require.Len(t, drafts, 1, "the marker section itself still yields the item")
	assert.Equal(t, int64(20), drafts[0].ArticleID)
}
