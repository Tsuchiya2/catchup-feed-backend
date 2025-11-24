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

/* ───────── TASK-014: Integration Tests for Rate Limiting ───────── */

// TestRateLimiter_Integration_RemoteAddrOnly tests end-to-end rate limiting
// using only RemoteAddr (no proxy trust)
func TestRateLimiter_Integration_RemoteAddrOnly(t *testing.T) {
	// Setup: Create rate limiter with RemoteAddr extraction only
	extractor := &RemoteAddrExtractor{}
	limiter := NewRateLimiter(3, time.Minute, extractor)

	// Create test server
	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	server := httptest.NewServer(handler)
	defer server.Close()

	// Test: Make 5 requests with X-Forwarded-For header (should be ignored)
	client := &http.Client{}
	successCount := 0
	rateLimitCount := 0

	for i := 0; i < 5; i++ {
		req, err := http.NewRequest("GET", server.URL+"/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Add X-Forwarded-For header (should be completely ignored)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i))

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			successCount++
		case http.StatusTooManyRequests:
			rateLimitCount++
		}

		_ = resp.Body.Close()
	}

	// Verify: First 3 requests succeed, next 2 are rate limited
	// All requests should be counted under the same RemoteAddr (test server IP)
	if successCount != 3 {
		t.Errorf("Expected 3 successful requests, got %d", successCount)
	}

	if rateLimitCount != 2 {
		t.Errorf("Expected 2 rate limited requests, got %d", rateLimitCount)
	}
}

// TestRateLimiter_Integration_TrustedProxy tests end-to-end rate limiting
// with trusted proxy configuration
func TestRateLimiter_Integration_TrustedProxy(t *testing.T) {
	// Setup: Create rate limiter with trusted proxy configuration
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("127.0.0.0/8"), // Trust localhost
		},
	}
	extractor := NewTrustedProxyExtractor(config)
	limiter := NewRateLimiter(3, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	// Create test server that will appear to come from localhost
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Manually set RemoteAddr to simulate trusted proxy
		r.RemoteAddr = "127.0.0.1:54321"
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	successCount := 0
	rateLimitCount := 0

	// Test: Make 5 requests with same client IP in X-Forwarded-For
	for i := 0; i < 5; i++ {
		req, err := http.NewRequest("GET", server.URL+"/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// All requests from same client IP
		req.Header.Set("X-Forwarded-For", "203.0.113.100")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			successCount++
		case http.StatusTooManyRequests:
			rateLimitCount++
		}

		_ = resp.Body.Close()
	}

	// Verify: All requests should be rate-limited under the client IP (203.0.113.100)
	// Not under the proxy IP (127.0.0.1)
	if successCount != 3 {
		t.Errorf("Expected 3 successful requests, got %d", successCount)
	}

	if rateLimitCount != 2 {
		t.Errorf("Expected 2 rate limited requests, got %d", rateLimitCount)
	}
}

// TestRateLimiter_Integration_UntrustedProxy tests that untrusted proxies
// cannot bypass rate limiting by spoofing headers
func TestRateLimiter_Integration_UntrustedProxy(t *testing.T) {
	// Setup: Create rate limiter with trusted proxy, but requests will come
	// from untrusted source
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"), // Trust only 10.0.0.0/8
		},
	}
	extractor := NewTrustedProxyExtractor(config)
	limiter := NewRateLimiter(3, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	// Create test server that simulates untrusted source (not in 10.0.0.0/8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set RemoteAddr to untrusted IP
		r.RemoteAddr = "203.0.113.50:12345"
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	successCount := 0
	rateLimitCount := 0

	// Test: Make 5 requests with DIFFERENT X-Forwarded-For values
	// Attempting to bypass rate limiting by rotating IPs
	for i := 0; i < 5; i++ {
		req, err := http.NewRequest("GET", server.URL+"/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		// Try to spoof different client IPs
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.168.1.%d", i))

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			successCount++
		case http.StatusTooManyRequests:
			rateLimitCount++
		}

		_ = resp.Body.Close()
	}

	// Verify: All requests should be rate-limited under the proxy IP (203.0.113.50)
	// X-Forwarded-For should be completely ignored
	if successCount != 3 {
		t.Errorf("Expected 3 successful requests (untrusted proxy cannot bypass), got %d", successCount)
	}

	if rateLimitCount != 2 {
		t.Errorf("Expected 2 rate limited requests, got %d", rateLimitCount)
	}
}

