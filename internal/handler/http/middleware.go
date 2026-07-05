package http

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"catchup-feed/internal/handler/http/pathutil"
	"catchup-feed/internal/handler/http/requestid"
	"catchup-feed/internal/handler/http/respond"
	"catchup-feed/internal/handler/http/responsewriter"
)

// Logging returns middleware that logs HTTP requests with structured logging.
// It captures request details, response status, size, and processing duration.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap ResponseWriter to record status code and size
			wrapped := responsewriter.Wrap(w)

			// Process request
			next.ServeHTTP(wrapped, r)

			// Extract request ID
			reqID := requestid.FromContext(r.Context())

			// Calculate processing duration
			duration := time.Since(start)

			// Log request completion with structured fields
			logger.Info("request completed",
				slog.String("request_id", reqID),
				slog.String("method", r.Method),
				slog.String("path", pathutil.RedactPath(r.URL.Path)),
				slog.String("query", r.URL.RawQuery),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.Header.Get("User-Agent")),
				slog.Int("status", wrapped.StatusCode()),
				slog.Int("bytes", wrapped.BytesWritten()),
				slog.Duration("duration", duration),
				slog.String("duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000)),
			)
		})
	}
}

// Recover returns middleware that catches panics and logs them with structured logging.
// It prevents the server from crashing and returns a 500 Internal Server Error response.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					// リクエストID を取得
					reqID := requestid.FromContext(r.Context())

					// スタックトレースを取得
					stack := string(debug.Stack())

					// エラーレスポンスを返す
					respond.SafeError(
						w,
						http.StatusInternalServerError,
						fmt.Errorf("internal error"),
					)

					// 構造化ログで記録
					logger.Error("panic recovered",
						slog.String("request_id", reqID),
						slog.String("method", r.Method),
						slog.String("path", pathutil.RedactPath(r.URL.Path)),
						slog.Any("panic", rec),
						slog.String("stack", stack),
					)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// LimitRequestBody returns middleware that limits the size of request bodies to prevent DoS attacks.
func LimitRequestBody(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
