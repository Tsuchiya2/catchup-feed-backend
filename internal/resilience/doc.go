// Package resilience provides reliability and fault tolerance patterns for the application.
// It includes implementations of circuit breakers, retry logic, and health check utilities
// to ensure system resilience in the face of failures.
//
// The package supports:
//   - Circuit breakers for external API calls (Claude, OpenAI, RSS feeds)
//   - Retry logic with exponential backoff and jitter
//   - Health check utilities for dependency monitoring
//
// Usage Example:
//
//	cb := circuitbreaker.NewCircuitBreaker("my-service", circuitbreaker.DefaultConfig())
//	result, err := cb.Execute(func() (interface{}, error) {
//	    return callExternalService()
//	})
//
//	retryConfig := retry.DefaultConfig()
//	err := retry.WithBackoff(ctx, retryConfig, func() error {
//	    return performOperation()
//	})
package resilience
