package script

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/infra/summarizer"
)

// *summarizer.Ollama must satisfy OllamaLLM — this is the local-only path
// (§12-4). The cloud chain *summarizer.Chain deliberately CANNOT: its Generate
// returns three values (text, provider, error), so it fails to implement this
// two-value interface at compile time. Book text can therefore never be passed
// to the fallback chain — the type system forbids it.
var _ OllamaLLM = (*summarizer.Ollama)(nil)

// fakeOllama is the local-model stand-in for book_review tests.
type fakeOllama struct {
	out     string
	err     error
	prompts []string
}

func (f *fakeOllama) Generate(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	if f.err != nil {
		return "", f.err
	}
	return f.out, f.err
}

func TestBookReviewGenerator_Generate(t *testing.T) {
	chunks := []BookChunk{
		{Position: 3, Content: "チャネルはゴルーチン間の通信路である。"},
		{Position: 4, Content: "バッファ付きチャネルは容量まで非同期に送れる。"},
	}

	tests := []struct {
		name       string
		out        string
		llmErr     error
		wantErr    bool
		wantScript string
		wantQuiz   *BookQuizDraft
		wantNoQuiz bool
	}{
		{
			name: "script and quiz parsed",
			out: "今日は、いま読んでいる本のコーナーです。チャネルの話をします。\n" +
				bookQuizMarker + "\n" +
				"概念: チャネルの役割\n" +
				"問題: ゴルーチン同士が値をやり取りする仕組みは何でしょう。\n" +
				"答え: チャネルです。送受信で同期もとれます。",
			wantScript: "今日は、いま読んでいる本のコーナーです。チャネルの話をします。",
			wantQuiz: &BookQuizDraft{
				Concept:  "チャネルの役割",
				Question: "ゴルーチン同士が値をやり取りする仕組みは何でしょう。",
				Answer:   "チャネルです。送受信で同期もとれます。",
			},
		},
		{
			name:       "missing marker degrades to review without quiz (§5.3)",
			out:        "今日は本のコーナーです。マーカーを書き忘れました。",
			wantScript: "今日は本のコーナーです。マーカーを書き忘れました。",
			wantNoQuiz: true,
		},
		{
			name:       "incomplete quiz fields degrade to nil quiz (§5.3)",
			out:        "本の紹介です。\n" + bookQuizMarker + "\n概念: 見出しだけ\n問題: 本文なし",
			wantScript: "本の紹介です。",
			wantNoQuiz: true,
		},
		{
			name: "full-width colon and wrapped lines tolerated",
			out: "紹介本文です。\n" + bookQuizMarker + "\n" +
				"概念： 全角コロン\n" +
				"問題： 一行目\n続きの折り返し\n" +
				"答え： 答えです。",
			wantScript: "紹介本文です。",
			wantQuiz: &BookQuizDraft{
				Concept:  "全角コロン",
				Question: "一行目\n続きの折り返し",
				Answer:   "答えです。",
			},
		},
		{
			name:    "ollama error is returned (caller skips book_review, §7.3)",
			llmErr:  errors.New("connection refused"),
			wantErr: true,
		},
		{
			name:    "empty review body is an error",
			out:     "   \n" + bookQuizMarker + "\n概念: c\n問題: q\n答え: a",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			llm := &fakeOllama{out: tt.out, err: tt.llmErr}
			g := NewBookReviewGenerator(llm, nil)

			res, err := g.Generate(context.Background(), "Learning Go", chunks)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantScript, res.Script)
			if tt.wantNoQuiz {
				assert.Nil(t, res.Quiz)
			} else {
				require.NotNil(t, res.Quiz)
				assert.Equal(t, tt.wantQuiz, res.Quiz)
			}

			// The prompt must carry the book title and every chunk's content
			// (private book text → local model only, checked structurally by
			// the OllamaLLM type; here we confirm the content is actually fed).
			require.Len(t, llm.prompts, 1)
			assert.Contains(t, llm.prompts[0], "Learning Go")
			for _, c := range chunks {
				assert.Contains(t, llm.prompts[0], c.Content)
			}
			assert.Contains(t, llm.prompts[0], bookQuizMarker,
				"prompt instructs the model to emit the marker")
		})
	}
}

// TestBookReviewGenerator_PromptCarriesTheProgramFormat pins that the 書籍
// コーナー goes through the same D-37 番組フォーマット as every other prompt:
// the shared persona block and the corner-opening 定型句 from format.go.
// Ollama がこの指示に従いきれないことは許容する(§12-4)が、指示を渡さない
// のは別問題 — テンプレートから persona や Lead が抜けたらここで落ちる。
func TestBookReviewGenerator_PromptCarriesTheProgramFormat(t *testing.T) {
	llm := &fakeOllama{out: "本文。"}
	g := NewBookReviewGenerator(llm, nil)

	_, err := g.Generate(context.Background(), "Learning Go", []BookChunk{{Position: 1, Content: "抜粋"}})
	require.NoError(t, err)
	require.Len(t, llm.prompts, 1)
	prompt := llm.prompts[0]

	assert.Contains(t, prompt, "ラジオ番組「キャッチアップフィード」", "persona ブロックが適用されている")
	assert.Contains(t, prompt, "「パーソナリティの〜」", "名乗りの禁止 (D-37 (1))")
	assert.Contains(t, prompt, "感嘆符は使わない", "口調の指定 (D-37 (2))")
	assert.NotContains(t, prompt, "catchup-feed", "ASCII の番組名は台本に出さない (D-37 (3))")
	assert.Contains(t, prompt, bookReviewLead(), "コーナー導入の定型句は format.go から渡す")
}

// TestBookReviewGenerator_NoChunks: an empty chunk set is a programming error
// (the caller must not reach generation with nothing to review), returned as
// an error rather than an Ollama call.
func TestBookReviewGenerator_NoChunks(t *testing.T) {
	llm := &fakeOllama{out: "unused"}
	g := NewBookReviewGenerator(llm, nil)

	_, err := g.Generate(context.Background(), "Learning Go", nil)
	require.Error(t, err)
	assert.Empty(t, llm.prompts, "no Ollama call when there is nothing to review")
}

// TestBookReviewGenerator_QuizStaysOutOfBody pins that the review script never
// carries the quiz text once a well-formed marker is present (§5.3 の相乗り
// 分離). book_review is private-only, but keeping the quiz out of the read
// script is still correct.
func TestBookReviewGenerator_QuizStaysOutOfBody(t *testing.T) {
	out := "本文。ここまでが紹介です。\n" + bookQuizMarker + "\n概念: c\n問題: q\n答え: a"
	g := NewBookReviewGenerator(&fakeOllama{out: out}, nil)

	res, err := g.Generate(context.Background(), "本", []BookChunk{{Position: 0, Content: "x"}})
	require.NoError(t, err)
	assert.False(t, strings.Contains(res.Script, "概念"), "quiz labels must not leak into the read script")
	assert.False(t, strings.Contains(res.Script, bookQuizMarker))
}
