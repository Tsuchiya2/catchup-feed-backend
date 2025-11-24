package http

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"catchup-feed/internal/handler/http/requestid"
	"catchup-feed/internal/handler/http/respond"
	"catchup-feed/internal/handler/http/responsewriter"

	"go.opentelemetry.io/otel/trace"
)

// Logging returns middleware that logs HTTP requests with structured logging.
// It captures request details, response status, size, and processing duration.
// The middleware also extracts and logs the trace ID from the OpenTelemetry span context
// to enable correlation between logs and distributed traces.
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

			// Extract trace ID from OpenTelemetry span context
			span := trace.SpanFromContext(r.Context())
			traceID := span.SpanContext().TraceID().String()

			// Calculate processing duration
			duration := time.Since(start)

			// Log request completion with structured fields
			logger.Info("request completed",
				slog.String("request_id", reqID),
				slog.String("trace_id", traceID),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
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
						slog.String("path", r.URL.Path),
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

// requestRecord stores request timestamps for sliding window rate limiting.
type requestRecord struct {
	timestamps []time.Time
	mu         sync.Mutex
}

// RateLimiter implements IP address-based rate limiting middleware using a sliding window algorithm.
type RateLimiter struct {
	records   sync.Map // map[string]*requestRecord
	limit     int      // 許可する最大リクエスト数
	window    time.Duration
	cleanMu   sync.Mutex
	lastClean time.Time
}

// NewRateLimiter creates a new rate limiting middleware.
// limit: maximum number of requests allowed within the time window.
// window: time window duration (e.g., for 5 requests per minute: limit=5, window=1*time.Minute).
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:     limit,
		window:    window,
		lastClean: time.Now(),
	}
}

// Limit applies rate limiting to incoming requests based on client IP address.
// Returns 429 Too Many Requests if the rate limit is exceeded.
func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)

		// 定期的に古いレコードをクリーンアップ（メモリリーク防止）
		rl.periodicCleanup()

		// リクエストが許可されるか確認
		if !rl.allow(ip) {
			respond.SafeError(w, http.StatusTooManyRequests, fmt.Errorf("rate limit exceeded"))
			return
		}

		next.ServeHTTP(w, r)
	})
}

// allow determines if a request is permitted and records the timestamp if allowed.
func (rl *RateLimiter) allow(ip string) bool {
	now := time.Now()

	// レコードを取得または作成
	val, _ := rl.records.LoadOrStore(ip, &requestRecord{
		timestamps: make([]time.Time, 0, rl.limit),
	})
	record := val.(*requestRecord)

	record.mu.Lock()
	defer record.mu.Unlock()

	// 時間窓外の古いタイムスタンプを削除
	cutoff := now.Add(-rl.window)
	validTimestamps := make([]time.Time, 0, len(record.timestamps))
	for _, ts := range record.timestamps {
		if ts.After(cutoff) {
			validTimestamps = append(validTimestamps, ts)
		}
	}
	record.timestamps = validTimestamps

	// リクエスト数が制限を超えているか確認
	if len(record.timestamps) >= rl.limit {
		return false
	}

	// 新しいタイムスタンプを記録
	record.timestamps = append(record.timestamps, now)
	return true
}

// periodicCleanup periodically removes old records to prevent memory leaks.
func (rl *RateLimiter) periodicCleanup() {
	rl.cleanMu.Lock()
	defer rl.cleanMu.Unlock()

	// 10分に1回クリーンアップ
	if time.Since(rl.lastClean) < 10*time.Minute {
		return
	}

	rl.lastClean = time.Now()
	cutoff := time.Now().Add(-rl.window * 2) // 時間窓の2倍以上古いレコードを削除

	rl.records.Range(func(key, value interface{}) bool {
		rl.cleanupRecord(key, value, cutoff)
		return true
	})
}

// cleanupRecord checks if a record is outdated and removes it if necessary.
func (rl *RateLimiter) cleanupRecord(key, value interface{}, cutoff time.Time) {
	record := value.(*requestRecord)
	record.mu.Lock()
	defer record.mu.Unlock()

	// すべてのタイムスタンプが古い場合はレコード自体を削除
	if rl.isRecordOutdated(record, cutoff) {
		rl.records.Delete(key)
	}
}

// isRecordOutdated checks if all timestamps in a record are older than the cutoff time.
func (rl *RateLimiter) isRecordOutdated(record *requestRecord, cutoff time.Time) bool {
	for _, ts := range record.timestamps {
		if ts.After(cutoff) {
			return false
		}
	}
	return true
}

// extractIP extracts the client IP address from the HTTP request.
// It checks X-Forwarded-For and X-Real-IP headers before falling back to RemoteAddr.
func extractIP(r *http.Request) string {
	// X-Forwarded-For ヘッダーを優先（リバースプロキシ経由の場合）
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// 最初のIPアドレスを使用（クライアントのIP）
		if ip := parseFirstIP(xff); ip != "" {
			return ip
		}
	}

	// X-Real-IP ヘッダーを確認
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}

	// RemoteAddr から取得（最後の手段）
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// parseFirstIP parses the first IP address from a comma-separated list.
func parseFirstIP(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			ip := net.ParseIP(s[:i])
			if ip != nil {
				return ip.String()
			}
			return ""
		}
	}
	// カンマがない場合は全体をパース
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return ""
}
