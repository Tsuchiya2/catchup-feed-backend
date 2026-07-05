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

	segments, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles())
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
}

// TestGenerator_PromptContainsSummaryOnly pins C-12: the news prompt embeds
// the summary body (and public metadata: title / source / category) — and
// nothing else article-derived. RadioArticle carries no content field, so
// the article text cannot leak into a cloud prompt by construction.
func TestGenerator_PromptContainsSummaryOnly(t *testing.T) {
	llm := &fakeLLM{}
	gen := script.NewGenerator(llm, "pulse", nil)

	_, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles())
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

	_, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles())
	require.NoError(t, err)

	assert.NotContains(t, llm.prompts[1], "直前のコーナー",
		"first news segment has no previous corner")
	assert.Contains(t, llm.prompts[2], "直前のコーナーでは「Go 1.26 リリース」",
		"second news segment must reference the previous article (つなぎ文)")
}

func TestGenerator_IntroAndOutroPrompts(t *testing.T) {
	llm := &fakeLLM{}
	gen := script.NewGenerator(llm, "pulse", nil)

	_, err := gen.GenerateEpisode(context.Background(), time.Date(2026, 7, 5, 4, 30, 0, 0, time.UTC), radioArticles())
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
			segments, err := gen.GenerateEpisode(context.Background(), day(4), radioArticles())
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSub)
			assert.Nil(t, segments)
		})
	}
}

func TestGenerator_NoArticles(t *testing.T) {
	gen := script.NewGenerator(&fakeLLM{}, "pulse", nil)
	_, err := gen.GenerateEpisode(context.Background(), day(4), nil)
	assert.Error(t, err)
}
