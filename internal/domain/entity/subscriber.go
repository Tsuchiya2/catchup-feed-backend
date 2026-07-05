package entity

import "time"

// Subscriber represents a friend who receives the public radio feed
// (subscribers table, §4). Tokens have their own revoke/reissue lifecycle
// while the friend persists, hence the 1:N split into feed_tokens (C-8).
type Subscriber struct {
	ID            int64
	Name          string
	Note          *string // 期待するフィードバックの種類など
	Email         *string
	CreatedAt     time.Time
	DeactivatedAt *time.Time // nil = アクティブ
}

// IsActive reports whether the subscriber is currently active.
func (s *Subscriber) IsActive() bool {
	return s.DeactivatedAt == nil
}
