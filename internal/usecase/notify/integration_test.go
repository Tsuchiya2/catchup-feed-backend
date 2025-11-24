package notify

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
)

// ========================================
// Integration Mock Notifier
// ========================================

// integrationMockChannel simulates a realistic notification channel for integration testing
type integrationMockChannel struct {
	name          string
	enabled       bool
	notifications []notificationRecord
	delay         time.Duration
	failAfter     int          // fail after N successful calls
	callCount     atomic.Int32 // thread-safe call counter
	mu            sync.Mutex   // protects notifications slice
}

// notificationRecord records details of each notification attempt
type notificationRecord struct {
	article   *entity.Article
	source    *entity.Source
	timestamp time.Time
	success   bool
}

func newIntegrationMockChannel(name string, enabled bool, delay time.Duration) *integrationMockChannel {
	return &integrationMockChannel{
		name:          name,
		enabled:       enabled,
		delay:         delay,
		notifications: make([]notificationRecord, 0),
		failAfter:     -1, // never fail by default
	}
}

func (m *integrationMockChannel) Name() string {
	return m.name
}

func (m *integrationMockChannel) IsEnabled() bool {
	return m.enabled
}

func (m *integrationMockChannel) Send(ctx context.Context, article *entity.Article, source *entity.Source) error {
	// Validate inputs
	if article == nil {
		return ErrInvalidArticle
	}
	if source == nil {
		return ErrInvalidSource
	}

	// Simulate realistic delay
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Increment call count
	count := m.callCount.Add(1)

	// Determine if this call should fail
	// failAfter = -1: never fail (default)
	// failAfter = 0: always fail
	// failAfter = N: fail after N successful calls
	shouldFail := (m.failAfter == 0) || (m.failAfter > 0 && int(count) > m.failAfter)

	// Record notification
	m.mu.Lock()
	m.notifications = append(m.notifications, notificationRecord{
		article:   article,
		source:    source,
		timestamp: time.Now(),
		success:   !shouldFail,
	})
	m.mu.Unlock()

	if shouldFail {
		return errors.New("simulated notification failure")
	}

	return nil
}

func (m *integrationMockChannel) getNotificationCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.notifications)
}

func (m *integrationMockChannel) getSuccessCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, n := range m.notifications {
		if n.success {
			count++
		}
	}
	return count
}

func (m *integrationMockChannel) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications = make([]notificationRecord, 0)
	m.callCount.Store(0)
}

// ========================================
// Test 1: Single Notification Flow
// ========================================

func TestIntegration_SingleNotification(t *testing.T) {
	// Track initial goroutine count
	initialGoroutines := runtime.NumGoroutine()

	// Create mock notifier
	mockNotifier := newIntegrationMockChannel("test-channel", true, 10*time.Millisecond)
	channels := []Channel{mockNotifier}

	// Create service
	service := NewService(channels, 10)

	// Create test data
	article := &entity.Article{
		ID:          1,
		SourceID:    1,
		Title:       "Integration Test Article",
		URL:         "https://example.com/article",
		Summary:     "Test summary",
		PublishedAt: time.Now(),
	}
	source := &entity.Source{
		ID:      1,
		Name:    "Test Source",
		FeedURL: "https://example.com/feed",
		Active:  true,
	}

	// Send notification
	ctx := context.Background()
	err := service.NotifyNewArticle(ctx, article, source)
	if err != nil {
		t.Fatalf("NotifyNewArticle() failed: %v", err)
	}

	// Wait for notification to complete (delay + buffer)
	time.Sleep(100 * time.Millisecond)

	// Verify notification was sent
	if count := mockNotifier.getNotificationCount(); count != 1 {
		t.Errorf("Expected 1 notification, got %d", count)
	}

	// Shutdown gracefully
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}

	// Verify no goroutine leak
	time.Sleep(100 * time.Millisecond) // Allow goroutines to cleanup
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > initialGoroutines+2 { // Allow small variance
		t.Errorf("Goroutine leak detected: initial=%d, final=%d", initialGoroutines, finalGoroutines)
	}
}

