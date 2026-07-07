package script

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/learning"
)

func cornerItems() []learning.Item {
	articleID := int64(77)
	bookID := int64(3)
	return []learning.Item{
		{ID: 101, Kind: learning.KindArticle, ArticleID: &articleID,
			Concept: "コンテキスト伝播", Question: "問いその1?", Answer: "答えその1。"},
		{ID: 102, Kind: learning.KindBook, BookID: &bookID,
			Concept: "章の学び", Question: "問いその2?", Answer: "答えその2。"},
	}
}

func TestBuildQuizCorner(t *testing.T) {
	t.Run("empty selection yields an empty corner", func(t *testing.T) {
		corner := BuildQuizCorner(nil)
		assert.Empty(t, corner.Lead)
		assert.Empty(t, corner.Items)
		assert.Nil(t, corner.Segments(5))
		assert.Empty(t, corner.ItemIDs())
	})

	t.Run("lead embeds the item count, no LLM involved (§7.2)", func(t *testing.T) {
		corner := BuildQuizCorner(cornerItems())
		assert.Contains(t, corner.Lead, "復習のコーナー")
		assert.Contains(t, corner.Lead, "2問")
	})

	t.Run("reads carry numbering and answer cue around the verbatim fields", func(t *testing.T) {
		corner := BuildQuizCorner(cornerItems())
		require.Len(t, corner.Items, 2)
		assert.Equal(t, "第1問。問いその1?", corner.Items[0].Question)
		assert.Equal(t, "答え。答えその1。", corner.Items[0].Answer)
		assert.Equal(t, "第2問。問いその2?", corner.Items[1].Question)
		require.NotNil(t, corner.Items[0].ArticleID)
		assert.Equal(t, int64(77), *corner.Items[0].ArticleID)
		assert.Nil(t, corner.Items[1].ArticleID, "book 項目に article_id はない")
	})

	t.Run("item ids in corner order", func(t *testing.T) {
		assert.Equal(t, []int64{101, 102}, BuildQuizCorner(cornerItems()).ItemIDs())
	})
}

func TestQuizCorner_Segments(t *testing.T) {
	corner := BuildQuizCorner(cornerItems())
	segs := corner.Segments(4)

	require.Len(t, segs, 3, "lead + 1項目=1行 (§7.2-4)")
	for i, s := range segs {
		assert.Equal(t, 4+i, s.Position)
		assert.Equal(t, entity.SegmentKindQuiz, s.Kind, "kind 定数は entity の一箇所 (§12-6)")
	}
	assert.Equal(t, corner.Lead, segs[0].Script)
	assert.Nil(t, segs[0].ArticleID)
	assert.Equal(t, "第1問。問いその1?\n\n答え。答えその1。", segs[1].Script,
		"script は読み上げ全文 = question+answer (§7.2-4)")
	require.NotNil(t, segs[1].ArticleID)
	assert.Equal(t, int64(77), *segs[1].ArticleID)
	assert.Nil(t, segs[2].ArticleID)
}

func TestAppendQuizShowNotes(t *testing.T) {
	t.Run("appends concepts and grading link (§7.5)", func(t *testing.T) {
		got := AppendQuizShowNotes("本文", cornerItems(), "https://pulse.catchup-feed.com/learning")
		assert.True(t, strings.HasPrefix(got, "本文"))
		assert.Contains(t, got, "今日の復習:")
		assert.Contains(t, got, "- コンテキスト伝播")
		assert.Contains(t, got, "- 章の学び")
		assert.Contains(t, got, "採点はこちら: https://pulse.catchup-feed.com/learning")
	})

	t.Run("no items leaves the notes untouched", func(t *testing.T) {
		assert.Equal(t, "本文", AppendQuizShowNotes("本文", nil, "https://x"))
	})
}

func TestQuizOnlyTemplates(t *testing.T) {
	date := time.Date(2026, 7, 5, 4, 30, 0, 0, time.UTC)

	intro := QuizOnlyIntro("pulse", date)
	assert.Contains(t, intro, "pulse")
	assert.Contains(t, intro, "2026年7月5日")
	assert.Contains(t, intro, "おさらい")

	outro := QuizOnlyOutro("pulse")
	assert.Contains(t, outro, "pulse")
	assert.Contains(t, outro, "以上です")

	assert.NotEmpty(t, QuizOnlyShowNotesBase())
}
