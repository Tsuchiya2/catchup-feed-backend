package http

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// Timeout returns middleware that enforces request timeouts.
// If a request takes longer than the specified duration, it returns 504 Gateway Timeout.
// The context is properly canceled to allow downstream handlers to cleanup.
//
// Note: This implementation uses a mutex to prevent race conditions when writing
// the timeout response. Only one goroutine (either the handler or timeout) will
// write to the response.
func Timeout(duration time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create context with timeout
			ctx, cancel := context.WithTimeout(r.Context(), duration)
			defer cancel()

			// Replace request context with timeout context
			r = r.WithContext(ctx)

			// Channel to signal completion and mutex to prevent concurrent writes
			done := make(chan struct{})
			var mu sync.Mutex
			timedOut := false

			// Wrap response writer to check for timeout before writing
			wrappedWriter := &timeoutResponseWriter{
				ResponseWriter: w,
				mu:             &mu,
				timedOut:       &timedOut,
			}

			// Execute handler in goroutine
			go func() {
				next.ServeHTTP(wrappedWriter, r)
				close(done)
			}()

			// Wait for either completion or timeout
			select {
			case <-done:
				// Request completed successfully
				return
			case <-ctx.Done():
				// Timeout occurred - acquire lock and write timeout response
				mu.Lock()
				timedOut = true
				if !wrappedWriter.written {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusGatewayTimeout)
					_, _ = w.Write([]byte(`{"error":"request timeout"}`))
				}
				mu.Unlock()
			}
		})
	}
}

// timeoutResponseWriter wraps http.ResponseWriter to prevent writes after timeout
type timeoutResponseWriter struct {
	http.ResponseWriter
	mu       *sync.Mutex
	timedOut *bool
	written  bool
}

// WriteHeader writes the status code if timeout hasn't occurred
func (w *timeoutResponseWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !*w.timedOut && !w.written {
		w.written = true
		w.ResponseWriter.WriteHeader(statusCode)
	}
}

// Write writes data if timeout hasn't occurred
func (w *timeoutResponseWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if *w.timedOut {
		return 0, http.ErrHandlerTimeout
	}

	if !w.written {
		w.written = true
		w.ResponseWriter.WriteHeader(http.StatusOK)
	}

	return w.ResponseWriter.Write(data)
}