// ========================================
// Test 2: Multiple Channels
// ========================================

func TestIntegration_MultipleChannels(t *testing.T) {
	// Create multiple mock notifiers
	discordMock := newIntegrationMockChannel("discord", true, 10*time.Millisecond)
	slackMock := newIntegrationMockChannel("slack", true, 15*time.Millisecond)
	disabledMock := newIntegrationMockChannel("disabled", false, 0)

	channels := []Channel{discordMock, slackMock, disabledMock}
	service := NewService(channels, 10)

	// Create test data
	article := &entity.Article{
		ID:    2,
		Title: "Multi-Channel Test",
		URL:   "https://example.com/multi",
	}
	source := &entity.Source{
		ID:   2,
		Name: "Multi Source",
	}

	// Send notification
	ctx := context.Background()
	err := service.NotifyNewArticle(ctx, article, source)
	if err != nil {
		t.Fatalf("NotifyNewArticle() failed: %v", err)
	}

	// Wait for notifications to complete
	time.Sleep(100 * time.Millisecond)

	// Verify notifications sent to enabled channels only
	if count := discordMock.getNotificationCount(); count != 1 {
		t.Errorf("Discord: expected 1 notification, got %d", count)
	}
	if count := slackMock.getNotificationCount(); count != 1 {
		t.Errorf("Slack: expected 1 notification, got %d", count)
	}
	if count := disabledMock.getNotificationCount(); count != 0 {
		t.Errorf("Disabled channel: expected 0 notifications, got %d", count)
	}

	// Shutdown gracefully
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}

	// Verify channel health
	health := service.GetChannelHealth()
	if len(health) != 3 {
		t.Errorf("Expected 3 channels in health status, got %d", len(health))
	}

	for _, h := range health {
		if h.Name == "disabled" && h.Enabled {
			t.Errorf("Disabled channel reported as enabled")
		}
		if (h.Name == "discord" || h.Name == "slack") && !h.Enabled {
			t.Errorf("Enabled channel %s reported as disabled", h.Name)
		}
	}
}

// ========================================
// Test 3: Circuit Breaker Integration
// ========================================

func TestIntegration_CircuitBreakerIntegration(t *testing.T) {
	// Create mock that fails after 2 successful sends
	mockNotifier := newIntegrationMockChannel("circuit-test", true, 5*time.Millisecond)
	mockNotifier.failAfter = 2 // Fail on 3rd, 4th, 5th calls

	channels := []Channel{mockNotifier}
	service := NewService(channels, 10)

	article := &entity.Article{
		ID:    3,
		Title: "Circuit Breaker Test",
		URL:   "https://example.com/circuit",
	}
	source := &entity.Source{
		ID:   3,
		Name: "Circuit Source",
	}

	ctx := context.Background()

	// Send notifications until circuit breaker opens
	// circuitBreakerThreshold = 5 consecutive failures
	for i := 0; i < 8; i++ {
		err := service.NotifyNewArticle(ctx, article, source)
		if err != nil {
			t.Fatalf("NotifyNewArticle() failed: %v", err)
		}
		time.Sleep(50 * time.Millisecond) // Allow goroutine to process
	}

	// Wait for all notifications to process
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}

	// Verify circuit breaker state
	health := service.GetChannelHealth()
	if len(health) != 1 {
		t.Fatalf("Expected 1 channel, got %d", len(health))
	}

	// Circuit breaker should be open after 5 consecutive failures
	// We sent 8 notifications: 2 success, then 5+ failures
	// So circuit breaker should be open
	if !health[0].CircuitBreakerOpen {
		t.Errorf("Circuit breaker should be open but was closed")
	}

	if health[0].DisabledUntil == nil {
		t.Errorf("DisabledUntil should be set when circuit breaker is open")
	}

	// Verify not all notifications were sent (some dropped by circuit breaker)
	totalSent := mockNotifier.getNotificationCount()
	if totalSent >= 8 {
		t.Errorf("Circuit breaker should have dropped some notifications, but got %d/8", totalSent)
	}
}