// TestRateLimiter_Integration_ConfigurationError tests handling of
// invalid configuration at startup
func TestRateLimiter_Integration_ConfigurationError(t *testing.T) {
	testCases := []struct {
		name        string
		setEnv      func(*testing.T)
		expectError bool
	}{
		{
			name: "valid configuration",
			setEnv: func(t *testing.T) {
				t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
				t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "10.0.0.0/8")
			},
			expectError: false,
		},
		{
			name: "enabled but empty proxies",
			setEnv: func(t *testing.T) {
				t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
				t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "")
			},
			expectError: true,
		},
		{
			name: "invalid CIDR format",
			setEnv: func(t *testing.T) {
				t.Setenv("RATE_LIMIT_TRUST_PROXY", "true")
				t.Setenv("RATE_LIMIT_TRUSTED_PROXIES", "invalid-cidr")
			},
			expectError: true,
		},
		{
			name: "disabled proxy trust",
			setEnv: func(t *testing.T) {
				t.Setenv("RATE_LIMIT_TRUST_PROXY", "false")
			},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setEnv(t)

			config, err := LoadTrustedProxyConfig()

			if tc.expectError {
				if err == nil {
					t.Error("Expected error for invalid configuration, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}

				// If no error, should be able to create rate limiter
				var extractor IPExtractor
				if config.Enabled {
					extractor = NewTrustedProxyExtractor(*config)
				} else {
					extractor = &RemoteAddrExtractor{}
				}

				limiter := NewRateLimiter(5, time.Minute, extractor)
				if limiter == nil {
					t.Error("Failed to create rate limiter with valid config")
				}
			}
		})
	}
}

// TestRateLimiter_Integration_MultipleConcurrentClients tests rate limiting
// with multiple concurrent clients
func TestRateLimiter_Integration_MultipleConcurrentClients(t *testing.T) {
	// Setup: Create rate limiter
	extractor := &RemoteAddrExtractor{}
	limiter := NewRateLimiter(10, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	// Track results by IP
	results := make(map[string]*struct {
		success int
		limited int
		mu      sync.Mutex
	})

	// Create test handler that simulates multiple clients
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract client ID from query parameter
		clientID := r.URL.Query().Get("client")
		if clientID == "" {
			http.Error(w, "Missing client parameter", http.StatusBadRequest)
			return
		}

		// Simulate different RemoteAddr for each client
		r.RemoteAddr = fmt.Sprintf("192.168.1.%s:12345", clientID)
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	// Test: 5 clients each making 15 requests concurrently
	numClients := 5
	requestsPerClient := 15
	var wg sync.WaitGroup

	for clientID := 1; clientID <= numClients; clientID++ {
		clientIDStr := fmt.Sprintf("%d", clientID)
		results[clientIDStr] = &struct {
			success int
			limited int
			mu      sync.Mutex
		}{}

		wg.Add(1)
		go func(cid string) {
			defer wg.Done()

			client := &http.Client{}
			for i := 0; i < requestsPerClient; i++ {
				url := fmt.Sprintf("%s/test?client=%s", server.URL, cid)
				resp, err := client.Get(url)
				if err != nil {
					t.Errorf("Request failed for client %s: %v", cid, err)
					continue
				}

				result := results[cid]
				result.mu.Lock()
				switch resp.StatusCode {
				case http.StatusOK:
					result.success++
				case http.StatusTooManyRequests:
					result.limited++
				}
				result.mu.Unlock()

				_ = resp.Body.Close()
			}
		}(clientIDStr)
	}

	wg.Wait()

	// Verify: Each client should have 10 successful requests and 5 rate limited
	for clientID, result := range results {
		if result.success != 10 {
			t.Errorf("Client %s: expected 10 successful requests, got %d", clientID, result.success)
		}
		if result.limited != 5 {
			t.Errorf("Client %s: expected 5 rate limited requests, got %d", clientID, result.limited)
		}
	}
}

// TestRateLimiter_Integration_ProxyChain tests handling of proxy chains
// in X-Forwarded-For header
func TestRateLimiter_Integration_ProxyChain(t *testing.T) {
	// Setup: Trust the immediate proxy
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("127.0.0.1/32"),
		},
	}
	extractor := NewTrustedProxyExtractor(config)
	limiter := NewRateLimiter(3, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "127.0.0.1:54321" // Trusted proxy
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	client := &http.Client{}

	// Test: Requests with proxy chain in X-Forwarded-For
	testCases := []struct {
		name            string
		xffHeader       string
		expectedSuccess int
		expectedLimited int
	}{
		{
			name:            "proxy chain - same client",
			xffHeader:       "203.0.113.1, 10.0.0.1, 172.16.0.1",
			expectedSuccess: 3,
			expectedLimited: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			successCount := 0
			rateLimitCount := 0

			// Make 5 requests with the same proxy chain
			for i := 0; i < 5; i++ {
				req, err := http.NewRequest("GET", server.URL+"/test", nil)
				if err != nil {
					t.Fatalf("Failed to create request: %v", err)
				}

				req.Header.Set("X-Forwarded-For", tc.xffHeader)

				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("Request failed: %v", err)
				}

				switch resp.StatusCode {
				case http.StatusOK:
					successCount++
				case http.StatusTooManyRequests:
					rateLimitCount++
				}

				_ = resp.Body.Close()
			}

			if successCount != tc.expectedSuccess {
				t.Errorf("Expected %d successful requests, got %d", tc.expectedSuccess, successCount)
			}

			if rateLimitCount != tc.expectedLimited {
				t.Errorf("Expected %d rate limited requests, got %d", tc.expectedLimited, rateLimitCount)
			}
		})
	}
}

