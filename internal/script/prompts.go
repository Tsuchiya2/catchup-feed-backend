// Package script builds the radio episode script (§3.2 internal/script:
// セグメント選定・順序・定型句の組み立て). The LLM input is the article summary
// only — never the extracted article content (C-12); this is enforced
// structurally by repository.RadioArticle carrying no content field.
//
// Prompts live as files under prompts/ and are embedded at build time, so
// prompt tuning is versioned like code (§6-2). プロンプトが持つのは「指示」
// だけで、台本に出る言い回し(番組名・日付・コーナー名・定型句)は format.go
// が持ちテンプレート変数として渡す (D-37 (9)(10))。共通のペルソナ・口調・
// 禁止事項は prompts/common.tmpl の "persona" ブロック1箇所にまとめてある。
package script

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed prompts/*.tmpl
var promptFS embed.FS

var promptTemplates = template.Must(template.ParseFS(promptFS, "prompts/*.tmpl"))

// introData feeds prompts/intro.tmpl. Lead / Handoff are the fixed opening and
// closing sentences from format.go; Corners are already converted to spoken
// names (D-37 (4): 生スラッグをプロンプトに出さない).
type introData struct {
	programFormat
	Date         string
	Corners      []string
	ArticleCount int
	Lead         string
	Handoff      string
}

// newsData feeds prompts/news.tmpl. Summary is the only article-derived body
// text (C-12). Lead is this segment's fixed opening sentence — コーナー継続か
// 切替かで出し分けたもの (D-37 (6): つなぎ文は廃止し、直前の記事には触れさせ
// ない)。Corner is the spoken corner name, not the DB slug.
type newsData struct {
	programFormat
	Corner   string
	Source   string
	Title    string
	Summary  string
	Lead     string
	Position int
	Total    int
}

// outroData feeds prompts/outro.tmpl. 記事タイトルは渡さない — クロージングは
// 総括のみで、全件はショーノートに載る (D-37 (7))。SignOff is the fixed last
// sentence. Quiz enables the Phase 3 learning item section piggybacked on the
// outro call (D-19: 台本生成と同一 LLM 呼び出しに相乗り — クオータ純増ゼロ);
// nil renders the outro prompt without that section, which is what keeps the
// public episode free of any regression (Phase 3 §12-1).
type outroData struct {
	programFormat
	Date         string
	Corners      []string
	ArticleCount int
	SignOff      string
	Quiz         *quizPromptData
}

// quizPromptData feeds the learning-item section of outro.tmpl (Phase 3
// §5.1). It carries public data only — numbered titles and summary bodies
// of the day's featured articles. Learning state (stage, review history,
// backlog counts) must never appear here (Phase 3 §10/§12-4): the struct
// deliberately has no field that could hold it.
type quizPromptData struct {
	Count    int    // M — 選ばせる記事数 (D-18)
	Marker   string // quizSectionMarker
	Articles []quizPromptArticle
}

// quizPromptArticle is one numbered entry of the 本日の記事一覧. Number is
// the 1-based on-air position; the parser maps it back to the article ID
// (§5.1: article_id との対応をパースで復元).
type quizPromptArticle struct {
	Number  int
	Title   string
	Summary string
}

func renderPrompt(name string, data any) (string, error) {
	var sb strings.Builder
	if err := promptTemplates.ExecuteTemplate(&sb, name, data); err != nil {
		return "", fmt.Errorf("script: render prompt %s: %w", name, err)
	}
	return sb.String(), nil
}
