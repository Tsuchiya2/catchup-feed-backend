package notify

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus metrics for notification system monitoring
var (
	// notificationDispatchedTotal tracks total notifications dispatched per channel
	notificationDispatchedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_dispatched_total",
			Help: "Total number of notifications dispatched",
		},
		[]string{"channel"},
	)

	// notificationSentTotal tracks notification send results per channel
	notificationSentTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_sent_total",
			Help: "Total number of notifications sent",
		},
		[]string{"channel", "status"}, // status: success|failure
	)

	// notificationDuration tracks notification send duration
	notificationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "notification_duration_seconds",
			Help:    "Notification send duration in seconds",
			Buckets: []float64{0.1, 0.5, 1, 5, 10, 30}, // 100ms to 30s
		},
		[]string{"channel"},
	)

	// notificationRateLimitHits tracks rate limit hits per channel
	notificationRateLimitHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_rate_limit_hits_total",
			Help: "Total number of rate limit hits",
		},
		[]string{"channel"},
	)

	// notificationRateLimitWaitSeconds tracks time spent waiting for rate limits
	notificationRateLimitWaitSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "notification_rate_limit_wait_seconds",
			Help:    "Time spent waiting for rate limits in seconds",
			Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60},
		},
		[]string{"channel"},
	)

	// circuitBreakerOpenTotal tracks circuit breaker open events
	circuitBreakerOpenTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_circuit_breaker_open_total",
			Help: "Total number of circuit breaker open events",
		},
		[]string{"channel"},
	)

	// notificationDroppedTotal tracks dropped notifications (worker pool full)
	notificationDroppedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_dropped_total",
			Help: "Total number of dropped notifications",
		},
		[]string{"channel", "reason"}, // reason: pool_full|circuit_open|disabled
	)

	// activeNotifications tracks currently active notification goroutines
	activeNotifications = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "notification_active_goroutines",
			Help: "Number of active notification goroutines",
		},
	)

	// channelsEnabled tracks number of enabled channels
	channelsEnabled = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "notification_channels_enabled",
			Help: "Number of enabled notification channels",
		},
	)
)

// RecordDispatch records a notification dispatch attempt.
//
// This should be called when a notification is about to be sent to a channel.
//
// Parameters:
//   - channel: The name of the notification channel (e.g., "Discord", "Slack")
func RecordDispatch(channel string) {
	notificationDispatchedTotal.WithLabelValues(channel).Inc()
}

// RecordSuccess records a successful notification send.
//
// This increments the success counter and records the send duration.
//
// Parameters:
//   - channel: The name of the notification channel
//   - duration: The time it took to send the notification
func RecordSuccess(channel string, duration time.Duration) {
	notificationSentTotal.WithLabelValues(channel, "success").Inc()
	notificationDuration.WithLabelValues(channel).Observe(duration.Seconds())
}

// RecordFailure records a failed notification send.
//
// This increments the failure counter and records the send duration.
//
// Parameters:
//   - channel: The name of the notification channel
//   - duration: The time it took before the notification failed
func RecordFailure(channel string, duration time.Duration) {
	notificationSentTotal.WithLabelValues(channel, "failure").Inc()
	notificationDuration.WithLabelValues(channel).Observe(duration.Seconds())
}

// RecordDropped records a dropped notification.
//
// This is called when a notification is dropped due to various reasons
// such as worker pool full, circuit breaker open, or channel disabled.
//
// Parameters:
//   - channel: The name of the notification channel
//   - reason: The reason for dropping (pool_full, circuit_open, disabled)
func RecordDropped(channel string, reason string) {
	notificationDroppedTotal.WithLabelValues(channel, reason).Inc()
}

// RecordCircuitBreakerOpen records a circuit breaker open event.
//
// This is called when a circuit breaker opens due to consecutive failures.
//
// Parameters:
//   - channel: The name of the notification channel
func RecordCircuitBreakerOpen(channel string) {
	circuitBreakerOpenTotal.WithLabelValues(channel).Inc()
}

// RecordRateLimitHit records a rate limit hit.
//
// This is called when a notification encounters a rate limit.
//
// Parameters:
//   - channel: The name of the notification channel
func RecordRateLimitHit(channel string) {
	notificationRateLimitHits.WithLabelValues(channel).Inc()
}

// RecordRateLimitWait records the time spent waiting for rate limits.
//
// This is called when a notification has to wait due to rate limiting.
//
// Parameters:
//   - channel: The name of the notification channel
//   - waitDuration: The time spent waiting
func RecordRateLimitWait(channel string, waitDuration time.Duration) {
	notificationRateLimitWaitSeconds.WithLabelValues(channel).Observe(waitDuration.Seconds())
}

// SetActiveGoroutines sets the current number of active notification goroutines.
//
// This should be incremented when a notification goroutine starts and
// decremented when it finishes.
//
// Parameters:
//   - count: The current number of active goroutines
func SetActiveGoroutines(count float64) {
	activeNotifications.Set(count)
}

// IncrementActiveGoroutines increments the active goroutines gauge by 1.
func IncrementActiveGoroutines() {
	activeNotifications.Inc()
}

// DecrementActiveGoroutines decrements the active goroutines gauge by 1.
func DecrementActiveGoroutines() {
	activeNotifications.Dec()
}

// SetChannelsEnabled sets the number of enabled notification channels.
//
// This should be called when the notification service is initialized or
// when channel configuration changes.
//
// Parameters:
//   - count: The number of enabled channels
func SetChannelsEnabled(count float64) {
	channelsEnabled.Set(count)
}
