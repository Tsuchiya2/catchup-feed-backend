package repository

import (
	"context"
	"time"

	"catchup-feed/internal/domain/entity"
)

// SubscriberRepository persists friends receiving the public feed
// (subscribers table, §4 / C-8). Subscribers are deactivated, not deleted:
// their tokens and access logs must survive for aggregation.
type SubscriberRepository interface {
	// Create inserts the subscriber and sets subscriber.ID.
	Create(ctx context.Context, subscriber *entity.Subscriber) error
	// Get returns the subscriber, or nil when not found.
	Get(ctx context.Context, id int64) (*entity.Subscriber, error)
	// List returns all subscribers (active and deactivated), oldest first.
	List(ctx context.Context) ([]*entity.Subscriber, error)
	// Update rewrites name / note / email.
	Update(ctx context.Context, subscriber *entity.Subscriber) error
	// Deactivate marks the subscriber inactive as of t. Their tokens stop
	// verifying (§5.2 checks subscriber activity), no rows are deleted.
	Deactivate(ctx context.Context, id int64, t time.Time) error
}
