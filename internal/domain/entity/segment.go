package entity

// Segment kinds (§4: segments.kind). Phase 3 adds 'quiz' | 'review'.
const (
	SegmentKindIntro = "intro"
	SegmentKindNews  = "news"
	SegmentKindOutro = "outro"
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