// ========================================
// Test 4: Worker Pool Saturation
// ========================================

func TestIntegration_WorkerPoolSaturation(t *testing.T) {
	// Create slow mock notifier (100ms delay)
	slowMock := newIntegrationMockChannel("slow-channel", true, 100*time.Millisecond)

	// Small worker pool (only 2 workers)
	channels := []Channel{slowMock}
	service := NewService(channels, 2)

	article := &entity.Article{
		ID:    4,
		Title: "Pool Saturation Test",
		URL:   "https://example.com/pool",
	}
	source := &entity.Source{
		ID:   4,
		Name: "Pool Source",
	}

	ctx := context.Background()

	// Send 10 notifications quickly (more than pool size)
	for i := 0; i < 10; i++ {
		err := service.NotifyNewArticle(ctx, article, source)
		if err != nil {
			t.Fatalf("NotifyNewArticle() failed: %v", err)
		}
	}

	// Wait for notifications to complete or timeout
	time.Sleep(150 * time.Millisecond)
	// Shutdown with generous timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}

	// Some notifications should be dropped due to pool saturation
	// With pool size 2 and workerPoolTimeout of 5s, most should succeed
	// but we verify that the pool limiting works
	sent := slowMock.getNotificationCount()
	if sent == 0 {
		t.Errorf("Expected some notifications to be sent, got 0")
	}
	// We don't assert exact count because timing varies
	t.Logf("Pool saturation test: sent %d/10 notifications", sent)
}

// ========================================
// Test 5: Graceful Shutdown
// ========================================

func TestIntegration_GracefulShutdown(t *testing.T) {
	// Create mock with short delay (to complete before shutdown)
	mockNotifier := newIntegrationMockChannel("shutdown-test", true, 10*time.Millisecond)

	channels := []Channel{mockNotifier}
	service := NewService(channels, 10)

	article := &entity.Article{
		ID:    5,
		Title: "Shutdown Test",
		URL:   "https://example.com/shutdown",
	}
	source := &entity.Source{
		ID:   5,
		Name: "Shutdown Source",
	}

	ctx := context.Background()

	// Send 5 notifications
	for i := 0; i < 5; i++ {
		err := service.NotifyNewArticle(ctx, article, source)
		if err != nil {
			t.Fatalf("NotifyNewArticle() failed: %v", err)
		}
	}

	// Wait for notifications to complete before shutdown
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdownStart := time.Now()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}
	shutdownDuration := time.Since(shutdownStart)

	// Verify all notifications completed
	sent := mockNotifier.getNotificationCount()
	if sent != 5 {
		t.Errorf("Expected all 5 notifications to complete, got %d", sent)
	}

	t.Logf("Graceful shutdown took %v for %d notifications", shutdownDuration, sent)
}

// ========================================
// Test 6: Metrics Recorded
// ========================================

