package notify

import (
	"context"
	"errors"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/infra/notifier"
)

// mockSlackNotifier is a test implementation of the Notifier interface
// used to test SlackChannel behavior without making real HTTP requests.
type mockSlackNotifier struct {
	notifyCalled    int
	returnErr       error
	capturedCtx     context.Context
	capturedArticle *entity.Article
	capturedSource  *entity.Source
}

func (m *mockSlackNotifier) NotifyArticle(ctx context.Context, article *entity.Article, source *entity.Source) error {
	m.notifyCalled++
	m.capturedCtx = ctx
	m.capturedArticle = article
	m.capturedSource = source
	return m.returnErr
}

// newTestSlackChannel creates a SlackChannel with a mock notifier for testing.
func newTestSlackChannel(enabled bool, mockNotifier *mockSlackNotifier) *SlackChannel {
	return &SlackChannel{
		notifier: mockNotifier,
		enabled:  enabled,
	}
}

// TestSlackChannel_Name verifies the Name method returns "slack".
func TestSlackChannel_Name(t *testing.T) {
	// Arrange
	config := notifier.SlackConfig{
		Enabled:    true,
		WebhookURL: "https://hooks.slack.com/services/test/test/test",
		Timeout:    10 * time.Second,
	}

	// Act
	ch := NewSlackChannel(config)

	// Assert
	got := ch.Name()
	want := "slack"
	if got != want {
		t.Errorf("Name() = %v, want %v", got, want)
	}
}

// TestSlackChannel_IsEnabled verifies the IsEnabled method returns the config value.
func TestSlackChannel_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{
			name:    "enabled channel",
			enabled: true,
			want:    true,
		},
		{
			name:    "disabled channel",
			enabled: false,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			config := notifier.SlackConfig{
				Enabled:    tt.enabled,
				WebhookURL: "https://hooks.slack.com/services/test/test/test",
				Timeout:    10 * time.Second,
			}

			// Act
			ch := NewSlackChannel(config)

			// Assert
			if got := ch.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSlackChannel_Send_DelegatesToNotifier verifies that Send delegates to NotifyArticle.
func TestSlackChannel_Send_DelegatesToNotifier(t *testing.T) {
	// Arrange
	ctx := context.Background()
	validArticle := &entity.Article{
		ID:          1,
		Title:       "Test Article",
		URL:         "https://example.com/article",
		Summary:     "Test summary",
		PublishedAt: time.Now(),
	}
	validSource := &entity.Source{
		ID:      1,
		Name:    "Test Source",
		FeedURL: "https://example.com",
	}

	mockNotifier := &mockSlackNotifier{
		returnErr: nil,
	}

	ch := newTestSlackChannel(true, mockNotifier)

	// Act
	err := ch.Send(ctx, validArticle, validSource)

	// Assert
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	if mockNotifier.notifyCalled != 1 {
		t.Errorf("NotifyArticle() called %d times, want 1", mockNotifier.notifyCalled)
	}

	if mockNotifier.capturedArticle != validArticle {
		t.Errorf("NotifyArticle() called with article = %v, want %v", mockNotifier.capturedArticle, validArticle)
	}

	if mockNotifier.capturedSource != validSource {
		t.Errorf("NotifyArticle() called with source = %v, want %v", mockNotifier.capturedSource, validSource)
	}

	if mockNotifier.capturedCtx != ctx {
		t.Errorf("NotifyArticle() called with different context")
	}
}

// TestSlackChannel_Send_PropagatesErrors verifies that Send propagates errors from the notifier.
func TestSlackChannel_Send_PropagatesErrors(t *testing.T) {
	tests := []struct {
		name          string
		enabled       bool
		article       *entity.Article
		source        *entity.Source
		notifierError error
		wantErr       error
		wantCalled    int
	}{
		{
			name:    "disabled channel returns ErrChannelDisabled",
			enabled: false,
			article: &entity.Article{
				ID:          1,
				Title:       "Test Article",
				URL:         "https://example.com",
				PublishedAt: time.Now(),
			},
			source: &entity.Source{
				ID:      1,
				Name:    "Test Source",
				FeedURL: "https://example.com",
			},
			notifierError: nil,
			wantErr:       ErrChannelDisabled,
			wantCalled:    0, // Should not call notifier when disabled
		},
		{
			name:    "nil article returns ErrInvalidArticle",
			enabled: true,
			article: nil,
			source: &entity.Source{
				ID:      1,
				Name:    "Test Source",
				FeedURL: "https://example.com",
			},
			notifierError: nil,
			wantErr:       ErrInvalidArticle,
			wantCalled:    0,
		},
		{
			name:    "nil source returns ErrInvalidSource",
			enabled: true,
			article: &entity.Article{
				ID:          1,
				Title:       "Test Article",
				URL:         "https://example.com",
				PublishedAt: time.Now(),
			},
			source:        nil,
			notifierError: nil,
			wantErr:       ErrInvalidSource,
			wantCalled:    0,
		},
		{
			name:    "notifier network error is propagated",
			enabled: true,
			article: &entity.Article{
				ID:          1,
				Title:       "Test Article",
				URL:         "https://example.com",
				PublishedAt: time.Now(),
			},
			source: &entity.Source{
				ID:      1,
				Name:    "Test Source",
				FeedURL: "https://example.com",
			},
			notifierError: errors.New("network error: connection refused"),
			wantErr:       errors.New("network error: connection refused"),
			wantCalled:    1,
		},
		{
			name:    "notifier rate limit error is propagated",
			enabled: true,
			article: &entity.Article{
				ID:          1,
				Title:       "Test Article",
				URL:         "https://example.com",
				PublishedAt: time.Now(),
			},
			source: &entity.Source{
				ID:      1,
				Name:    "Test Source",
				FeedURL: "https://example.com",
			},
			notifierError: errors.New("Slack rate limit exceeded (retry after 5s)"),
			wantErr:       errors.New("Slack rate limit exceeded (retry after 5s)"),
			wantCalled:    1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			ctx := context.Background()
			mockNotifier := &mockSlackNotifier{
				returnErr: tt.notifierError,
			}

			ch := newTestSlackChannel(tt.enabled, mockNotifier)

			// Act
			err := ch.Send(ctx, tt.article, tt.source)

			// Assert
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("Send() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("Send() error = nil, want %v", tt.wantErr)
				} else if !errors.Is(err, tt.wantErr) && err.Error() != tt.wantErr.Error() {
					t.Errorf("Send() error = %v, want %v", err, tt.wantErr)
				}
			}

			if mockNotifier.notifyCalled != tt.wantCalled {
				t.Errorf("NotifyArticle() called %d times, want %d", mockNotifier.notifyCalled, tt.wantCalled)
			}
		})
	}
}

