package middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"catchup-feed/pkg/ratelimit"
)

// mockIPExtractorFunc is a function-based IPExtractor for testing.
type mockIPExtractorFunc func(*http.Request) (string, error)

func (f mockIPExtractorFunc) ExtractIP(r *http.Request) (string, error) {
	return f(r)
}

// TestNewIPRateLimiter tests the NewIPRateLimiter constructor.
func TestNewIPRateLimiter(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		config := IPRateLimiterConfig{
			Limit:   100,
			Window:  1 * time.Minute,
			Enabled: true,
		}
		extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
			return "192.168.1.1", nil
		})
		store := newMockRateLimitStore()
		algorithm := &mockRateLimitAlgorithm{}
		metrics := newMockRateLimitMetrics()
		cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

		limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

		if limiter == nil {
			t.Fatal("Expected non-nil limiter")
		}
		if limiter.config.Limit != 100 {
			t.Errorf("Expected limit 100, got %d", limiter.config.Limit)
		}
		if limiter.config.Window != 1*time.Minute {
			t.Errorf("Expected window 1m, got %s", limiter.config.Window)
		}
	})

	t.Run("with zero limit applies default", func(t *testing.T) {
		config := IPRateLimiterConfig{
			Limit:  0, // Zero, should apply default
			Window: 0, // Zero, should apply default
		}
		extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
			return "192.168.1.1", nil
		})
		store := newMockRateLimitStore()
		algorithm := &mockRateLimitAlgorithm{}
		metrics := newMockRateLimitMetrics()
		cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

		limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

		if limiter.config.Limit != 100 {
			t.Errorf("Expected default limit 100, got %d", limiter.config.Limit)
		}
		if limiter.config.Window != 1*time.Minute {
			t.Errorf("Expected default window 1m, got %s", limiter.config.Window)
		}
	})

	t.Run("with negative limit applies default", func(t *testing.T) {
		config := IPRateLimiterConfig{
			Limit:  -1,
			Window: -1 * time.Second,
		}
		extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
			return "192.168.1.1", nil
		})
		store := newMockRateLimitStore()
		algorithm := &mockRateLimitAlgorithm{}
		metrics := newMockRateLimitMetrics()
		cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

		limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

		if limiter.config.Limit != 100 {
			t.Errorf("Expected default limit 100, got %d", limiter.config.Limit)
		}
		if limiter.config.Window != 1*time.Minute {
			t.Errorf("Expected default window 1m, got %s", limiter.config.Window)
		}
	})
}

// TestIPRateLimiter_Middleware_Disabled tests that middleware is bypassed when disabled.
func TestIPRateLimiter_Middleware_Disabled(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: false, // Disabled
		Limit:   1,
		Window:  1 * time.Minute,
	}
	extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
		return "192.168.1.1", nil
	})
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

	limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make multiple requests, all should pass through
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, rec.Code)
		}
	}
}

// TestIPRateLimiter_Middleware_AllowWithinLimit tests requests within limit are allowed.
func TestIPRateLimiter_Middleware_AllowWithinLimit(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: true,
		Limit:   3,
		Window:  1 * time.Minute,
	}
	extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
		return "192.168.1.1", nil
	})
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

	limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 3 requests (within limit)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, rec.Code)
		}

		// Verify rate limit headers are set
		if rec.Header().Get("X-RateLimit-Limit") == "" {
			t.Error("Expected X-RateLimit-Limit header")
		}
		if rec.Header().Get("X-RateLimit-Remaining") == "" {
			t.Error("Expected X-RateLimit-Remaining header")
		}
		if rec.Header().Get("X-RateLimit-Reset") == "" {
			t.Error("Expected X-RateLimit-Reset header")
		}
		if rec.Header().Get("X-RateLimit-Type") != "ip" {
			t.Errorf("Expected X-RateLimit-Type=ip, got %s", rec.Header().Get("X-RateLimit-Type"))
		}
	}

	// Verify metrics
	if metrics.allowed != 3 {
		t.Errorf("Expected 3 allowed requests, got %d", metrics.allowed)
	}
}

