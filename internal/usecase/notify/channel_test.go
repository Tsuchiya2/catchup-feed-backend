package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
)

// mockChannel is a test implementation of the Channel interface
type mockChannel struct {
	name        string
	enabled     bool
	sendError   error
	sendDelay   time.Duration
	panicOnSend bool
	sendCalled  int
	mu          sync.Mutex
}

func (m *mockChannel) Name() string {
	return m.name
}

func (m *mockChannel) IsEnabled() bool {
	return m.enabled
}

func (m *mockChannel) Send(ctx context.Context, article *entity.Article, source *entity.Source) error {
	m.mu.Lock()
	m.sendCalled++
	shouldPanic := m.panicOnSend
	m.mu.Unlock()

	if shouldPanic {
		panic("mock panic in Send()")
	}

	if !m.enabled {
		return ErrChannelDisabled
	}
	if article == nil {
		return ErrInvalidArticle
	}
	if source == nil {
		return ErrInvalidSource
	}

	if m.sendDelay > 0 {
		select {
		case <-time.After(m.sendDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	m.mu.Lock()
	err := m.sendError
	m.mu.Unlock()
	return err
}

func (m *mockChannel) getSendCalledCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sendCalled
}

func (m *mockChannel) resetSendCalled() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCalled = 0
}

func (m *mockChannel) setPanicOnSend(panic bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.panicOnSend = panic
}

func (m *mockChannel) setSendError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendError = err
}

// TestChannelInterface verifies that mockChannel implements Channel interface
func TestChannelInterface(t *testing.T) {
	var _ Channel = (*mockChannel)(nil)
}

// TestMockChannel_Name tests the Name method
func TestMockChannel_Name(t *testing.T) {
	ch := &mockChannel{name: "test-channel"}
	if got := ch.Name(); got != "test-channel" {
		t.Errorf("Name() = %v, want %v", got, "test-channel")
	}
}

// TestMockChannel_IsEnabled tests the IsEnabled method
func TestMockChannel_IsEnabled(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
		want    bool
	}{
		{"enabled channel", true, true},
		{"disabled channel", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &mockChannel{enabled: tt.enabled}
			if got := ch.IsEnabled(); got != tt.want {
				t.Errorf("IsEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMockChannel_Send tests the Send method with various scenarios
func TestMockChannel_Send(t *testing.T) {
	ctx := context.Background()
	validArticle := &entity.Article{
		ID:    1,
		Title: "Test Article",
		URL:   "https://example.com",
	}
	validSource := &entity.Source{
		ID:   1,
		Name: "Test Source",
	}

	tests := []struct {
		name      string
		enabled   bool
		article   *entity.Article
		source    *entity.Source
		sendError error
		wantErr   error
	}{
		{
			name:    "successful send",
			enabled: true,
			article: validArticle,
			source:  validSource,
			wantErr: nil,
		},
		{
			name:    "disabled channel",
			enabled: false,
			article: validArticle,
			source:  validSource,
			wantErr: ErrChannelDisabled,
		},
		{
			name:    "nil article",
			enabled: true,
			article: nil,
			source:  validSource,
			wantErr: ErrInvalidArticle,
		},
		{
			name:    "nil source",
			enabled: true,
			article: validArticle,
			source:  nil,
			wantErr: ErrInvalidSource,
		},
		{
			name:      "send error",
			enabled:   true,
			article:   validArticle,
			source:    validSource,
			sendError: errors.New("network error"),
			wantErr:   errors.New("network error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := &mockChannel{
				enabled:   tt.enabled,
				sendError: tt.sendError,
			}

			err := ch.Send(ctx, tt.article, tt.source)

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
		})
	}
}
