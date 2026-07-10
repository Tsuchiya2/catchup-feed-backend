package entity

// Segment kinds (§4: segments.kind). The column is bare text — no CHECK
// constraint — so the Phase 3 kinds insert without a migration (Phase 3 §4
// 設計メモ). This const block is the single home for segment kinds (Phase 3
// §12-6: kind の列挙を分散させない).
const (
	SegmentKindIntro = "intro"
	SegmentKindNews  = "news"
	SegmentKindOutro = "outro"
	// Phase 3 (§7): 私的エピソード限定のコーナー。公開フィードには載せない
	// (D-16)。
	SegmentKindQuiz       = "quiz"        // 復習クイズ(§7.2)
	SegmentKindReview     = "review"      // 週次振り返り(§7.4)
	SegmentKindBookReview = "book_review" // 書籍紹介(§7.3、C-22)
)

// Segment represents one part of an episode script (segments table, §4).
// Script is a first-class asset: keeping the read-aloud text lets the
// Phase 3 comprehension tracker reference when and how a topic was
// explained (§4 設計メモ).
type Segment struct {
	ID        int64
	EpisodeID int64
	Position  int
	Kind      string // SegmentKindIntro | SegmentKindNews | SegmentKindOutro
	ArticleID *int64 // set when Kind == SegmentKindNews
	Script    string
}