// TestSlackChannel_Send_RespectsContext verifies that Send respects context cancellation.
func TestSlackChannel_Send_RespectsContext(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	validArticle := &entity.Article{
		ID:          1,
		Title:       "Test Article",
		URL:         "https://example.com",
		PublishedAt: time.Now(),
	}
	validSource := &entity.Source{
		ID:      1,
		Name:    "Test Source",
		FeedURL: "https://example.com",
	}

	mockNotifier := &mockSlackNotifier{
		returnErr: context.Canceled,
	}

	ch := newTestSlackChannel(true, mockNotifier)

	// Cancel context before sending
	cancel()

	// Act
	err := ch.Send(ctx, validArticle, validSource)

	// Assert
	if err == nil {
		t.Error("Send() error = nil, want context.Canceled")
	}

	// Verify that the canceled context was passed to the notifier
	if mockNotifier.capturedCtx != ctx {
		t.Error("Send() did not pass context to notifier")
	}

	if mockNotifier.notifyCalled != 1 {
		t.Errorf("NotifyArticle() called %d times, want 1", mockNotifier.notifyCalled)
	}
}

// TestSlackChannel_Send_WithTimeout verifies timeout behavior.
func TestSlackChannel_Send_WithTimeout(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	validArticle := &entity.Article{
		ID:          1,
		Title:       "Test Article",
		URL:         "https://example.com",
		PublishedAt: time.Now(),
	}
	validSource := &entity.Source{
		ID:      1,
		Name:    "Test Source",
		FeedURL: "https://example.com",
	}

	mockNotifier := &mockSlackNotifier{
		returnErr: context.DeadlineExceeded,
	}

	ch := newTestSlackChannel(true, mockNotifier)

	// Wait for context to timeout
	time.Sleep(5 * time.Millisecond)

	// Act
	err := ch.Send(ctx, validArticle, validSource)

	// Assert
	if err == nil {
		t.Error("Send() error = nil, want context.DeadlineExceeded")
	}

	if mockNotifier.notifyCalled != 1 {
		t.Errorf("NotifyArticle() called %d times, want 1", mockNotifier.notifyCalled)
	}
}

// TestSlackChannel_NewSlackChannel_WithDisabledConfig verifies NoOpNotifier is used when disabled.
func TestSlackChannel_NewSlackChannel_WithDisabledConfig(t *testing.T) {
	// Arrange
	config := notifier.SlackConfig{
		Enabled:    false,
		WebhookURL: "",
		Timeout:    10 * time.Second,
	}

	// Act
	ch := NewSlackChannel(config)

	// Assert
	if ch.IsEnabled() {
		t.Error("IsEnabled() = true, want false")
	}

	// Verify that it uses NoOpNotifier (should not panic with empty webhook URL)
	ctx := context.Background()
	article := &entity.Article{
		ID:          1,
		Title:       "Test Article",
		URL:         "https://example.com",
		PublishedAt: time.Now(),
	}
	source := &entity.Source{
		ID:      1,
		Name:    "Test Source",
		FeedURL: "https://example.com",
	}

	// Send should return ErrChannelDisabled
	err := ch.Send(ctx, article, source)
	if !errors.Is(err, ErrChannelDisabled) {
		t.Errorf("Send() error = %v, want ErrChannelDisabled", err)
	}
}

// TestSlackChannel_NewSlackChannel_WithEnabledConfig verifies SlackNotifier is used when enabled.
func TestSlackChannel_NewSlackChannel_WithEnabledConfig(t *testing.T) {
	// Arrange
	config := notifier.SlackConfig{
		Enabled:    true,
		WebhookURL: "https://hooks.slack.com/services/test/test/test",
		Timeout:    10 * time.Second,
	}

	// Act
	ch := NewSlackChannel(config)

	// Assert
	if !ch.IsEnabled() {
		t.Error("IsEnabled() = false, want true")
	}

	if ch.Name() != "slack" {
		t.Errorf("Name() = %v, want slack", ch.Name())
	}

	// Verify notifier is not nil
	if ch.notifier == nil {
		t.Error("notifier is nil, want SlackNotifier instance")
	}
}
