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

// AppendVoicevoxCredit appends the VOICEVOX credit line to the show notes
// (U-13: 生成音声の配布には「VOICEVOX:話者名」の表記が利用規約上必須).
// Every distributed episode carries this line; the channel description only
// names the engine generically, so the per-speaker credit lives here.
func AppendVoicevoxCredit(showNotes, speakerName string) string {
	return showNotes + "\n\n音声合成: VOICEVOX:" + speakerName
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
