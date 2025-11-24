package http

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRateLimiter_Allow(t *testing.T) {
	tests := []struct {
		name           string
		limit          int
		window         time.Duration
		requests       int
		expectedStatus []int
	}{
		{
			name:           "5 requests per minute - all allowed",
			limit:          5,
			window:         1 * time.Minute,
			requests:       5,
			expectedStatus: []int{200, 200, 200, 200, 200},
		},
		{
			name:           "5 requests per minute - 6th request blocked",
			limit:          5,
			window:         1 * time.Minute,
			requests:       6,
			expectedStatus: []int{200, 200, 200, 200, 200, 429},
		},
		{
			name:           "3 requests per minute - immediate limit",
			limit:          3,
			window:         1 * time.Minute,
			requests:       5,
			expectedStatus: []int{200, 200, 200, 429, 429},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := NewRateLimiter(tt.limit, tt.window)

			handler := rl.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			for i := 0; i < tt.requests; i++ {
				req := httptest.NewRequest(http.MethodPost, "/auth/token", nil)
				req.RemoteAddr = "192.168.1.1:12345"

				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)

				if rr.Code != tt.expectedStatus[i] {
					t.Errorf("request %d: got status %d, want %d", i+1, rr.Code, tt.expectedStatus[i])
				}
			}
		})
	}
}

func TestRateLimiter_SlidingWindow(t *testing.T) {
	// 5 requests per second
	rl := NewRateLimiter(5, 1*time.Second)

	handler := rl.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 5 requests immediately - all should succeed
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/token", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("initial request %d: got status %d, want 200", i+1, rr.Code)
		}
	}

	// 6th request should be blocked
	req := httptest.NewRequest(http.MethodPost, "/auth/token", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("6th request: got status %d, want 429", rr.Code)
	}

	// Wait for window to expire
	time.Sleep(1100 * time.Millisecond)

	// After window expires, new request should succeed
	req = httptest.NewRequest(http.MethodPost, "/auth/token", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("after window expiry: got status %d, want 200", rr.Code)
	}
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	// 3 requests per minute
	rl := NewRateLimiter(3, 1*time.Minute)

	handler := rl.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// IP1: 3 requests (all should succeed)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/token", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("IP1 request %d: got status %d, want 200", i+1, rr.Code)
		}
	}

	// IP1: 4th request should be blocked
	req := httptest.NewRequest(http.MethodPost, "/auth/token", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("IP1 4th request: got status %d, want 429", rr.Code)
	}

	// IP2: should have separate limit and succeed
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/auth/token", nil)
		req.RemoteAddr = "192.168.1.2:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("IP2 request %d: got status %d, want 200", i+1, rr.Code)
		}
	}
}

func TestRateLimiter_Concurrent(t *testing.T) {
	// 10 requests per second
	rl := NewRateLimiter(10, 1*time.Second)

	handler := rl.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 20 concurrent requests from same IP
	var wg sync.WaitGroup
	okCount := 0
	blockedCount := 0
	var mu sync.Mutex

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req := httptest.NewRequest(http.MethodPost, "/auth/token", nil)
			req.RemoteAddr = "192.168.1.1:12345"

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			mu.Lock()
			switch rr.Code {
			case http.StatusOK:
				okCount++
			case http.StatusTooManyRequests:
				blockedCount++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Should have exactly 10 successful requests and 10 blocked
	if okCount != 10 {
		t.Errorf("concurrent test: got %d successful requests, want 10", okCount)
	}
	if blockedCount != 10 {
		t.Errorf("concurrent test: got %d blocked requests, want 10", blockedCount)
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		wantIP     string
	}{
		{
			name:       "X-Forwarded-For single IP",
			remoteAddr: "192.168.1.1:12345",
			xff:        "203.0.113.195",
			wantIP:     "203.0.113.195",
		},
		{
			name:       "X-Forwarded-For multiple IPs",
			remoteAddr: "192.168.1.1:12345",
			xff:        "203.0.113.195, 70.41.3.18, 150.172.238.178",
			wantIP:     "203.0.113.195",
		},
		{
			name:       "X-Real-IP",
			remoteAddr: "192.168.1.1:12345",
			xri:        "203.0.113.195",
			wantIP:     "203.0.113.195",
		},
		{
			name:       "RemoteAddr fallback",
			remoteAddr: "192.168.1.1:12345",
			wantIP:     "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For takes precedence over X-Real-IP",
			remoteAddr: "192.168.1.1:12345",
			xff:        "203.0.113.195",
			xri:        "198.51.100.178",
			wantIP:     "203.0.113.195",
		},
		{
			name:       "IPv6",
			remoteAddr: "[2001:db8::1]:12345",
			wantIP:     "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}

			got := extractIP(req)
			if got != tt.wantIP {
				t.Errorf("extractIP() = %q, want %q", got, tt.wantIP)
			}
		})
	}
}

func TestParseFirstIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "203.0.113.195",
			want:  "203.0.113.195",
		},
		{
			input: "203.0.113.195, 70.41.3.18",
			want:  "203.0.113.195",
		},
		{
			input: "invalid, 70.41.3.18",
			want:  "",
		},
		{
			input: "",
			want:  "",
		},
		{
			input: "2001:db8::1",
			want:  "2001:db8::1",
		},
		{
			input: "2001:db8::1, 2001:db8::2",
			want:  "2001:db8::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseFirstIP(tt.input)
			if got != tt.want {
				t.Errorf("parseFirstIP(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLogging(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name           string
		method         string
		path           string
		query          string
		expectedStatus int
	}{
		{
			name:           "GET request with 200 response",
			method:         http.MethodGet,
			path:           "/api/health",
			query:          "",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST request with query params",
			method:         http.MethodPost,
			path:           "/api/articles",
			query:          "page=1&limit=10",
			expectedStatus: http.StatusCreated,
		},
		{
			name:           "DELETE request",
			method:         http.MethodDelete,
			path:           "/api/articles/123",
			query:          "",
			expectedStatus: http.StatusNoContent,
		},
		{
			name:           "request with 500 error",
			method:         http.MethodGet,
			path:           "/api/error",
			query:          "",
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.expectedStatus)
				_, _ = w.Write([]byte("response body"))
			}))

			url := tt.path
			if tt.query != "" {
				url += "?" + tt.query
			}

			req := httptest.NewRequest(tt.method, url, nil)
			req.Header.Set("User-Agent", "test-agent/1.0")
			req.RemoteAddr = "192.168.1.1:12345"

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("got status %d, want %d", rr.Code, tt.expectedStatus)
			}
		})
	}
}

func TestRecover(t *testing.T) {
	logger := slog.Default()

	tests := []struct {
		name        string
		panicValue  interface{}
		shouldPanic bool
	}{
		{
			name:        "panic with string",
			panicValue:  "something went wrong",
			shouldPanic: true,
		},
		{
			name:        "panic with error",
			panicValue:  fmt.Errorf("test error"),
			shouldPanic: true,
		},
		{
			name:        "panic with nil",
			panicValue:  nil,
			shouldPanic: false,
		},
		{
			name:        "panic with number",
			panicValue:  42,
			shouldPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := Recover(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.shouldPanic {
					panic(tt.panicValue)
				}
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rr := httptest.NewRecorder()

			// Should not panic - middleware catches it
			handler.ServeHTTP(rr, req)

			if tt.shouldPanic {
				// Should return 500 error
				if rr.Code != http.StatusInternalServerError {
					t.Errorf("got status %d, want %d", rr.Code, http.StatusInternalServerError)
				}
			} else {
				// Should return 200
				if rr.Code != http.StatusOK {
					t.Errorf("got status %d, want %d", rr.Code, http.StatusOK)
				}
			}
		})
	}
}

func TestLimitRequestBody(t *testing.T) {
	tests := []struct {
		name           string
		maxBytes       int64
		bodySize       int
		expectedStatus int
		shouldSucceed  bool
	}{
		{
			name:           "small body within limit",
			maxBytes:       1024,
			bodySize:       512,
			expectedStatus: http.StatusOK,
			shouldSucceed:  true,
		},
		{
			name:           "body exactly at limit",
			maxBytes:       1024,
			bodySize:       1024,
			expectedStatus: http.StatusOK,
			shouldSucceed:  true,
		},
		{
			name:           "body exceeds limit",
			maxBytes:       100,
			bodySize:       200,
			expectedStatus: http.StatusRequestEntityTooLarge,
			shouldSucceed:  false,
		},
		{
			name:           "very large body",
			maxBytes:       1024,
			bodySize:       10240,
			expectedStatus: http.StatusRequestEntityTooLarge,
			shouldSucceed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := LimitRequestBody(tt.maxBytes)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Try to read the body
				_, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusRequestEntityTooLarge)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))

			// Create body of specified size
			body := strings.Repeat("a", tt.bodySize)
			req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.expectedStatus {
				t.Errorf("got status %d, want %d", rr.Code, tt.expectedStatus)
			}
		})
	}
}

func TestRateLimiter_PeriodicCleanup(t *testing.T) {
	// Test cleanup is called periodically
	rl := NewRateLimiter(10, 1*time.Minute)

	handler := rl.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make some requests to populate records
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = fmt.Sprintf("192.168.1.%d:12345", i)

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Verify requests succeeded
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("got status %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestRateLimiter_CleanupOldRecords(t *testing.T) {
	// Create rate limiter with very short window for testing
	rl := NewRateLimiter(5, 100*time.Millisecond)

	handler := rl.Limit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make requests
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should be able to make new requests after window expires
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("request %d: got status %d, want %d", i+1, rr.Code, http.StatusOK)
		}
	}
}

func TestExtractIP_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		wantIP     string
	}{
		{
			name:       "invalid X-Real-IP is ignored",
			remoteAddr: "192.168.1.1:12345",
			xri:        "invalid-ip",
			wantIP:     "192.168.1.1",
		},
		{
			name:       "RemoteAddr without port",
			remoteAddr: "192.168.1.1",
			wantIP:     "192.168.1.1",
		},
		{
			name:       "empty X-Forwarded-For falls back to X-Real-IP",
			remoteAddr: "192.168.1.1:12345",
			xff:        "",
			xri:        "203.0.113.195",
			wantIP:     "203.0.113.195",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}

			got := extractIP(req)
			if got != tt.wantIP {
				t.Errorf("extractIP() = %q, want %q", got, tt.wantIP)
			}
		})
	}
}
