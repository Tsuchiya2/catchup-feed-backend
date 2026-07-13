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
//
// quizCount > 0 piggybacks the Phase 3 learning-item request onto the
// outro call (D-19: 相乗り、LLM 呼び出し回数の増分ゼロ — Phase 3 §12-3):
// the model picks the quizCount articles with the largest technical
// takeaway and drafts one quiz each, returned as QuizDrafts. The section
// is split off by marker before the outro reaches the broadcast script;
// any parse failure degrades to zero drafts and never errors (Phase 3
// §5.1: 縮退の方向は「クイズなし」、放送は止めない). quizCount <= 0
// renders the exact pre-Phase 3 outro prompt and returns nil drafts.
func (g *Generator) GenerateEpisode(ctx context.Context, date time.Time, articles []repository.RadioArticle, quizCount int) ([]*entity.Segment, []QuizDraft, error) {
	if len(articles) == 0 {
		return nil, nil, fmt.Errorf("script: no articles to script")
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
		return nil, nil, err
	}
	introScript, err := g.generate(ctx, entity.SegmentKindIntro, introPrompt)
	if err != nil {
		return nil, nil, err
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
			return nil, nil, err
		}
		newsScript, err := g.generate(ctx, entity.SegmentKindNews, newsPrompt)
		if err != nil {
			return nil, nil, fmt.Errorf("article %d (%s): %w", article.ID, article.Title, err)
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
	outroScript, drafts, err := g.generateOutro(ctx, outroData{
		ShowName: g.showName,
		Date:     dateStr,
		Titles:   titles,
		Quiz:     quizPrompt(articles, quizCount),
	}, articles, quizCount)
	if err != nil {
		return nil, nil, err
	}
	segments = append(segments, &entity.Segment{
		Position: len(articles) + 2,
		Kind:     entity.SegmentKindOutro,
		Script:   outroScript,
	})

	return segments, drafts, nil
}

// quizPrompt builds the learning-item section data for the outro prompt.
// count <= 0 returns nil, which renders outro.tmpl exactly as before the
// Phase 3 extension — this nil is the backpressure/duplicate-guard switch
// (§5.2: プロンプト側で抑止、トークンも消費しない).
func quizPrompt(articles []repository.RadioArticle, count int) *quizPromptData {
	if count <= 0 {
		return nil
	}
	entries := make([]quizPromptArticle, len(articles))
	for i, a := range articles {
		entries[i] = quizPromptArticle{Number: i + 1, Title: a.Title, Summary: a.Summary}
	}
	return &quizPromptData{Count: count, Marker: quizSectionMarker, Articles: entries}
}

// generateOutro renders the outro prompt from data, runs the (possibly
// piggybacked) call and separates the broadcast script from the
// learning-item section. Quiz-side failures — missing marker, unparseable
// blocks — degrade to nil drafts with a warning (§5.1), and stripQuizLeak
// additionally truncates any item text that a marker-mangling model left
// inside the body (§12-1: 公開台本への混入の構造的遮断).
//
// An empty outro body — natively empty or emptied by the truncation — has
// one more degradation rung when the prompt carried the quiz section
// (D-26 (1), 2026-07-13 欠番障害の恒久対応): the composite format itself
// can be what defeated the model (実測: Ollama まで縮退した日は放送本文が
// 空になる), so the outro is regenerated exactly once with the quiz-less
// pre-Phase 3 prompt (Quiz=nil) and the day's item generation is skipped —
// the same "クイズなし" direction as every §5.1 degradation, keeping the
// broadcast alive (§9). Only if that retry also comes back empty (or the
// prompt was quiz-less to begin with) is it a script generation failure:
// without a closing script there is no episode to ship, so the day is
// skipped (§8) rather than broadcasting a truncated show.
func (g *Generator) generateOutro(ctx context.Context, data outroData, articles []repository.RadioArticle, quizCount int) (string, []QuizDraft, error) {
	prompt, err := renderPrompt("outro.tmpl", data)
	if err != nil {
		return "", nil, err
	}
	raw, provider, err := g.llm.Generate(ctx, prompt)
	if err != nil {
		return "", nil, fmt.Errorf("script: generate outro segment: %w", err)
	}

	body := raw
	var drafts []QuizDraft
	if quizCount > 0 {
		var section string
		var found bool
		body, section, found = cutQuizSection(raw)
		switch {
		case !found:
			g.logger.WarnContext(ctx, "learning-item section missing from outro output, skipping today's item generation (§5.1)",
				slog.String("provider", provider))
		default:
			drafts = parseQuizItems(section, articles, quizCount, provider, g.logger)
			if len(drafts) == 0 {
				g.logger.WarnContext(ctx, "learning-item section yielded no valid item, skipping today's item generation (§5.1)",
					slog.String("provider", provider))
			}
		}
		// §12-1 の安全ネット: マーカー表記を崩したモデル(空白入り
		// マーカー、マーカー省略で項目直書き)が残した学習項目の痕跡を
		// 放送原稿から切り落とす。マーカー分割の found に依らず必ず通す —
		// found=true でもマーカー前に項目が書かれる逸脱はあり得る。項目
		// 側は上の縮退のまま(クイズなしで放送継続)とし、公開台本への
		// 混入だけを構造的に遮断する。切断後が空なら下の empty-script
		// エラー = 当日スキップ (§8)。
		if clean, leaked := stripQuizLeak(body); leaked {
			g.logger.WarnContext(ctx, "learning-item text leaked into the outro body, truncated (§12-1)",
				slog.String("provider", provider),
				slog.Int("removed_chars", len([]rune(body))-len([]rune(clean))))
			body = clean
		}
	}

	body = strings.TrimSpace(body)
	if body == "" && quizCount > 0 {
		// D-26 (1): クイズ相乗りが本文を空にした — 複合フォーマット自体が
		// 敗因の可能性が高い(実測 2026-07-13: Ollama 縮退日に決定論的再現)。
		// クイズなしの旧プロンプトで1回だけ再生成し、当日の学習項目生成は
		// スキップ(§5.1 と同じ「クイズなし」への縮退。drafts は捨てる —
		// 本文が空の応答から拾えた項目を、再試行で成った放送に紐付けない)。
		g.logger.WarnContext(ctx, "piggybacked outro body was empty, retrying once without the quiz section (D-26)",
			slog.String("provider", provider),
			slog.Bool("quizless_retry", true))
		data.Quiz = nil
		drafts = nil
		prompt, err = renderPrompt("outro.tmpl", data)
		if err != nil {
			return "", nil, err
		}
		raw, provider, err = g.llm.Generate(ctx, prompt)
		if err != nil {
			return "", nil, fmt.Errorf("script: generate outro segment: %w", err)
		}
		body = strings.TrimSpace(raw)
	}
	if body == "" {
		return "", nil, fmt.Errorf("script: generate outro segment: empty script")
	}
	g.logger.InfoContext(ctx, "segment script generated",
		slog.String("kind", entity.SegmentKindOutro),
		slog.String("provider", provider),
		slog.Int("script_chars", len([]rune(body))),
		slog.Int("learning_items", len(drafts)))
	return body, drafts, nil
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
