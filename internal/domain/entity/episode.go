package entity

import "time"

// Episode feed kinds (§4: episodes.feed_kind). The private feed lives
// outside the subscriber concept: 'private' episodes are served only by the
// tailnet-bound handler and never pass token verification (§4 設計メモ).
const (
	FeedKindPublic  = "public"
	FeedKindPrivate = "private"
)

// Episode represents one generated radio episode (episodes table, §4).
// The mp3 lives on the filesystem; the DB stores only the local path and
// byte size (C-10) so the server can serve it with http.ServeContent.
type Episode struct {
	ID          int64
	FeedKind    string // FeedKindPublic | FeedKindPrivate
	Title       string
	ShowNotes   string // 記事リンク集。通知にも流用
	AudioPath   string // Pi ローカルパス
	AudioBytes  int64
	DurationSec int
	PublishedAt time.Time
}
