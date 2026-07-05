package script_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"catchup-feed/internal/repository"
	"catchup-feed/internal/script"
)

func TestBuildShowNotes(t *testing.T) {
	featured := []repository.RadioArticle{
		{Title: "記事A", URL: "https://example.com/a"},
		{Title: "記事B", URL: "https://example.com/b"},
	}
	overflow := []repository.RadioArticle{
		{Title: "記事C", URL: "https://example.com/c"},
	}

	t.Run("featured and overflow sections", func(t *testing.T) {
		notes := script.BuildShowNotes(featured, overflow)

		assert.Equal(t,
			"今日紹介した記事:\n"+
				"- 記事A\n  https://example.com/a\n"+
				"- 記事B\n  https://example.com/b\n"+
				"\n紹介しきれなかった記事:\n"+
				"- 記事C\n  https://example.com/c",
			notes)
	})

	t.Run("no overflow omits the second section", func(t *testing.T) {
		notes := script.BuildShowNotes(featured, nil)

		assert.NotContains(t, notes, "紹介しきれなかった記事")
		assert.Contains(t, notes, "https://example.com/b")
	})
}

func TestAppendVoicevoxCredit(t *testing.T) {
	// U-13 のピン留め: 配布するエピソードのショーノートは必ず
	// 「VOICEVOX:話者名」のクレジット行で終わる。
	notes := script.AppendVoicevoxCredit("今日紹介した記事:\n- 記事A", "ずんだもん")

	assert.Equal(t,
		"今日紹介した記事:\n- 記事A\n\n音声合成: VOICEVOX:ずんだもん",
		notes)
}
