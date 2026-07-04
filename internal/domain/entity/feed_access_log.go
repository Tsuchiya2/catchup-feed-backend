package entity

import "time"

// FeedAccessLog records one authenticated access to the public feed
// (feed_access_logs table, §4). EpisodeID is nil for feed.xml fetches and
// set for episode mp3 downloads. Logs aggregate per friend via the token's
// subscriber (C-8).
type FeedAccessLog struct {
	ID         int64
	TokenID    int64
	EpisodeID  *int64 // nil = feed.xml 取得
	UserAgent  *string
	AccessedAt time.Time
}
