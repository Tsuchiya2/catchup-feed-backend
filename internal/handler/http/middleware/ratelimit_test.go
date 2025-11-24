package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// mockIPExtractor is a mock implementation of IPExtractor for testing
type mockIPExtractor struct {
	ip  string
	err error
}

func (m *mockIPExtractor) ExtractIP(r *http.Request) (string, error) {
	return m.ip, m.err
}

// TestRateLimiter_AllowWithinLimit tests that requests within rate limit are allowed
func TestRateLimiter_AllowWithinLimit(t *testing.T) {
	extractor := &mockIPExtractor{ip: "192.168.1.1"}
	limiter := NewRateLimiter(3, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 3 requests (within limit)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: expected status %d, got %d", i+1, http.StatusOK, rec.Code)
		}
	}
}

// TestRateLimiter_BlockExceedingLimit tests that requests exceeding rate limit are blocked
func TestRateLimiter_BlockExceedingLimit(t *testing.T) {
	extractor := &mockIPExtractor{ip: "192.168.1.1"}
	limiter := NewRateLimiter(3, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 3 requests (within limit)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Request %d should succeed, got status %d", i+1, rec.Code)
		}
	}

	// 4th request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("4th request: expected status %d, got %d", http.StatusTooManyRequests, rec.Code)
	}
}

// TestRateLimiter_DifferentIPsIndependent tests that different IPs have independent limits
func TestRateLimiter_DifferentIPsIndependent(t *testing.T) {
	limiter := NewRateLimiter(2, time.Minute, nil)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ips := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}

	for _, ip := range ips {
		// Each IP should be able to make 2 requests
		for i := 0; i < 2; i++ {
			extractor := &mockIPExtractor{ip: ip}
			limiter.ipExtractor = extractor

			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("IP %s request %d: expected status %d, got %d", ip, i+1, http.StatusOK, rec.Code)
			}
		}
	}

	// Verify each IP is now rate limited
	for _, ip := range ips {
		extractor := &mockIPExtractor{ip: ip}
		limiter.ipExtractor = extractor

		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("IP %s 3rd request: expected status %d, got %d", ip, http.StatusTooManyRequests, rec.Code)
		}
	}
}

// TestRateLimiter_WindowSliding tests that sliding window works correctly
func TestRateLimiter_WindowSliding(t *testing.T) {
	extractor := &mockIPExtractor{ip: "192.168.1.1"}
	limiter := NewRateLimiter(2, 100*time.Millisecond, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 2 requests (reach limit)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Request %d should succeed", i+1)
		}
	}

	// 3rd request should be blocked
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Error("3rd request should be rate limited")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Now requests should be allowed again
	req = httptest.NewRequest("GET", "/test", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Request after window expiry: expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

// TestRateLimiter_CleanupExpired tests cleanup of expired entries
func TestRateLimiter_CleanupExpired(t *testing.T) {
	extractor := &mockIPExtractor{ip: "192.168.1.1"}
	limiter := NewRateLimiter(5, 50*time.Millisecond, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make some requests
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Verify IP is in the map
	limiter.mu.Lock()
	if _, exists := limiter.requests["192.168.1.1"]; !exists {
		t.Fatal("Expected IP to be in requests map")
	}
	limiter.mu.Unlock()

	// Wait for entries to expire
	time.Sleep(100 * time.Millisecond)

	// Run cleanup
	limiter.CleanupExpired()

	// Verify IP was removed
	limiter.mu.Lock()
	if _, exists := limiter.requests["192.168.1.1"]; exists {
		t.Error("Expected IP to be removed after cleanup")
	}
	limiter.mu.Unlock()
}

// TestRateLimiter_ConcurrentRequests tests thread-safety with concurrent requests
func TestRateLimiter_ConcurrentRequests(t *testing.T) {
	extractor := &mockIPExtractor{ip: "192.168.1.1"}
	limiter := NewRateLimiter(50, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	successCount := 0
	rateLimitCount := 0
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			mu.Lock()
			switch rec.Code {
			case http.StatusOK:
				successCount++
			case http.StatusTooManyRequests:
				rateLimitCount++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Verify that exactly 50 requests succeeded (the limit)
	if successCount != 50 {
		t.Errorf("Expected 50 successful requests, got %d", successCount)
	}

	// Verify that the remaining requests were rate limited
	if rateLimitCount != 50 {
		t.Errorf("Expected 50 rate limited requests, got %d", rateLimitCount)
	}
}

// TestRateLimiter_IPExtractorError tests handling of IPExtractor errors
func TestRateLimiter_IPExtractorError(t *testing.T) {
	extractor := &mockIPExtractor{
		ip:  "",
		err: fmt.Errorf("extraction failed"),
	}
	limiter := NewRateLimiter(5, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:8080"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should fallback to RemoteAddr extraction and succeed
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d when extractor returns error, got %d", http.StatusOK, rec.Code)
	}
}

// TestRateLimiter_WithRemoteAddrExtractor tests RateLimiter with RemoteAddrExtractor
func TestRateLimiter_WithRemoteAddrExtractor(t *testing.T) {
	extractor := &RemoteAddrExtractor{}
	limiter := NewRateLimiter(3, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make 3 requests from same IP (within limit)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:54321"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: expected status %d, got %d", i+1, http.StatusOK, rec.Code)
		}
	}

	// 4th request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:54321"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("4th request: expected status %d, got %d", http.StatusTooManyRequests, rec.Code)
	}
}

// TestRateLimiter_WithTrustedProxyExtractor tests RateLimiter with TrustedProxyExtractor
func TestRateLimiter_WithTrustedProxyExtractor(t *testing.T) {
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
		},
	}
	extractor := NewTrustedProxyExtractor(config)
	limiter := NewRateLimiter(3, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make 3 requests with same client IP in X-Forwarded-For
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "10.0.0.5:54321" // Trusted proxy
		req.Header.Set("X-Forwarded-For", "203.0.113.1")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: expected status %d, got %d", i+1, http.StatusOK, rec.Code)
		}
	}

	// 4th request should be rate limited (same client IP)
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.5:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("4th request: expected status %d, got %d", http.StatusTooManyRequests, rec.Code)
	}
}

