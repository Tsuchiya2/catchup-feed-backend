package script

import (
	"strings"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/learning"
)

// QuizCorner is the read-aloud plan of the §7.2 復習コーナー, built from the
// day's due learning items. Everything here is template text — the corner
// never spends an LLM call (§7.2-3: クイズコーナーのために LLM を追加で
// 呼ばない), and the question/answer bodies are the learning_items fields
// verbatim (生成時点でラジオ口調).
//
// The corner is private-episode material only (D-16). It must never reach
// the public feed, episode notifications (D-29: admin email) or a cloud
// LLM prompt (§10).
type QuizCorner struct {
	// Lead is the corner introduction (format.go の定型句+項目数).
	Lead string
	// Items are the reads, in ListDue order (§6.3: due_on ASC, id ASC).
	Items []QuizRead
}

// QuizRead is one item's read-aloud pair. Question and Answer are voiced as
// separate TTS units so the pipeline can place the 3-second silence between
// them in the ffmpeg concat list (§7.2-2: 無音は VOICEVOX に作らせない).
type QuizRead struct {
	ItemID    int64
	ArticleID *int64 // 元記事があれば設定 (§7.2-4); nil for kind='book'
	Question  string // quizReadQuestion(番号, learning_items.question)
	Answer    string // quizReadAnswer(learning_items.answer)
}

// BuildQuizCorner renders the corner for the given due items; an empty
// selection yields an empty corner (the private episode then carries no
// quiz segments at all).
func BuildQuizCorner(items []learning.Item) QuizCorner {
	if len(items) == 0 {
		return QuizCorner{}
	}
	corner := QuizCorner{
		Lead:  quizCornerLead(len(items)),
		Items: make([]QuizRead, 0, len(items)),
	}
	for i, item := range items {
		var articleID *int64
		if item.ArticleID != nil {
			id := *item.ArticleID
			articleID = &id
		}
		corner.Items = append(corner.Items, QuizRead{
			ItemID:    item.ID,
			ArticleID: articleID,
			// The numbering and answer cues are part of the read text and
			// therefore part of the archived segment script — segments must
			// record exactly what went on air (§4 設計メモ: script カラムに
			// 読み上げ全文が残る). 文言は format.go (D-37 (9)).
			Question: quizReadQuestion(i+1, item.Question),
			Answer:   quizReadAnswer(item.Answer),
		})
	}
	return corner
}

// Segments renders the corner as segment rows for the PRIVATE episode:
// one lead row plus 1 項目 = 1 行 (kind='quiz', script = 読み上げ全文,
// §7.2-4), positions starting at startPosition.
func (c QuizCorner) Segments(startPosition int) []*entity.Segment {
	if len(c.Items) == 0 {
		return nil
	}
	segments := make([]*entity.Segment, 0, len(c.Items)+1)
	segments = append(segments, &entity.Segment{
		Position: startPosition,
		Kind:     entity.SegmentKindQuiz,
		Script:   c.Lead,
	})
	for i, item := range c.Items {
		segments = append(segments, &entity.Segment{
			Position:  startPosition + 1 + i,
			Kind:      entity.SegmentKindQuiz,
			ArticleID: item.ArticleID,
			Script:    item.Question + "\n\n" + item.Answer,
		})
	}
	return segments
}

// ItemIDs returns the asked item IDs in corner order (RecordAsked input).
func (c QuizCorner) ItemIDs() []int64 {
	ids := make([]int64, len(c.Items))
	for i, item := range c.Items {
		ids[i] = item.ItemID
	}
	return ids
}

// AppendQuizShowNotes appends the §7.5 learning section to the PRIVATE
// episode's show notes: the asked concepts plus the grading-page link —
// the only bridge from listening (podcast app) to grading (dashboard PWA).
// An empty selection returns the notes unchanged. Never call this for the
// public episode (§10: 学習コンテンツを公開フィードに流さない).
func AppendQuizShowNotes(notes string, items []learning.Item, gradeURL string) string {
	if len(items) == 0 {
		return notes
	}
	var sb strings.Builder
	sb.WriteString(notes)
	sb.WriteString(quizNotesHeading)
	for _, item := range items {
		sb.WriteString("- ")
		sb.WriteString(item.Concept)
		sb.WriteString("\n")
	}
	sb.WriteString(quizNotesGradePrefix)
	sb.WriteString(gradeURL)
	return sb.String()
}

// QuizOnlyIntro is the fixed opening of a quiz-only private episode (§7.1:
// 記事ゼロでも期日到来項目があれば私的版のみ生成). Deliberately a template,
// not an LLM call: the day has no public article material, quiz content must
// not reach a cloud prompt (§10), and a fixed two-liner costs zero quota
// (§12-3) for a segment only the admin ever hears.
//
// 番組名は引数に取らない — 読み上げ名は format.go に固定されている (D-37 (3))。
// date は放送日のタイムゾーンに変換済みの時刻であること。
func QuizOnlyIntro(date time.Time) string { return quizOnlyIntroScript(date) }

// QuizOnlyOutro is the fixed closing of a quiz-only private episode.
func QuizOnlyOutro() string { return quizOnlyOutroScript }

// QuizOnlyShowNotesBase opens the quiz-only episode's show notes; the
// concept list and grading link are appended via AppendQuizShowNotes.
func QuizOnlyShowNotesBase() string { return quizOnlyShowNotesBase }