func TestIntegration_MetricsRecorded(t *testing.T) {
	// Note: This is a basic test that verifies metrics functions don't panic.
	// Full metrics validation would require prometheus test utilities.

	successMock := newIntegrationMockChannel("metrics-success", true, 10*time.Millisecond)
	failMock := newIntegrationMockChannel("metrics-fail", true, 10*time.Millisecond)
	failMock.failAfter = 0 // Fail immediately

	channels := []Channel{successMock, failMock}
	service := NewService(channels, 10)

	article := &entity.Article{
		ID:    6,
		Title: "Metrics Test",
		URL:   "https://example.com/metrics",
	}
	source := &entity.Source{
		ID:   6,
		Name: "Metrics Source",
	}

	ctx := context.Background()

	// Send notification (should record metrics for both success and failure)
	err := service.NotifyNewArticle(ctx, article, source)
	if err != nil {
		t.Fatalf("NotifyNewArticle() failed: %v", err)
	}

	// Wait for notifications to complete
	time.Sleep(50 * time.Millisecond)

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}

	// Verify notifications were attempted
	if count := successMock.getNotificationCount(); count != 1 {
		t.Errorf("Success channel: expected 1 notification, got %d", count)
	}
	if count := failMock.getNotificationCount(); count != 1 {
		t.Errorf("Fail channel: expected 1 notification, got %d", count)
	}

	// Verify success/failure counts
	if success := successMock.getSuccessCount(); success != 1 {
		t.Errorf("Expected 1 successful notification on success channel, got %d", success)
	}
	if success := failMock.getSuccessCount(); success != 0 {
		t.Errorf("Expected 0 successful notifications on fail channel, got %d", success)
	}

	t.Log("Metrics recording verified (no panics)")
}

// ========================================
// Test 7: Context Cancellation
// ========================================

func TestIntegration_ContextCancellation(t *testing.T) {
	// Create slow mock that would take 5 seconds
	slowMock := newIntegrationMockChannel("context-test", true, 5*time.Second)

	channels := []Channel{slowMock}
	service := NewService(channels, 10)

	article := &entity.Article{
		ID:    7,
		Title: "Context Test",
		URL:   "https://example.com/context",
	}
	source := &entity.Source{
		ID:   7,
		Name: "Context Source",
	}

	// Create context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Send notification (context should cancel during send)
	err := service.NotifyNewArticle(ctx, article, source)
	if err != nil {
		t.Fatalf("NotifyNewArticle() should not return error: %v", err)
	}

	// Wait for goroutine to process
	time.Sleep(200 * time.Millisecond)

	// Shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}

	// Notification should have been attempted but context cancelled
	// The notification record may or may not exist depending on timing
	count := slowMock.getNotificationCount()
	t.Logf("Context cancellation test: %d notifications recorded (timing-dependent)", count)

	// The important part is that no panic occurred and shutdown completed
}

// ========================================
// Test 8: Concurrent Notifications (Stress Test)
// ========================================

func TestIntegration_ConcurrentNotifications(t *testing.T) {
	// Track goroutines
	initialGoroutines := runtime.NumGoroutine()

	// Create mock notifiers
	fastMock := newIntegrationMockChannel("fast-channel", true, 5*time.Millisecond)
	mediumMock := newIntegrationMockChannel("medium-channel", true, 20*time.Millisecond)

	channels := []Channel{fastMock, mediumMock}
	service := NewService(channels, 20) // Larger pool for stress test

	// Prepare test data
	articles := make([]*entity.Article, 100)
	sources := make([]*entity.Source, 100)
	for i := 0; i < 100; i++ {
		articles[i] = &entity.Article{
			ID:    int64(1000 + i),
			Title: "Concurrent Test Article",
			URL:   "https://example.com/concurrent",
		}
		sources[i] = &entity.Source{
			ID:   int64(1000 + i),
			Name: "Concurrent Source",
		}
	}

	ctx := context.Background()

	// Send 100 notifications concurrently
	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			err := service.NotifyNewArticle(ctx, articles[idx], sources[idx])
			if err != nil {
				t.Errorf("NotifyNewArticle() failed: %v", err)
			}
		}(i)
	}

	// Wait for all dispatches to complete
	wg.Wait()
	dispatchDuration := time.Since(startTime)

	// Wait for background goroutines to process
	time.Sleep(150 * time.Millisecond)

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}
	totalDuration := time.Since(startTime)

	// Verify notifications were sent
	fastCount := fastMock.getNotificationCount()
	mediumCount := mediumMock.getNotificationCount()

	t.Logf("Stress test results:")
	t.Logf("  - Fast channel: %d/100 notifications", fastCount)
	t.Logf("  - Medium channel: %d/100 notifications", mediumCount)
	t.Logf("  - Dispatch duration: %v", dispatchDuration)
	t.Logf("  - Total duration: %v", totalDuration)

	// Most notifications should succeed (allow some drops due to pool saturation)
	if fastCount < 80 {
		t.Errorf("Fast channel: expected at least 80 notifications, got %d", fastCount)
	}
	if mediumCount < 80 {
		t.Errorf("Medium channel: expected at least 80 notifications, got %d", mediumCount)
	}

	// Verify no goroutine leak
	time.Sleep(200 * time.Millisecond) // Allow cleanup
	finalGoroutines := runtime.NumGoroutine()
	if finalGoroutines > initialGoroutines+5 { // Allow small variance
		t.Errorf("Goroutine leak detected: initial=%d, final=%d, leaked=%d",
			initialGoroutines, finalGoroutines, finalGoroutines-initialGoroutines)
	}

	// Verify dispatch was fast (non-blocking)
	if dispatchDuration > 1*time.Second {
		t.Errorf("Dispatch took too long (%v), should be non-blocking", dispatchDuration)
	}
}

