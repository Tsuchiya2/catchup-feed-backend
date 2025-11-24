// Package circuitbreaker provides circuit breaker implementations for external service calls.
// It uses the github.com/sony/gobreaker library to prevent cascading failures.
package circuitbreaker

import (
	"log/slog"
	"time"

	"github.com/sony/gobreaker"
)

// Config holds the configuration for a circuit breaker.
type Config struct {
	// Name is the circuit breaker name for logging and metrics
	Name string

	// MaxRequests is the maximum number of requests allowed in half-open state
	MaxRequests uint32

	// Interval is the cyclic period of the closed state to clear success/failure counts
	Interval time.Duration

	// Timeout is how long to wait in open state before trying again
	Timeout time.Duration

	// FailureThreshold is the failure ratio threshold to trip the circuit
	// For example, 0.6 means 60% failure rate
	FailureThreshold float64

	// MinRequests is the minimum number of requests before calculating failure ratio
	MinRequests uint32
}

// DefaultConfig returns a default configuration for circuit breakers.
func DefaultConfig(name string) Config {
	return Config{
		Name:             name,
		MaxRequests:      3,
		Interval:         30 * time.Second,
		Timeout:          60 * time.Second,
		FailureThreshold: 0.6,
		MinRequests:      5,
	}
}

// ClaudeAPIConfig returns configuration optimized for Claude API calls.
func ClaudeAPIConfig() Config {
	return Config{
		Name:             "claude-api",
		MaxRequests:      3,
		Interval:         30 * time.Second,
		Timeout:          60 * time.Second,
		FailureThreshold: 0.6,
		MinRequests:      5,
	}
}

// OpenAIAPIConfig returns configuration optimized for OpenAI API calls.
func OpenAIAPIConfig() Config {
	return Config{
		Name:             "openai-api",
		MaxRequests:      3,
		Interval:         30 * time.Second,
		Timeout:          60 * time.Second,
		FailureThreshold: 0.6,
		MinRequests:      5,
	}
}

// FeedFetchConfig returns configuration optimized for RSS feed fetching.
func FeedFetchConfig() Config {
	return Config{
		Name:             "feed-fetch",
		MaxRequests:      5,
		Interval:         60 * time.Second,
		Timeout:          120 * time.Second,
		FailureThreshold: 0.7,
		MinRequests:      10,
	}
}

// WebScraperConfig returns configuration optimized for web scraping.
// More conservative than RSS fetching due to SSRF risks and site structure changes.
func WebScraperConfig() Config {
	return Config{
		Name:             "web-scraper",
		MaxRequests:      3,
		Interval:         60 * time.Second,
		Timeout:          3600 * time.Second, // 1 hour
		FailureThreshold: 0.8,                // 80% failure rate (5 consecutive failures)
		MinRequests:      5,
	}
}

// CircuitBreaker wraps gobreaker.CircuitBreaker with additional functionality.
type CircuitBreaker struct {
	breaker *gobreaker.CircuitBreaker
	name    string
}

// New creates a new circuit breaker with the given configuration.
func New(cfg Config) *CircuitBreaker {
	settings := gobreaker.Settings{
		Name:        cfg.Name,
		MaxRequests: cfg.MaxRequests,
		Interval:    cfg.Interval,
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			if counts.Requests < cfg.MinRequests {
				return false
			}
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return failureRatio >= cfg.FailureThreshold
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			slog.Warn("circuit breaker state changed",
				slog.String("circuit", name),
				slog.String("from", from.String()),
				slog.String("to", to.String()))
		},
	}

	return &CircuitBreaker{
		breaker: gobreaker.NewCircuitBreaker(settings),
		name:    cfg.Name,
	}
}

// Execute runs the given function through the circuit breaker.
// If the circuit is open, it returns ErrOpenState immediately.
func (cb *CircuitBreaker) Execute(fn func() (interface{}, error)) (interface{}, error) {
	return cb.breaker.Execute(fn)
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() gobreaker.State {
	return cb.breaker.State()
}

// Name returns the name of the circuit breaker.
func (cb *CircuitBreaker) Name() string {
	return cb.name
}

// IsOpen returns true if the circuit breaker is in the open state.
func (cb *CircuitBreaker) IsOpen() bool {
	return cb.breaker.State() == gobreaker.StateOpen
}
