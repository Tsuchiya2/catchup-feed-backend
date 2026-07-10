// Package script builds the radio episode script (§3.2 internal/script:
// セグメント選定・順序・つなぎ文生成). The LLM input is the article summary
// only — never the extracted article content (C-12); this is enforced
// structurally by repository.RadioArticle carrying no content field.
//
// Prompts live as files under prompts/ and are embedded at build time, so
// prompt tuning is versioned like code (§6-2).
package script

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
	"time"

	"catchup-feed/internal/repository"
)

//go:embed prompts/*.tmpl
var promptFS embed.FS

var promptTemplates = template.Must(template.ParseFS(promptFS, "prompts/*.tmpl"))

// introData feeds prompts/intro.tmpl.
type introData struct {
	ShowName     string
	Date         string
	Corners      []string
	ArticleCount int
}

// newsData feeds prompts/news.tmpl. Summary is the only article-derived
// body text (C-12); PrevTitle drives the つなぎ文 (§6-2).
type newsData struct {
	ShowName  string
	Category  string
	Source    string
	Title     string
	Summary   string
	PrevTitle string
	Position  int
	Total     int
}

// outroData feeds prompts/outro.tmpl. Quiz enables the Phase 3 learning
// item section piggybacked on the outro call (D-19: 台本生成と同一 LLM
// 呼び出しに相乗り — クオータ純増ゼロ); nil renders the outro prompt
// byte-identically to the pre-Phase 3 template, which is what keeps the
// public episode free of any regression (Phase 3 §12-1).
type outroData struct {
	ShowName string
	Date     string
	Titles   []string
	Quiz     *quizPromptData
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

// formatDate renders the broadcast date in spoken Japanese form.
func formatDate(t time.Time) string {
	return fmt.Sprintf("%d年%d月%d日", t.Year(), int(t.Month()), t.Day())
}

// corners returns the distinct categories in on-air order.
func corners(articles []repository.RadioArticle) []string {
	var out []string
	seen := make(map[string]bool, len(articles))
	for _, a := range articles {
		if !seen[a.Category] {
			seen[a.Category] = true
			out = append(out, a.Category)
		}
	}
	return out
}
