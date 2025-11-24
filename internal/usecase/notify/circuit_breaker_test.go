package notify

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
)

// testChannel is a test channel for circuit breaker testing
// It extends the existing mockChannel from service_test.go with failure mode control
type testChannel struct {
	*mockChannel
	failureMode   bool // when true, Send() returns error
	failureModeMu sync.RWMutex
}

func newTestChannel(name string, enabled bool) *testChannel {
	return &testChannel{
		mockChannel: &mockChannel{
			name:    name,
			enabled: enabled,
		},
		failureMode: false,
	}
}

func (tc *testChannel) Send(ctx context.Context, article *entity.Article, source *entity.Source) error {
	tc.failureModeMu.RLock()
	shouldFail := tc.failureMode
	tc.failureModeMu.RUnlock()

	if shouldFail {
		tc.mu.Lock()
		tc.sendCalled++
		tc.mu.Unlock()
		return errors.New("simulated channel failure")
	}
	return tc.mockChannel.Send(ctx, article, source)
}

func (tc *testChannel) setFailureMode(mode bool) {
	tc.failureModeMu.Lock()
	defer tc.failureModeMu.Unlock()
	tc.failureMode = mode
}

func (tc *testChannel) getSendCalledCount() int {
	return tc.mockChannel.getSendCalledCount()
}