// TestRateLimiter_PerformanceHighThroughput is a performance test
// verifying the limiter can handle >1000 requests/sec
func TestRateLimiter_PerformanceHighThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	extractor := &RemoteAddrExtractor{}
	limiter := NewRateLimiter(10000, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	const numRequests = 2000
	start := time.Now()

	for i := 0; i < numRequests; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = fmt.Sprintf("192.168.1.%d:8080", i%255)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	duration := time.Since(start)
	requestsPerSec := float64(numRequests) / duration.Seconds()

	if requestsPerSec < 1000 {
		t.Errorf("Performance too low: %.2f req/sec (expected >1000)", requestsPerSec)
	}

	t.Logf("Performance: %.2f requests/sec", requestsPerSec)
}

// TestRateLimiter_Allow_EdgeCases tests edge cases in the allow method
func TestRateLimiter_Allow_EdgeCases(t *testing.T) {
	extractor := &mockIPExtractor{ip: "192.168.1.1"}
	limiter := NewRateLimiter(1, 100*time.Millisecond, extractor)

	// First request should be allowed
	if !limiter.allow("192.168.1.1") {
		t.Error("First request should be allowed")
	}

	// Second request immediately after should be blocked
	if limiter.allow("192.168.1.1") {
		t.Error("Second request should be blocked")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Request should be allowed again
	if !limiter.allow("192.168.1.1") {
		t.Error("Request after window expiry should be allowed")
	}
}

// TestRateLimiter_CleanupPreservesActiveIPs tests that cleanup
// doesn't remove IPs with valid timestamps
func TestRateLimiter_CleanupPreservesActiveIPs(t *testing.T) {
	extractor := &mockIPExtractor{ip: "192.168.1.1"}
	limiter := NewRateLimiter(5, time.Minute, extractor)

	// Make a request
	limiter.allow("192.168.1.1")

	// Immediately cleanup (timestamps should still be valid)
	limiter.CleanupExpired()

	// Verify IP still exists
	limiter.mu.Lock()
	if _, exists := limiter.requests["192.168.1.1"]; !exists {
		t.Error("Expected IP to still be in requests map after cleanup")
	}
	limiter.mu.Unlock()
}

// TestRateLimiter_MultipleIPsWithCleanup tests cleanup with multiple IPs
func TestRateLimiter_MultipleIPsWithCleanup(t *testing.T) {
	limiter := NewRateLimiter(5, 50*time.Millisecond, nil)

	// Add requests for multiple IPs
	ips := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	for _, ip := range ips {
		limiter.allow(ip)
	}

	// Verify all IPs are in the map
	limiter.mu.Lock()
	if len(limiter.requests) != 3 {
		t.Errorf("Expected 3 IPs in map, got %d", len(limiter.requests))
	}
	limiter.mu.Unlock()

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Run cleanup
	limiter.CleanupExpired()

	// Verify all IPs were removed
	limiter.mu.Lock()
	if len(limiter.requests) != 0 {
		t.Errorf("Expected 0 IPs after cleanup, got %d", len(limiter.requests))
	}
	limiter.mu.Unlock()
}

// TestRateLimiter_InvalidRemoteAddrFallback tests fallback when
// RemoteAddr extraction fails
func TestRateLimiter_InvalidRemoteAddrFallback(t *testing.T) {
	extractor := &mockIPExtractor{
		ip:  "",
		err: fmt.Errorf("extraction failed"),
	}
	limiter := NewRateLimiter(5, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "invalid-addr"
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should return internal server error when RemoteAddr extraction also fails
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d when RemoteAddr extraction fails, got %d",
			http.StatusInternalServerError, rec.Code)
	}
}