// TestRateLimiter_Integration_IPv6 tests rate limiting with IPv6 addresses
func TestRateLimiter_Integration_IPv6(t *testing.T) {
	// Setup: Trust IPv6 proxy
	config := TrustedProxyConfig{
		Enabled: true,
		AllowedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("::1/128"), // IPv6 localhost
		},
	}
	extractor := NewTrustedProxyExtractor(config)
	limiter := NewRateLimiter(3, time.Minute, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "[::1]:54321" // IPv6 trusted proxy
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	successCount := 0
	rateLimitCount := 0

	// Test: Requests with IPv6 client IP in X-Forwarded-For
	for i := 0; i < 5; i++ {
		req, err := http.NewRequest("GET", server.URL+"/test", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		req.Header.Set("X-Forwarded-For", "2001:db8::1")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			successCount++
		case http.StatusTooManyRequests:
			rateLimitCount++
		}

		_ = resp.Body.Close()
	}

	// Verify rate limiting works with IPv6
	if successCount != 3 {
		t.Errorf("Expected 3 successful requests, got %d", successCount)
	}

	if rateLimitCount != 2 {
		t.Errorf("Expected 2 rate limited requests, got %d", rateLimitCount)
	}
}

// TestRateLimiter_Integration_CleanupDuringOperation tests that
// cleanup doesn't interfere with active rate limiting
func TestRateLimiter_Integration_CleanupDuringOperation(t *testing.T) {
	extractor := &RemoteAddrExtractor{}
	limiter := NewRateLimiter(5, 100*time.Millisecond, extractor)

	handler := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.RemoteAddr = "192.168.1.1:12345"
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	// Start periodic cleanup
	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				limiter.CleanupExpired()
			case <-done:
				return
			}
		}
	}()
	defer close(done)

	client := &http.Client{}

	// Make requests while cleanup is running
	for i := 0; i < 10; i++ {
		resp, err := client.Get(server.URL + "/test")
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		_ = resp.Body.Close()

		// Small delay between requests
		time.Sleep(10 * time.Millisecond)
	}

	// Test passes if no race conditions or panics occur
	t.Log("Cleanup during operation completed successfully")
}