// TestIPRateLimiter_Middleware_DenyExceedingLimit tests requests exceeding limit are denied.
func TestIPRateLimiter_Middleware_DenyExceedingLimit(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: true,
		Limit:   2,
		Window:  1 * time.Minute,
	}
	extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
		return "192.168.1.1", nil
	})
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

	limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 2 requests (within limit)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Request %d should succeed, got status %d", i+1, rec.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", rec.Code)
	}

	// Verify Retry-After header
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Expected Retry-After header")
	}

	// Verify JSON response
	var response map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["error"] != "rate_limit_exceeded" {
		t.Errorf("Expected error=rate_limit_exceeded, got %v", response["error"])
	}

	// Verify metrics
	if metrics.allowed != 2 {
		t.Errorf("Expected 2 allowed requests, got %d", metrics.allowed)
	}
	if metrics.denied != 1 {
		t.Errorf("Expected 1 denied request, got %d", metrics.denied)
	}
}

// TestIPRateLimiter_Middleware_IPExtractionError tests fail-open when IP extraction fails.
func TestIPRateLimiter_Middleware_IPExtractionError(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: true,
		Limit:   1,
		Window:  1 * time.Minute,
	}
	extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
		return "", errors.New("extraction failed")
	})
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

	limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request should be allowed (fail-open behavior)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 (fail-open), got %d", rec.Code)
	}
}

// TestIPRateLimiter_Middleware_CircuitBreakerOpen tests fail-open when circuit breaker is open.
func TestIPRateLimiter_Middleware_CircuitBreakerOpen(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: true,
		Limit:   1,
		Window:  1 * time.Minute,
	}
	extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
		return "192.168.1.1", nil
	})
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
		FailureThreshold: 1,
		LimiterType:      "ip",
	})

	// Force circuit breaker to open
	cb.RecordFailure()

	limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Multiple requests should all pass through (circuit is open)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200 (circuit open), got %d", i+1, rec.Code)
		}
	}
}

// TestIPRateLimiter_Middleware_RateLimitCheckError tests fail-open when rate limit check fails.
func TestIPRateLimiter_Middleware_RateLimitCheckError(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: true,
		Limit:   1,
		Window:  1 * time.Minute,
	}
	extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
		return "192.168.1.1", nil
	})
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{
		err: errors.New("rate limit check failed"),
	}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

	limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request should be allowed (fail-open behavior)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 (fail-open), got %d", rec.Code)
	}
}

// TestIPRateLimiter_Middleware_ConcurrentRequests tests thread-safety with concurrent requests.
func TestIPRateLimiter_Middleware_ConcurrentRequests(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: true,
		Limit:   50,
		Window:  1 * time.Minute,
	}
	extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
		return "192.168.1.1", nil
	})
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

	limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

// TestIPRateLimiter_Middleware_DifferentIPs tests different IPs have independent limits.
func TestIPRateLimiter_Middleware_DifferentIPs(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: true,
		Limit:   2,
		Window:  1 * time.Minute,
	}

	ips := []string{"192.168.1.1", "192.168.1.2", "192.168.1.3"}
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

	for _, ip := range ips {
		currentIP := ip
		extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
			return currentIP, nil
		})

		limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

		handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// Each IP should be able to make 2 requests
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("IP %s request %d: expected 200, got %d", ip, i+1, rec.Code)
			}
		}

		// 3rd request should be rate limited
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("IP %s 3rd request: expected 429, got %d", ip, rec.Code)
		}
	}
}

// TestIPRateLimiter_Middleware_MetricsRecorded tests metrics are recorded correctly.
func TestIPRateLimiter_Middleware_MetricsRecorded(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: true,
		Limit:   2,
		Window:  1 * time.Minute,
	}
	extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
		return "192.168.1.1", nil
	})
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

	limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 3 requests (2 allowed, 1 denied)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Verify metrics
	if metrics.allowed != 2 {
		t.Errorf("Expected 2 allowed, got %d", metrics.allowed)
	}
	if metrics.denied != 1 {
		t.Errorf("Expected 1 denied, got %d", metrics.denied)
	}
	if len(metrics.checkDurations) != 3 {
		t.Errorf("Expected 3 check duration records, got %d", len(metrics.checkDurations))
	}
}

