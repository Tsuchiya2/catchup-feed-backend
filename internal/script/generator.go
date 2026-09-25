package script

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
	utiltext "catchup-feed/internal/utils/text"
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
	llm    LLM
	logger *slog.Logger
	// quizLimits bounds the piggybacked quiz section of the outro prompt so
	// one outro request stays inside the Groq free-tier TPM ceiling
	// (D-46 (1)). The zero value means the built-in defaults.
	quizLimits OutroQuizLimits
}

// NewGenerator creates a Generator; a nil logger falls back to slog.Default()
// and a zero-valued quizLimits to DefaultOutroQuizLimits (D-46 (1)).
//
// 番組名は引数に取らない: 台本に出る番組名は format.go の spokenShowName に
// 固定されており、RADIO_SHOW_NAME(ASCII、エピソードタイトルと ID3 用)を
// プロンプトへ流し込む経路をここで断っている (D-37 (3))。
func NewGenerator(llm LLM, logger *slog.Logger, quizLimits OutroQuizLimits) *Generator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Generator{llm: llm, logger: logger, quizLimits: quizLimits.withDefaults()}
}

// GenerateEpisode produces the ordered segments for one episode: intro,
// one news segment per featured article (each opened by the fixed corner
// 定型句 — D-37 (6): つなぎ文は廃止), and outro. Positions are 1-based. Any
// LLM failure aborts the whole episode (§8: 縮退はエピソード単位 — 当日スキップ).
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

	g.warnUnknownCorners(ctx, articles)

	dateStr := spokenDate(date)
	cornerList := cornerNames(articles)
	segments := make([]*entity.Segment, 0, len(articles)+2)

	introPrompt, err := renderPrompt("intro.tmpl", introData{
		Date:         dateStr,
		Corners:      cornerList,
		ArticleCount: len(articles),
		Lead:         openingLead(date),
		Handoff:      openingHandoff,
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
		corner := cornerName(article.Category)
		newsPrompt, err := renderPrompt("news.tmpl", newsData{
			Corner:   corner,
			Source:   article.SourceName,
			Title:    article.Title,
			Summary:  article.Summary, // C-12: 要約のみ。原文は渡らない
			Lead:     newsLead(articles, i, corner),
			Position: i + 1,
			Total:    len(articles),
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

	// D-46 (1): クイズ相乗りセクションは当日の**全記事**を候補に出したまま、
	// 各要約を文字数で切り詰める。要約を丸ごと埋め込むと8記事日のアウトロが
	// Groq 無料枠の TPM 8,000 を1リクエストで超え(413、待っても通らない)、
	// Ollama 段しか残らない — 2026-09-25 欠番の構造。
	//
	// 記事の件数は絞らない: plan.Plan() は featured をカテゴリのスラッグ辞書順
	// に並べ替えるため、先頭N件に絞ると後ろのコーナーが構造的に一度も
	// 出題されない(当初実装のレビュー指摘、D-46 (1) 改訂)。
	quiz, summaryChars, outcome := quizPrompt(articles, quizCount, g.quizLimits)
	switch outcome {
	case quizPromptShortened:
		// RADIO_MAX_ARTICLES を大きく上げた日の安全弁が効いた。通常運転では
		// 出ない WARN なので、出たら記事数か要約長の設定を見直す合図。
		g.logger.WarnContext(ctx, "outro quiz summaries shortened to fit the prompt budget (D-46 (1))",
			slog.Int("articles", len(articles)),
			slog.Int("configured_summary_chars", g.quizLimits.SummaryChars),
			slog.Int("effective_summary_chars", summaryChars))
	case quizPromptOmitted:
		// 下限まで縮めても予算に収まらない = 記事数が極端。巨大なプロンプトを
		// 投げて 413 でエピソードごと落とすより、当日の学習項目を諦めて放送を
		// 出す(§5.2 と同じ「クイズなし」への縮退)。RADIO_MAX_ARTICLES の
		// 運用ミスを気づけるように、上とは別メッセージで残す。
		g.logger.WarnContext(ctx, "outro quiz section omitted: candidate list cannot fit the prompt budget even at the minimum summary length (D-46 (1))",
			slog.Int("articles", len(articles)),
			slog.Int("configured_summary_chars", g.quizLimits.SummaryChars),
			slog.Int("min_summary_chars", minQuizPromptSummaryChars),
			slog.Int("budget_chars", quizPromptListBudgetChars),
			slog.String("hint", "RADIO_MAX_ARTICLES が大きすぎる可能性がある"))
	case quizPromptDisabled, quizPromptFull:
		// 通常運転(QUIZ_ITEMS_PER_DAY=0 の日常運転を含む)。ログは出さない。
	}
	// generateOutro の契約は「quizCount > 0 ⟺ プロンプトに相乗りセクションが
	// ある」。予算で省いた日(quiz == nil)にそのまま quizCount を渡すと、存在
	// しないマーカーを探して「section missing」の WARN を出し、本文が空なら
	// 意味のない D-26 (1) 再試行(同一プロンプト)まで走ってしまう。渡すのは
	// 実際にプロンプトへ載った件数にする。
	effectiveQuizCount := quizCount
	if quiz == nil {
		effectiveQuizCount = 0
	}
	outroScript, drafts, err := g.generateOutro(ctx, outroData{
		Date:         dateStr,
		Corners:      cornerList,
		ArticleCount: len(articles),
		SignOff:      closingSignOff,
		Quiz:         quiz,
	}, articles, effectiveQuizCount)
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
// broadcast alive (§9). The retry body passes through stripQuizLeak too —
// the model that just deviated into the composite format is the last one
// to be trusted not to volunteer item lines again. Only if that retry also
// ends up empty — natively or after the truncation — (or the prompt was
// quiz-less to begin with) is it a script generation failure:
// without a closing script there is no episode to ship, so the day is
// skipped (§8) rather than broadcasting a truncated show.
// scope is the candidate list the prompt presented (D-46 (1): 当日の全記事);
// 記事番号はこの並びの1始まりなので、パーサへ渡すのも同じスライスでなければ
// ならない。
func (g *Generator) generateOutro(ctx context.Context, data outroData, scope []repository.RadioArticle, quizCount int) (string, []QuizDraft, error) {
	prompt, err := renderPrompt("outro.tmpl", data)
	if err != nil {
		return "", nil, err
	}
	// D-46 (1): アウトロは連鎖の中で最も大きい単一プロンプトで、Groq 無料枠の
	// TPM 8,000 を1リクエストで超えると 413 で即拒否される(待っても通らない)。
	// 縮小が効いているかを毎朝のログで確認できるようにサイズを残す。dry-run
	// でも同一のレンダリングを通るので、調整時の実測値もここに出る。
	g.logger.InfoContext(ctx, "outro prompt rendered",
		slog.Int("prompt_chars", utiltext.CountRunes(prompt)),
		slog.Int("quiz_candidates", len(scope)),
		slog.Int("quiz_count", quizCount))
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
			drafts = parseQuizItems(section, scope, quizCount, provider, g.logger)
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
		body = raw
		// 再試行プロンプトにマーカーもクイズ指示も存在しないが、§12-1 の
		// 設計前提は「モデルは指示から逸脱する」— しかもここに来るのは
		// 直前に複合フォーマットへ逸脱したばかりのモデルである。プライマリ
		// 経路と同じ stripQuizLeak で公開台本への混入を遮断する(切断で
		// 空になれば下の empty script = 当日スキップ §8)。なお通常の
		// quizCount<=0 経路(QUIZ_ITEMS_PER_DAY=0 の日常運転)は従来
		// どおり素通し — 同一実行内にクイズ指示が一度も存在しない文脈で、
		// この遮断の対象外。
		if clean, leaked := stripQuizLeak(body); leaked {
			g.logger.WarnContext(ctx, "learning-item text leaked into the outro body, truncated (§12-1)",
				slog.String("provider", provider),
				slog.Bool("quizless_retry", true),
				slog.Int("removed_chars", len([]rune(body))-len([]rune(clean))))
			body = clean
		}
		body = strings.TrimSpace(body)
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

// warnUnknownCorners logs the category slugs missing from the spoken-name
// table (D-37 (4)). Unknown slugs fall through to the raw slug — i.e. ASCII
// reaches the TTS — so this WARN is the signal to add a row to
// cornerNameBySlug. Deliberately not an error: a source category added from
// the dashboard must never take the broadcast down (§8).
func (g *Generator) warnUnknownCorners(ctx context.Context, articles []repository.RadioArticle) {
	seen := make(map[string]bool, len(articles))
	for _, a := range articles {
		if isKnownCorner(a.Category) || seen[a.Category] {
			continue
		}
		seen[a.Category] = true
		g.logger.WarnContext(ctx, "unknown source category, corner name falls back to the raw slug (D-37 (4): 対応表に追記が必要)",
			slog.String("category", a.Category))
	}
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