// ========================================
// Test 9: Invalid Input Handling
// ========================================

func TestIntegration_InvalidInputHandling(t *testing.T) {
	mockNotifier := newIntegrationMockChannel("invalid-input", true, 10*time.Millisecond)
	channels := []Channel{mockNotifier}
	service := NewService(channels, 10)

	ctx := context.Background()

	tests := []struct {
		name    string
		article *entity.Article
		source  *entity.Source
	}{
		{
			name:    "nil article",
			article: nil,
			source: &entity.Source{
				ID:   1,
				Name: "Test",
			},
		},
		{
			name: "nil source",
			article: &entity.Article{
				ID:    1,
				Title: "Test",
				URL:   "https://example.com",
			},
			source: nil,
		},
		{
			name:    "both nil",
			article: nil,
			source:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock
			mockNotifier.reset()

			// Send notification with invalid input
			err := service.NotifyNewArticle(ctx, tt.article, tt.source)
			if err != nil {
				t.Fatalf("NotifyNewArticle() should not return error for invalid input: %v", err)
			}

			// Wait briefly
			time.Sleep(50 * time.Millisecond)

			// Verify no notification was sent
			if count := mockNotifier.getNotificationCount(); count != 0 {
				t.Errorf("Expected 0 notifications for invalid input, got %d", count)
			}
		})
	}

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}
}

// ========================================
// Test 10: No Enabled Channels
// ========================================

func TestIntegration_NoEnabledChannels(t *testing.T) {
	// All channels disabled
	disabledMock1 := newIntegrationMockChannel("disabled1", false, 0)
	disabledMock2 := newIntegrationMockChannel("disabled2", false, 0)

	channels := []Channel{disabledMock1, disabledMock2}
	service := NewService(channels, 10)

	article := &entity.Article{
		ID:    10,
		Title: "No Channels Test",
		URL:   "https://example.com/nochannels",
	}
	source := &entity.Source{
		ID:   10,
		Name: "No Channels Source",
	}

	ctx := context.Background()

	// Send notification (should return immediately, no goroutines spawned)
	err := service.NotifyNewArticle(ctx, article, source)
	if err != nil {
		t.Fatalf("NotifyNewArticle() failed: %v", err)
	}

	// Wait briefly to ensure no goroutines were spawned
	time.Sleep(50 * time.Millisecond)

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}

	// Verify no notifications sent
	if count := disabledMock1.getNotificationCount(); count != 0 {
		t.Errorf("Disabled channel 1: expected 0 notifications, got %d", count)
	}
	if count := disabledMock2.getNotificationCount(); count != 0 {
		t.Errorf("Disabled channel 2: expected 0 notifications, got %d", count)
	}

	// Verify health status shows all disabled
	health := service.GetChannelHealth()
	for _, h := range health {
		if h.Enabled {
			t.Errorf("Channel %s should be disabled but is enabled", h.Name)
		}
	}
}
