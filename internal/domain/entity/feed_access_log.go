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

// FeedAccessRecord is the dashboard read model: one access joined with the
// owning subscriber through the token (C-8), so the timeline can be shown
// per friend without the client resolving token→subscriber itself.
type FeedAccessRecord struct {
	FeedAccessLog
	SubscriberID   int64
	SubscriberName string
}

// SubscriberAccessSummary aggregates accesses per friend (C-8) for the
// dashboard: last access plus recent counts, the inputs of neglect
// detection ("最終アクセスが N 日以上前の友人"). Subscribers without any
// access (or even without tokens) still appear, with LastAccessedAt nil.
type SubscriberAccessSummary struct {
	SubscriberID   int64
	SubscriberName string
	Active         bool       // subscribers.deactivated_at IS NULL
	LastAccessedAt *time.Time // nil = 一度もアクセスなし
	Count7d        int64
	Count30d       int64
}
