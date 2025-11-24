package http

import (
	"net/http"
)

// InputValidation returns middleware that validates and limits request inputs.
// It enforces limits on:
// - Authorization header size (8KB)
// - URI path length (2KB)
// - Request body size (10MB)
//
// This prevents DoS attacks and ensures reasonable request sizes.
func InputValidation() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Authorization header length limit (8KB)
			// JWT tokens typically < 1KB, but allow headroom for custom headers
			authHeader := r.Header.Get("Authorization")
			if len(authHeader) > 8192 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"authorization header too large"}`))
				return
			}

			// Path length limit (2KB)
			// Prevents path traversal attacks and keeps URLs reasonable
			if len(r.URL.Path) > 2048 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusRequestURITooLong)
				_, _ = w.Write([]byte(`{"error":"URI too long"}`))
				return
			}

			// Request body size limit (10MB)
			// Prevents memory exhaustion from large payloads
			r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

			next.ServeHTTP(w, r)
		})
	}
}
