// Package requestid provides middleware and utilities for managing HTTP request IDs.
// It generates unique IDs for each request to enable request tracing across logs.
package requestid

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// RequestIDKey is the context key for storing request IDs.
	RequestIDKey contextKey = "request_id"
	// RequestIDHeader is the HTTP header name for request IDs.
	RequestIDHeader = "X-Request-ID"
)

// FromContext retrieves the request ID from the context.
// Returns an empty string if no request ID is found.
func FromContext(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// WithRequestID adds a request ID to the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, RequestIDKey, id)
}

// Middleware generates or propagates request IDs for HTTP requests.
// If an X-Request-ID header exists, it uses that value; otherwise, it generates a new UUID v4.
// The request ID is added to both the response header and the request context.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 既存のリクエストID を確認
		requestID := r.Header.Get(RequestIDHeader)
		if requestID == "" {
			// 新規生成（UUID v4）
			requestID = uuid.New().String()
		}

		// レスポンスヘッダーにも追加（クライアントが追跡可能に）
		w.Header().Set(RequestIDHeader, requestID)

		// コンテキストに追加
		ctx := WithRequestID(r.Context(), requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
