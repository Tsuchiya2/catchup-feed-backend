package script

import (
	"strings"

	"catchup-feed/internal/repository"
)

// BuildShowNotes renders the episode description (episodes.show_notes, §4).
// Every selected article appears with title and URL; overflow articles that
// did not make it on air are listed as links only (§6-1). The text doubles
// as the notification payload (§7).
func BuildShowNotes(featured, overflow []repository.RadioArticle) string {
	var sb strings.Builder
	sb.WriteString("今日紹介した記事:\n")
	writeArticleLinks(&sb, featured)
	if len(overflow) > 0 {
		sb.WriteString("\n紹介しきれなかった記事:\n")
		writeArticleLinks(&sb, overflow)
	}
	return strings.TrimRight(sb.String(), "\n")
}

func writeArticleLinks(sb *strings.Builder, articles []repository.RadioArticle) {
	for _, a := range articles {
		sb.WriteString("- ")
		sb.WriteString(a.Title)
		sb.WriteString("\n  ")
		sb.WriteString(a.URL)
		sb.WriteString("\n")
	}
}