// TestDefaultIPRateLimiterConfig tests default configuration values.
func TestDefaultIPRateLimiterConfig(t *testing.T) {
	config := DefaultIPRateLimiterConfig()

	if config.Limit != 100 {
		t.Errorf("Expected default limit 100, got %d", config.Limit)
	}
	if config.Window != 1*time.Minute {
		t.Errorf("Expected default window 1m, got %s", config.Window)
	}
	if !config.Enabled {
		t.Error("Expected default enabled=true")
	}
}

// TestIPRateLimiter_Middleware_HeadersFormat tests rate limit headers format.
func TestIPRateLimiter_Middleware_HeadersFormat(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: true,
		Limit:   5,
		Window:  1 * time.Minute,
	}
	extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
		return "192.168.1.1", nil
	})
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

	limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Verify header values
	if rec.Header().Get("X-RateLimit-Limit") != "5" {
		t.Errorf("Expected X-RateLimit-Limit=5, got %s", rec.Header().Get("X-RateLimit-Limit"))
	}
	if rec.Header().Get("X-RateLimit-Type") != "ip" {
		t.Errorf("Expected X-RateLimit-Type=ip, got %s", rec.Header().Get("X-RateLimit-Type"))
	}

	// Reset header should be a valid Unix timestamp
	reset := rec.Header().Get("X-RateLimit-Reset")
	if reset == "" {
		t.Error("Expected X-RateLimit-Reset header")
	}
}

// TestIPRateLimiter_Middleware_ErrorResponseFormat tests 429 response format.
func TestIPRateLimiter_Middleware_ErrorResponseFormat(t *testing.T) {
	config := IPRateLimiterConfig{
		Enabled: true,
		Limit:   1,
		Window:  1 * time.Minute,
	}
	extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
		return "192.168.1.1", nil
	})
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()
	cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

	limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request (allowed)
	req1 := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	// Second request (denied)
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	// Verify response format
	if rec2.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Expected Content-Type=application/json, got %s", rec2.Header().Get("Content-Type"))
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rec2.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response["error"] != "rate_limit_exceeded" {
		t.Errorf("Expected error=rate_limit_exceeded, got %v", response["error"])
	}
	if response["message"] == nil {
		t.Error("Expected message field")
	}
	if response["retry_after"] == nil {
		t.Error("Expected retry_after field")
	}
}

// TestIPRateLimiter_ExtractIP tests IP extraction delegation.
func TestIPRateLimiter_ExtractIP(t *testing.T) {
	testCases := []struct {
		name        string
		extractorIP string
		extractorErr error
		expectedIP  string
		expectError bool
	}{
		{
			name:        "successful extraction",
			extractorIP: "192.168.1.1",
			extractorErr: nil,
			expectedIP:  "192.168.1.1",
			expectError: false,
		},
		{
			name:        "extraction error",
			extractorIP: "",
			extractorErr: fmt.Errorf("extraction failed"),
			expectedIP:  "",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			extractor := mockIPExtractorFunc(func(r *http.Request) (string, error) {
				return tc.extractorIP, tc.extractorErr
			})

			config := IPRateLimiterConfig{Limit: 100, Window: 1 * time.Minute}
			store := newMockRateLimitStore()
			algorithm := &mockRateLimitAlgorithm{}
			metrics := newMockRateLimitMetrics()
			cb := ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{})

			limiter := NewIPRateLimiter(config, extractor, store, algorithm, metrics, cb)

			req := httptest.NewRequest("GET", "/test", nil)
			ip, err := limiter.extractIP(req)

			if tc.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if ip != tc.expectedIP {
					t.Errorf("Expected IP %s, got %s", tc.expectedIP, ip)
				}
			}
		})
	}
}
