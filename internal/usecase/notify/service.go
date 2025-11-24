package notify

import (
	"context"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"catchup-feed/internal/domain/entity"

	"github.com/google/uuid"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const requestIDKey contextKey = "request_id"

// Circuit breaker constants
const (
	circuitBreakerThreshold = 5                // Number of consecutive failures before opening
	circuitBreakerTimeout   = 5 * time.Minute  // Duration to keep circuit breaker open
	workerPoolTimeout       = 5 * time.Second  // Timeout for acquiring worker slot
	notificationTimeout     = 30 * time.Second // Timeout for individual notification
)

// Service handles notification dispatching to multiple channels.
// It orchestrates sending notifications asynchronously without blocking
// the caller.
type Service interface {
	// NotifyNewArticle dispatches a notification about a newly saved article
	// to all enabled notification channels.
	//
	// This method is non-blocking and returns immediately. Notifications
	// are sent in background goroutines, and failures are logged but do
	// not propagate errors to the caller.
	//
	// Parameters:
	//   - ctx: Context for cancellation (used for logging, not propagated to goroutines)
	//   - article: The article to notify about (must not be nil)
	//   - source: The feed source of the article (must not be nil)
	//
	// Returns:
	//   - nil (always succeeds, errors are handled internally)
	NotifyNewArticle(ctx context.Context, article *entity.Article, source *entity.Source) error

	// GetChannelHealth returns the health status of all notification channels.
	//
	// This method provides visibility into circuit breaker states for monitoring
	// and health check endpoints. The returned data is safe for concurrent access.
	//
	// Returns:
	//   - []ChannelHealthStatus: Health status for each channel
	GetChannelHealth() []ChannelHealthStatus

	// Shutdown gracefully stops the notification service, waiting for
	// in-flight notifications to complete or timeout.
	//
	// This method blocks until all goroutines complete or the context timeout.
	//
	// Parameters:
	//   - ctx: Context with timeout for shutdown (recommended: 30s)
	//
	// Returns:
	//   - error: Non-nil if shutdown timeout exceeded
	Shutdown(ctx context.Context) error
}

// ChannelHealthStatus represents the health status of a notification channel.
type ChannelHealthStatus struct {
	Name               string     // Channel name (e.g., "Discord", "Slack")
	Enabled            bool       // Whether the channel is enabled
	CircuitBreakerOpen bool       // Whether the circuit breaker is currently open
	DisabledUntil      *time.Time // Time until circuit breaker remains open (nil if closed)
}

// service is the concrete implementation of Service interface.
type service struct {
	channels       []Channel                 // Notification channels (Discord, Slack, etc.)
	workerPool     chan struct{}             // Semaphore for limiting concurrent notifications
	channelHealth  map[string]*channelHealth // Circuit breaker state per channel
	healthMu       sync.RWMutex              // Protects channelHealth map
	wg             sync.WaitGroup            // Track in-flight notifications
	shutdownCtx    context.Context           // Context for signaling shutdown
	shutdownCancel context.CancelFunc        // Cancel function for shutdown
}

// channelHealth tracks circuit breaker state for a channel
type channelHealth struct {
	consecutiveFailures int        // Number of consecutive failures
	disabledUntil       time.Time  // Time until circuit breaker is open
	mu                  sync.Mutex // Protects this struct's fields
}

// NewService creates a new notification service with the given channels.
//
// Parameters:
//   - channels: List of notification channels (Discord, Slack, etc.)
//   - maxConcurrent: Maximum concurrent notifications (recommended: 10-20)
//
// Returns:
//   - Service: Configured notification service
func NewService(channels []Channel, maxConcurrent int) Service {
	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())

	svc := &service{
		channels:       channels,
		workerPool:     make(chan struct{}, maxConcurrent),
		channelHealth:  make(map[string]*channelHealth),
		shutdownCtx:    shutdownCtx,
		shutdownCancel: shutdownCancel,
	}

	// Initialize circuit breaker state for each channel
	for _, ch := range channels {
		svc.channelHealth[ch.Name()] = &channelHealth{}
	}

	return svc
}

// NotifyNewArticle implements Service.NotifyNewArticle.
func (s *service) NotifyNewArticle(ctx context.Context, article *entity.Article, source *entity.Source) error {
	// Validate inputs before spawning goroutines
	if article == nil || source == nil {
		slog.Warn("Invalid notification input",
			slog.Bool("nil_article", article == nil),
			slog.Bool("nil_source", source == nil))
		return nil // Don't spawn goroutines for invalid inputs
	}

	// Generate unique request ID for tracing
	// Try to inherit from parent context first
	requestID, ok := ctx.Value("request_id").(string)
	if !ok || requestID == "" {
		requestID = uuid.New().String()
	}

	// Count enabled channels
	enabledCount := 0
	for _, ch := range s.channels {
		if ch.IsEnabled() {
			enabledCount++
		}
	}

	// Update metrics for enabled channels
	SetChannelsEnabled(float64(enabledCount))

	if enabledCount == 0 {
		slog.Debug("No notification channels enabled",
			slog.String("request_id", requestID),
			slog.Int64("article_id", article.ID))
		return nil
	}

	slog.Info("Dispatching article notification",
		slog.String("request_id", requestID),
		slog.Int64("article_id", article.ID),
		slog.String("url", article.URL),
		slog.Int("enabled_channels", enabledCount))

	// Fire goroutine for each enabled channel
	for _, ch := range s.channels {
		if ch.IsEnabled() {
			channel := ch // Capture for goroutine
			s.wg.Add(1)
			go s.notifyChannel(requestID, channel, article, source)
		}
	}

	return nil
}

