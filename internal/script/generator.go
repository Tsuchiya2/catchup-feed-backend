package script

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// LLM is the text generator behind the script. It is satisfied by
// summarizer.Chain (D-3: 台本は要約と同一の Gemini→Groq→Ollama 連鎖).
// The second return value is the winning provider name (observability, §8).
type LLM interface {
	Generate(ctx context.Context, prompt string) (text string, provider string, err error)
}

// Generator turns planned articles into read-aloud segments (§6-2). One LLM
// call per segment (intro / each news corner / outro) keeps the output
// parse-free: whatever a provider returns is the script verbatim, so the
// Ollama fallback needs no structured-output discipline.
type Generator struct {
	llm      LLM
	showName string
	logger   *slog.Logger
}

// NewGenerator creates a Generator. showName appears in every prompt as the
// program name; a nil logger falls back to slog.Default().
func NewGenerator(llm LLM, showName string, logger *slog.Logger) *Generator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Generator{llm: llm, showName: showName, logger: logger}
}

// GenerateEpisode produces the ordered segments for one episode: intro,
// one news segment per featured article (with a transition referencing the
// previous corner — つなぎ文), and outro. Positions are 1-based. Any LLM
// failure aborts the whole episode (§8: 縮退はエピソード単位 — 当日スキップ).
func (g *Generator) GenerateEpisode(ctx context.Context, date time.Time, articles []repository.RadioArticle) ([]*entity.Segment, error) {
	if len(articles) == 0 {
		return nil, fmt.Errorf("script: no articles to script")
	}

	dateStr := formatDate(date)
	segments := make([]*entity.Segment, 0, len(articles)+2)

	introPrompt, err := renderPrompt("intro.tmpl", introData{
		ShowName:     g.showName,
		Date:         dateStr,
		Corners:      corners(articles),
		ArticleCount: len(articles),
	})
	if err != nil {
		return nil, err
	}
	introScript, err := g.generate(ctx, entity.SegmentKindIntro, introPrompt)
	if err != nil {
		return nil, err
	}
	segments = append(segments, &entity.Segment{
		Position: 1,
		Kind:     entity.SegmentKindIntro,
		Script:   introScript,
	})

	for i, article := range articles {
		prevTitle := ""
		if i > 0 {
			prevTitle = articles[i-1].Title
		}
		newsPrompt, err := renderPrompt("news.tmpl", newsData{
			ShowName:  g.showName,
			Category:  article.Category,
			Source:    article.SourceName,
			Title:     article.Title,
			Summary:   article.Summary, // C-12: 要約のみ。原文は渡らない
			PrevTitle: prevTitle,
			Position:  i + 1,
			Total:     len(articles),
		})
		if err != nil {
			return nil, err
		}
		newsScript, err := g.generate(ctx, entity.SegmentKindNews, newsPrompt)
		if err != nil {
			return nil, fmt.Errorf("article %d (%s): %w", article.ID, article.Title, err)
		}
		articleID := article.ID
		segments = append(segments, &entity.Segment{
			Position:  i + 2,
			Kind:      entity.SegmentKindNews,
			ArticleID: &articleID,
			Script:    newsScript,
		})
	}

	titles := make([]string, len(articles))
	for i, a := range articles {
		titles[i] = a.Title
	}
	outroPrompt, err := renderPrompt("outro.tmpl", outroData{
		ShowName: g.showName,
		Date:     dateStr,
		Titles:   titles,
	})
	if err != nil {
		return nil, err
	}
	outroScript, err := g.generate(ctx, entity.SegmentKindOutro, outroPrompt)
	if err != nil {
		return nil, err
	}
	segments = append(segments, &entity.Segment{
		Position: len(articles) + 2,
		Kind:     entity.SegmentKindOutro,
		Script:   outroScript,
	})

	return segments, nil
}

func (g *Generator) generate(ctx context.Context, kind, prompt string) (string, error) {
	text, provider, err := g.llm.Generate(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("script: generate %s segment: %w", kind, err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("script: generate %s segment: empty script", kind)
	}
	g.logger.InfoContext(ctx, "segment script generated",
		slog.String("kind", kind),
		slog.String("provider", provider),
		slog.Int("script_chars", len([]rune(text))))
	return text, nil
}
