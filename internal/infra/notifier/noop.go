package notifier

import (
	"context"

	"catchup-feed/internal/domain/entity"
)

// NoOpNotifier is a no-operation implementation of the Notifier interface.
// It is used when notifications are disabled to avoid null checks in the code.
// This follows the Null Object pattern.
type NoOpNotifier struct{}

// NewNoOpNotifier creates a new NoOpNotifier instance.
func NewNoOpNotifier() *NoOpNotifier {
	return &NoOpNotifier{}
}

// NotifyArticle does nothing and returns nil immediately.
// This allows the notification feature to be disabled without changing the code flow.
func (n *NoOpNotifier) NotifyArticle(ctx context.Context, article *entity.Article, source *entity.Source) error {
	// No-op: intentionally does nothing
	return nil
}