// TestCircuitBreaker_OpensAfterThresholdFailures verifies that 5 consecutive failures trigger circuit breaker
func TestCircuitBreaker_OpensAfterThresholdFailures(t *testing.T) {
	// Arrange: Create a test channel that always fails
	channel := newTestChannel("test-channel", true)
	channel.setFailureMode(true) // Always fail

	svc := NewService([]Channel{channel}, 10)

	validArticle := &entity.Article{
		ID:    1,
		Title: "Test Article",
		URL:   "https://example.com",
	}
	validSource := &entity.Source{
		ID:   1,
		Name: "Test Source",
	}

	// Act: Send 5 notifications (threshold)
	for i := 0; i < circuitBreakerThreshold; i++ {
		err := svc.NotifyNewArticle(context.Background(), validArticle, validSource)
		if err != nil {
			t.Fatalf("NotifyNewArticle() failed on iteration %d: %v", i, err)
		}
	}

	// Wait for goroutines to complete
	time.Sleep(100 * time.Millisecond)

	// Assert: Circuit breaker should be open
	healthStatuses := svc.GetChannelHealth()
	if len(healthStatuses) != 1 {
		t.Fatalf("Expected 1 channel health status, got %d", len(healthStatuses))
	}

	health := healthStatuses[0]
	if !health.CircuitBreakerOpen {
		t.Errorf("Circuit breaker should be open after %d failures", circuitBreakerThreshold)
	}

	if health.DisabledUntil == nil {
		t.Error("DisabledUntil should not be nil when circuit breaker is open")
	}

	// Verify that Send() was called 5 times (before circuit breaker opened)
	if channel.getSendCalledCount() != circuitBreakerThreshold {
		t.Errorf("Send() called %d times, expected %d", channel.getSendCalledCount(), circuitBreakerThreshold)
	}

	// Act: Try to send one more notification
	err := svc.NotifyNewArticle(context.Background(), validArticle, validSource)
	if err != nil {
		t.Fatalf("NotifyNewArticle() failed: %v", err)
	}

	// Wait for goroutine to check circuit breaker
	time.Sleep(100 * time.Millisecond)

	// Assert: Send() should NOT be called again (circuit breaker prevents it)
	if channel.getSendCalledCount() != circuitBreakerThreshold {
		t.Errorf("Send() should not be called when circuit breaker is open, but was called %d times", channel.getSendCalledCount())
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = svc.Shutdown(ctx)
}

// TestCircuitBreaker_ResetsOnSuccess verifies that success resets failure counter
func TestCircuitBreaker_ResetsOnSuccess(t *testing.T) {
	// Arrange: Create a test channel
	channel := newTestChannel("test-channel", true)

	svc := NewService([]Channel{channel}, 10)

	validArticle := &entity.Article{
		ID:    1,
		Title: "Test Article",
		URL:   "https://example.com",
	}
	validSource := &entity.Source{
		ID:   1,
		Name: "Test Source",
	}

	// Act: Send 3 failures, then 1 success, then 3 more failures
	channel.setFailureMode(true)
	for i := 0; i < 3; i++ {
		_ = svc.NotifyNewArticle(context.Background(), validArticle, validSource)
	}
	time.Sleep(100 * time.Millisecond)

	// Success
	channel.setFailureMode(false)
	_ = svc.NotifyNewArticle(context.Background(), validArticle, validSource)
	time.Sleep(100 * time.Millisecond)

	// 3 more failures (total would be 6 if counter wasn't reset)
	channel.setFailureMode(true)
	for i := 0; i < 3; i++ {
		_ = svc.NotifyNewArticle(context.Background(), validArticle, validSource)
	}
	time.Sleep(100 * time.Millisecond)

	// Assert: Circuit breaker should still be closed (3 consecutive failures < 5)
	healthStatuses := svc.GetChannelHealth()
	if len(healthStatuses) != 1 {
		t.Fatalf("Expected 1 channel health status, got %d", len(healthStatuses))
	}

	health := healthStatuses[0]
	if health.CircuitBreakerOpen {
		t.Error("Circuit breaker should NOT be open (success reset counter)")
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = svc.Shutdown(ctx)
}

// TestCircuitBreaker_PreventsSendWhenOpen verifies send is skipped when circuit open
func TestCircuitBreaker_PreventsSendWhenOpen(t *testing.T) {
	// Arrange: Create a test channel that always fails
	channel := newTestChannel("test-channel", true)
	channel.setFailureMode(true)

	svc := NewService([]Channel{channel}, 10)

	validArticle := &entity.Article{
		ID:    1,
		Title: "Test Article",
		URL:   "https://example.com",
	}
	validSource := &entity.Source{
		ID:   1,
		Name: "Test Source",
	}

	// Act: Send 5 notifications to open circuit breaker
	for i := 0; i < circuitBreakerThreshold; i++ {
		_ = svc.NotifyNewArticle(context.Background(), validArticle, validSource)
	}
	time.Sleep(100 * time.Millisecond)

	// Reset send counter
	sendCountBeforeCircuitOpen := channel.getSendCalledCount()

	// Now the channel will succeed (but circuit breaker prevents call)
	channel.setFailureMode(false)

	// Try to send 3 more notifications
	for i := 0; i < 3; i++ {
		_ = svc.NotifyNewArticle(context.Background(), validArticle, validSource)
	}
	time.Sleep(100 * time.Millisecond)

	// Assert: Send() should NOT be called (circuit breaker prevents it)
	if channel.getSendCalledCount() != sendCountBeforeCircuitOpen {
		t.Errorf("Send() should not be called when circuit breaker is open, called %d times total", channel.getSendCalledCount())
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = svc.Shutdown(ctx)
}

// TestCircuitBreaker_AutoRecoveryAfterTimeout verifies circuit closes after timeout
func TestCircuitBreaker_AutoRecoveryAfterTimeout(t *testing.T) {
	// Use a shorter timeout for testing (1 second instead of 5 minutes)
	// We'll directly manipulate the service's internal state for testing
	channel := newTestChannel("test-channel", true)
	channel.setFailureMode(true)

	svc := NewService([]Channel{channel}, 10).(*service)

	validArticle := &entity.Article{
		ID:    1,
		Title: "Test Article",
		URL:   "https://example.com",
	}
	validSource := &entity.Source{
		ID:   1,
		Name: "Test Source",
	}

	// Act: Send 5 notifications to open circuit breaker
	for i := 0; i < circuitBreakerThreshold; i++ {
		_ = svc.NotifyNewArticle(context.Background(), validArticle, validSource)
	}
	time.Sleep(100 * time.Millisecond)

	// Assert: Circuit breaker is open
	healthStatuses := svc.GetChannelHealth()
	if !healthStatuses[0].CircuitBreakerOpen {
		t.Fatal("Circuit breaker should be open")
	}

	// Manually set disabledUntil to 1 second in the future (for testing)
	health := svc.getChannelHealth("test-channel")
	health.mu.Lock()
	health.disabledUntil = time.Now().Add(1 * time.Second)
	health.mu.Unlock()

	// Circuit should still be open
	healthStatuses = svc.GetChannelHealth()
	if !healthStatuses[0].CircuitBreakerOpen {
		t.Error("Circuit breaker should still be open")
	}

	// Wait for timeout to expire
	time.Sleep(1100 * time.Millisecond)

	// Assert: Circuit breaker should auto-close
	healthStatuses = svc.GetChannelHealth()
	if healthStatuses[0].CircuitBreakerOpen {
		t.Error("Circuit breaker should be closed after timeout")
	}

	// Channel should now succeed
	channel.setFailureMode(false)

	// Reset send counter
	sendCountBeforeRecovery := channel.getSendCalledCount()

	// Try to send a notification
	_ = svc.NotifyNewArticle(context.Background(), validArticle, validSource)
	time.Sleep(100 * time.Millisecond)

	// Assert: Send() should be called now (circuit breaker recovered)
	if channel.getSendCalledCount() == sendCountBeforeRecovery {
		t.Error("Send() should be called after circuit breaker recovers")
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = svc.Shutdown(ctx)
}

// TestCircuitBreaker_IndependentPerChannel verifies Discord failure doesn't affect Slack
func TestCircuitBreaker_IndependentPerChannel(t *testing.T) {
	// Arrange: Create two channels - Discord (fails) and Slack (succeeds)
	discordChannel := newTestChannel("discord", true)
	discordChannel.setFailureMode(true) // Always fail

	slackChannel := newTestChannel("slack", true)
	slackChannel.setFailureMode(false) // Always succeed

	svc := NewService([]Channel{discordChannel, slackChannel}, 10)

	validArticle := &entity.Article{
		ID:    1,
		Title: "Test Article",
		URL:   "https://example.com",
	}
	validSource := &entity.Source{
		ID:   1,
		Name: "Test Source",
	}

	// Act: Send 5 notifications (Discord fails, Slack succeeds)
	for i := 0; i < circuitBreakerThreshold; i++ {
		_ = svc.NotifyNewArticle(context.Background(), validArticle, validSource)
	}

	// Wait for goroutines to complete
	time.Sleep(100 * time.Millisecond)

	// Assert: Discord circuit breaker should be open, Slack should be closed
	healthStatuses := svc.GetChannelHealth()
	if len(healthStatuses) != 2 {
		t.Fatalf("Expected 2 channel health statuses, got %d", len(healthStatuses))
	}

	var discordHealth, slackHealth ChannelHealthStatus
	for _, h := range healthStatuses {
		switch h.Name {
		case "discord":
			discordHealth = h
		case "slack":
			slackHealth = h
		}
	}

	// Discord circuit breaker should be open
	if !discordHealth.CircuitBreakerOpen {
		t.Error("Discord circuit breaker should be open after 5 failures")
	}

	// Slack circuit breaker should be closed
	if slackHealth.CircuitBreakerOpen {
		t.Error("Slack circuit breaker should NOT be open (independent from Discord)")
	}

	// Verify send counts
	if discordChannel.getSendCalledCount() != circuitBreakerThreshold {
		t.Errorf("Discord Send() called %d times, expected %d", discordChannel.getSendCalledCount(), circuitBreakerThreshold)
	}

	if slackChannel.getSendCalledCount() != circuitBreakerThreshold {
		t.Errorf("Slack Send() called %d times, expected %d", slackChannel.getSendCalledCount(), circuitBreakerThreshold)
	}

	// Act: Send one more notification
	_ = svc.NotifyNewArticle(context.Background(), validArticle, validSource)
	time.Sleep(100 * time.Millisecond)

	// Assert: Discord should NOT call Send() (circuit open), Slack should call Send()
	if discordChannel.getSendCalledCount() != circuitBreakerThreshold {
		t.Errorf("Discord Send() should not be called when circuit is open")
	}

	if slackChannel.getSendCalledCount() != circuitBreakerThreshold+1 {
		t.Errorf("Slack Send() should be called (circuit is closed), got %d calls", slackChannel.getSendCalledCount())
	}

	// Cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = svc.Shutdown(ctx)
}
