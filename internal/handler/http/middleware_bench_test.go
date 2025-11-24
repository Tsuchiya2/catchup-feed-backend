package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpHandler "catchup-feed/internal/handler/http"
)

// BenchmarkRateLimiter_Sequential は順次リクエストの性能を測定
func BenchmarkRateLimiter_Sequential(b *testing.B) {
	limiter := httpHandler.NewRateLimiter(100, time.Minute)

	handler := limiter.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

// BenchmarkRateLimiter_Parallel は並行リクエストの性能を測定
func BenchmarkRateLimiter_Parallel(b *testing.B) {
	limiter := httpHandler.NewRateLimiter(1000, time.Minute)

	handler := limiter.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// 異なるIPアドレスをシミュレート
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = "192.168.1." + string(rune(i%255)) + ":12345"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			i++
		}
	})
}

// BenchmarkRateLimiter_SameIP は同一IPからの連続リクエストの性能を測定
func BenchmarkRateLimiter_SameIP(b *testing.B) {
	limiter := httpHandler.NewRateLimiter(10000, time.Minute)

	handler := limiter.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.100:12345"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

// BenchmarkRateLimiter_MultipleIPs は複数IPの混在リクエストの性能を測定
func BenchmarkRateLimiter_MultipleIPs(b *testing.B) {
	limiter := httpHandler.NewRateLimiter(1000, time.Minute)

	handler := limiter.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ips := []string{
		"192.168.1.1:12345",
		"192.168.1.2:12345",
		"192.168.1.3:12345",
		"192.168.1.4:12345",
		"192.168.1.5:12345",
		"192.168.1.6:12345",
		"192.168.1.7:12345",
		"192.168.1.8:12345",
		"192.168.1.9:12345",
		"192.168.1.10:12345",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = ips[i%len(ips)]
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}
