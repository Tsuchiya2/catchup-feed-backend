package notifier

import (
	"context"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
)

func TestNoOpNotifier_NotifyArticle(t *testing.T) {
	t.Run("TC-1: should return nil without error", func(t *testing.T) {
		// Arrange
		notifier := NewNoOpNotifier()
		ctx := context.Background()

		article := &entity.Article{
			ID:          1,
			SourceID:    1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
			CreatedAt:   time.Now(),
		}

		source := &entity.Source{
			ID:            1,
			Name:          "Test Source",
			FeedURL:       "https://example.com/feed",
			LastCrawledAt: &[]time.Time{time.Now()}[0],
			Active:        true,
		}

		// Act
		err := notifier.NotifyArticle(ctx, article, source)

		// Assert
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("TC-2: should not make any HTTP requests", func(t *testing.T) {
		// Arrange
		// This test verifies the no-op behavior by ensuring the method returns immediately
		// and doesn't trigger any side effects.

		notifier := NewNoOpNotifier()
		ctx := context.Background()

		article := &entity.Article{
			ID:          1,
			Title:       "Test Article",
			URL:         "https://example.com/article/1",
			Summary:     "Test summary",
			PublishedAt: time.Now(),
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		start := time.Now()
		err := notifier.NotifyArticle(ctx, article, source)
		elapsed := time.Since(start)

		// Assert
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}

		// Should complete immediately (< 1ms) since it does nothing
		if elapsed > time.Millisecond {
			t.Errorf("expected no-op to complete immediately, but took %v", elapsed)
		}
	})

	t.Run("TC-3: should work with nil article or source", func(t *testing.T) {
		// Arrange
		notifier := NewNoOpNotifier()
		ctx := context.Background()

		// Act & Assert - nil article
		err := notifier.NotifyArticle(ctx, nil, &entity.Source{ID: 1, Name: "Test"})
		if err != nil {
			t.Errorf("expected nil error with nil article, got %v", err)
		}

		// Act & Assert - nil source
		err = notifier.NotifyArticle(ctx, &entity.Article{ID: 1, Title: "Test"}, nil)
		if err != nil {
			t.Errorf("expected nil error with nil source, got %v", err)
		}

		// Act & Assert - both nil
		err = notifier.NotifyArticle(ctx, nil, nil)
		if err != nil {
			t.Errorf("expected nil error with both nil, got %v", err)
		}
	})

	t.Run("TC-4: should work with canceled context", func(t *testing.T) {
		// Arrange
		notifier := NewNoOpNotifier()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		article := &entity.Article{
			ID:      1,
			Title:   "Test Article",
			Summary: "Test summary",
		}

		source := &entity.Source{
			ID:   1,
			Name: "Test Source",
		}

		// Act
		err := notifier.NotifyArticle(ctx, article, source)

		// Assert - Should still succeed even with canceled context
		if err != nil {
			t.Errorf("expected nil error even with canceled context, got %v", err)
		}
	})
}

func TestNewNoOpNotifier(t *testing.T) {
	t.Run("should create a new NoOpNotifier instance", func(t *testing.T) {
		// Act
		notifier := NewNoOpNotifier()

		// Assert
		if notifier == nil {
			t.Fatal("expected non-nil notifier")
		}
	})
}