// notifyChannel sends notification to a single channel in a goroutine.
func (s *service) notifyChannel(requestID string, channel Channel, article *entity.Article, source *entity.Source) {
	defer s.wg.Done()

	// Track active goroutines
	IncrementActiveGoroutines()
	defer DecrementActiveGoroutines()

	// Panic recovery
	defer func() {
		if r := recover(); r != nil {
			slog.Error("Panic in notification channel",
				slog.String("request_id", requestID),
				slog.String("channel", channel.Name()),
				slog.Any("panic", r),
				slog.String("stack", string(debug.Stack())))
		}
	}()

	// Acquire worker slot (with timeout to prevent blocking)
	select {
	case s.workerPool <- struct{}{}:
		defer func() { <-s.workerPool }() // Release slot
	case <-time.After(workerPoolTimeout):
		slog.Warn("Notification dropped: worker pool full",
			slog.String("request_id", requestID),
			slog.String("channel", channel.Name()))
		RecordDropped(channel.Name(), "pool_full")
		return
	}

	// Check circuit breaker
	health := s.getChannelHealth(channel.Name())
	health.mu.Lock()
	if time.Now().Before(health.disabledUntil) {
		slog.Warn("Channel temporarily disabled due to circuit breaker",
			slog.String("request_id", requestID),
			slog.String("channel", channel.Name()),
			slog.Time("disabled_until", health.disabledUntil))
		health.mu.Unlock()
		RecordDropped(channel.Name(), "circuit_open")
		return
	}
	health.mu.Unlock()

	// Create context with timeout (use shutdown context instead of Background)
	ctx, cancel := context.WithTimeout(s.shutdownCtx, notificationTimeout)
	defer cancel()

	// Add request_id to context for tracing
	ctx = context.WithValue(ctx, requestIDKey, requestID)

	// Record start time for metrics
	startTime := time.Now()
	RecordDispatch(channel.Name())

	// Send notification
	err := channel.Send(ctx, article, source)
	duration := time.Since(startTime)

	// Update circuit breaker state
	health.mu.Lock()
	if err != nil {
		health.consecutiveFailures++
		if health.consecutiveFailures >= circuitBreakerThreshold {
			health.disabledUntil = time.Now().Add(circuitBreakerTimeout)
			slog.Error("Circuit breaker opened for channel",
				slog.String("request_id", requestID),
				slog.String("channel", channel.Name()),
				slog.Int("consecutive_failures", health.consecutiveFailures))
			RecordCircuitBreakerOpen(channel.Name())
		}
	} else {
		health.consecutiveFailures = 0 // Reset on success
	}
	health.mu.Unlock()

	// Record metrics and log result
	if err != nil {
		RecordFailure(channel.Name(), duration)
		slog.Warn("Channel notification failed",
			slog.String("request_id", requestID),
			slog.String("channel", channel.Name()),
			slog.Int64("article_id", article.ID),
			slog.String("url", article.URL),
			slog.Duration("send_duration", duration),
			slog.Any("error", err))
	} else {
		RecordSuccess(channel.Name(), duration)
		slog.Info("Channel notification sent successfully",
			slog.String("request_id", requestID),
			slog.String("channel", channel.Name()),
			slog.Int64("article_id", article.ID),
			slog.String("title", article.Title),
			slog.Duration("send_duration", duration))
	}
}

// getChannelHealth returns circuit breaker state for a channel
func (s *service) getChannelHealth(channelName string) *channelHealth {
	s.healthMu.RLock()
	defer s.healthMu.RUnlock()
	return s.channelHealth[channelName]
}

// GetChannelHealth implements Service.GetChannelHealth.
func (s *service) GetChannelHealth() []ChannelHealthStatus {
	s.healthMu.RLock()
	defer s.healthMu.RUnlock()

	statuses := make([]ChannelHealthStatus, 0, len(s.channels))

	for _, ch := range s.channels {
		health := s.channelHealth[ch.Name()]

		// Lock individual channel health for consistent read
		health.mu.Lock()

		var disabledUntil *time.Time
		circuitBreakerOpen := false

		// Check if circuit breaker is currently open
		if time.Now().Before(health.disabledUntil) {
			circuitBreakerOpen = true
			disabledUntil = &health.disabledUntil
		}

		health.mu.Unlock()

		statuses = append(statuses, ChannelHealthStatus{
			Name:               ch.Name(),
			Enabled:            ch.IsEnabled(),
			CircuitBreakerOpen: circuitBreakerOpen,
			DisabledUntil:      disabledUntil,
		})
	}

	return statuses
}

// Shutdown implements Service.Shutdown.
func (s *service) Shutdown(ctx context.Context) error {
	slog.Info("Shutting down notification service")

	// Signal all goroutines to stop
	s.shutdownCancel()

	// Wait for in-flight notifications with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("Notification service shutdown complete")
		return nil
	case <-ctx.Done():
		slog.Warn("Notification service shutdown timeout")
		return ctx.Err()
	}
}
