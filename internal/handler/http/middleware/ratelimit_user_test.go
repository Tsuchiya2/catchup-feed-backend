package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"catchup-feed/pkg/ratelimit"
)

// mockUserExtractor is a mock implementation of UserExtractor for testing.
type mockUserExtractor struct {
	userID string
	tier   ratelimit.UserTier
	ok     bool
}

func (m *mockUserExtractor) ExtractUser(ctx context.Context) (string, ratelimit.UserTier, bool) {
	return m.userID, m.tier, m.ok
}

// mockUserTierProvider is a mock implementation of UserTierProvider for testing.
type mockUserTierProvider struct {
	tiers map[string]ratelimit.UserTier
}

func (m *mockUserTierProvider) GetUserTier(ctx context.Context, userID string) ratelimit.UserTier {
	if tier, ok := m.tiers[userID]; ok {
		return tier
	}
	return ratelimit.TierBasic
}

// TestNewJWTUserExtractor tests the JWTUserExtractor constructor.
func TestNewJWTUserExtractor(t *testing.T) {
	t.Run("with tier provider", func(t *testing.T) {
		provider := &mockUserTierProvider{
			tiers: map[string]ratelimit.UserTier{
				"user1@example.com": ratelimit.TierPremium,
			},
		}
		extractor := NewJWTUserExtractor("user", provider)

		if extractor == nil {
			t.Fatal("Expected non-nil extractor")
		}
	})

	t.Run("with nil tier provider uses default", func(t *testing.T) {
		extractor := NewJWTUserExtractor("user", nil)

		if extractor == nil {
			t.Fatal("Expected non-nil extractor")
		}

		// Should use default tier provider
		ctx := context.WithValue(context.Background(), "user", "test@example.com")
		_, tier, ok := extractor.ExtractUser(ctx)

		if !ok {
			t.Error("Expected user extraction to succeed")
		}
		if tier != ratelimit.TierBasic {
			t.Errorf("Expected default tier to be Basic, got %s", tier)
		}
	})
}

// TestJWTUserExtractor_ExtractUser tests user extraction from context.
func TestJWTUserExtractor_ExtractUser(t *testing.T) {
	testCases := []struct {
		name         string
		contextKey   interface{}
		contextValue interface{}
		tierProvider UserTierProvider
		expectedUser string
		expectedTier ratelimit.UserTier
		expectedOK   bool
	}{
		{
			name:         "valid user in context",
			contextKey:   "user",
			contextValue: "user1@example.com",
			tierProvider: &mockUserTierProvider{
				tiers: map[string]ratelimit.UserTier{
					"user1@example.com": ratelimit.TierPremium,
				},
			},
			expectedUser: "user1@example.com",
			expectedTier: ratelimit.TierPremium,
			expectedOK:   true,
		},
		{
			name:         "user not in context",
			contextKey:   "other",
			contextValue: "something",
			tierProvider: nil,
			expectedUser: "",
			expectedTier: "",
			expectedOK:   false,
		},
		{
			name:         "nil context value",
			contextKey:   "user",
			contextValue: nil,
			tierProvider: nil,
			expectedUser: "",
			expectedTier: "",
			expectedOK:   false,
		},
		{
			name:         "non-string context value",
			contextKey:   "user",
			contextValue: 123,
			tierProvider: nil,
			expectedUser: "",
			expectedTier: "",
			expectedOK:   false,
		},
		{
			name:         "empty string user",
			contextKey:   "user",
			contextValue: "",
			tierProvider: nil,
			expectedUser: "",
			expectedTier: "",
			expectedOK:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			extractor := NewJWTUserExtractor("user", tc.tierProvider)

			ctx := context.Background()
			if tc.contextValue != nil {
				ctx = context.WithValue(ctx, tc.contextKey, tc.contextValue)
			}

			userID, tier, ok := extractor.ExtractUser(ctx)

			if ok != tc.expectedOK {
				t.Errorf("Expected ok=%v, got %v", tc.expectedOK, ok)
			}
			if userID != tc.expectedUser {
				t.Errorf("Expected user=%s, got %s", tc.expectedUser, userID)
			}
			if tier != tc.expectedTier {
				t.Errorf("Expected tier=%s, got %s", tc.expectedTier, tier)
			}
		})
	}
}

// TestDefaultTierProvider tests the DefaultTierProvider.
func TestDefaultTierProvider(t *testing.T) {
	provider := &DefaultTierProvider{}
	ctx := context.Background()

	tier := provider.GetUserTier(ctx, "any-user")
	if tier != ratelimit.TierBasic {
		t.Errorf("Expected TierBasic, got %s", tier)
	}
}

// TestNewUserRateLimiter tests the UserRateLimiter constructor.
func TestNewUserRateLimiter(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		config := UserRateLimiterConfig{
			Store:          newMockRateLimitStore(),
			Algorithm:      &mockRateLimitAlgorithm{},
			Metrics:        newMockRateLimitMetrics(),
			CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{}),
			UserExtractor: &mockUserExtractor{
				userID: "user1@example.com",
				tier:   ratelimit.TierBasic,
				ok:     true,
			},
			TierLimits:          NewDefaultTierLimits(),
			DefaultLimit:        1000,
			DefaultWindow:       1 * time.Hour,
			SkipUnauthenticated: true,
		}

		limiter := NewUserRateLimiter(config)

		if limiter == nil {
			t.Fatal("Expected non-nil limiter")
		}
		if limiter.config.DefaultLimit != 1000 {
			t.Errorf("Expected default limit 1000, got %d", limiter.config.DefaultLimit)
		}
	})

	t.Run("applies defaults for zero values", func(t *testing.T) {
		config := UserRateLimiterConfig{
			Store:          newMockRateLimitStore(),
			Algorithm:      &mockRateLimitAlgorithm{},
			Metrics:        newMockRateLimitMetrics(),
			CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{}),
			UserExtractor:  &mockUserExtractor{},
			DefaultLimit:   0, // Should apply default
			DefaultWindow:  0, // Should apply default
		}

		limiter := NewUserRateLimiter(config)

		if limiter.config.DefaultLimit != 1000 {
			t.Errorf("Expected default limit 1000, got %d", limiter.config.DefaultLimit)
		}
		if limiter.config.DefaultWindow != 1*time.Hour {
			t.Errorf("Expected default window 1h, got %s", limiter.config.DefaultWindow)
		}
		if limiter.config.Clock == nil {
			t.Error("Expected default clock to be set")
		}
	})
}

// TestUserRateLimiter_Middleware_SkipUnauthenticated tests skipping unauthenticated requests.
func TestUserRateLimiter_Middleware_SkipUnauthenticated(t *testing.T) {
	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{
			ok: false, // No user in context
		},
		DefaultLimit:        10,
		DefaultWindow:       1 * time.Minute,
		SkipUnauthenticated: true,
	}

	limiter := NewUserRateLimiter(config)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Multiple unauthenticated requests should all pass through
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, rec.Code)
		}
	}
}

// TestUserRateLimiter_Middleware_TierBasedLimits tests tier-based rate limiting.
func TestUserRateLimiter_Middleware_TierBasedLimits(t *testing.T) {
	testCases := []struct {
		name       string
		tier       ratelimit.UserTier
		limit      int
		window     time.Duration
		numAllowed int
	}{
		{
			name:       "admin tier",
			tier:       ratelimit.TierAdmin,
			limit:      10000,
			window:     1 * time.Hour,
			numAllowed: 10000,
		},
		{
			name:       "premium tier",
			tier:       ratelimit.TierPremium,
			limit:      5000,
			window:     1 * time.Hour,
			numAllowed: 5000,
		},
		{
			name:       "basic tier",
			tier:       ratelimit.TierBasic,
			limit:      1000,
			window:     1 * time.Hour,
			numAllowed: 1000,
		},
		{
			name:       "viewer tier",
			tier:       ratelimit.TierViewer,
			limit:      500,
			window:     1 * time.Hour,
			numAllowed: 500,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := UserRateLimiterConfig{
				Store:     newMockRateLimitStore(),
				Algorithm: &mockRateLimitAlgorithm{},
				Metrics:   newMockRateLimitMetrics(),
				CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
					LimiterType: "user",
				}),
				UserExtractor: &mockUserExtractor{
					userID: "user@example.com",
					tier:   tc.tier,
					ok:     true,
				},
				TierLimits:    NewDefaultTierLimits(),
				DefaultLimit:  1000,
				DefaultWindow: 1 * time.Hour,
			}

			limiter := NewUserRateLimiter(config)

			// Verify tier limit is correctly retrieved
			limit, window := limiter.getTierLimit(tc.tier)
			if limit != tc.limit {
				t.Errorf("Expected limit %d, got %d", tc.limit, limit)
			}
			if window != tc.window {
				t.Errorf("Expected window %s, got %s", tc.window, window)
			}
		})
	}
}

// TestUserRateLimiter_Middleware_AllowWithinLimit tests requests within limit are allowed.
func TestUserRateLimiter_Middleware_AllowWithinLimit(t *testing.T) {
	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{
			userID: "user@example.com",
			tier:   ratelimit.TierBasic,
			ok:     true,
		},
		TierLimits: map[ratelimit.UserTier]TierLimit{
			ratelimit.TierBasic: {
				Limit:  3,
				Window: 1 * time.Minute,
			},
		},
		DefaultLimit:  1000,
		DefaultWindow: 1 * time.Hour,
	}

	limiter := NewUserRateLimiter(config)

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

		// Verify rate limit headers
		if rec.Header().Get("X-RateLimit-Limit") == "" {
			t.Error("Expected X-RateLimit-Limit header")
		}
		if rec.Header().Get("X-RateLimit-Type") != "user" {
			t.Errorf("Expected X-RateLimit-Type=user, got %s", rec.Header().Get("X-RateLimit-Type"))
		}
	}
}

// TestUserRateLimiter_Middleware_DenyExceedingLimit tests requests exceeding limit are denied.
func TestUserRateLimiter_Middleware_DenyExceedingLimit(t *testing.T) {
	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{
			userID: "user@example.com",
			tier:   ratelimit.TierBasic,
			ok:     true,
		},
		TierLimits: map[ratelimit.UserTier]TierLimit{
			ratelimit.TierBasic: {
				Limit:  2,
				Window: 1 * time.Minute,
			},
		},
		DefaultLimit:  1000,
		DefaultWindow: 1 * time.Hour,
	}

	limiter := NewUserRateLimiter(config)

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

	if response["error"] != "rate limit exceeded" {
		t.Errorf("Expected error='rate limit exceeded', got %v", response["error"])
	}
}

// TestUserRateLimiter_Middleware_CircuitBreakerOpen tests fail-open when circuit breaker is open.
func TestUserRateLimiter_Middleware_CircuitBreakerOpen(t *testing.T) {
	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			FailureThreshold: 1,
			LimiterType:      "user",
		}),
		UserExtractor: &mockUserExtractor{
			userID: "user@example.com",
			tier:   ratelimit.TierBasic,
			ok:     true,
		},
		TierLimits: map[ratelimit.UserTier]TierLimit{
			ratelimit.TierBasic: {
				Limit:  1,
				Window: 1 * time.Minute,
			},
		},
	}

	limiter := NewUserRateLimiter(config)

	// Force circuit breaker to open
	config.CircuitBreaker.RecordFailure()

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Multiple requests should all pass through (circuit is open)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200 (circuit open), got %d", i+1, rec.Code)
		}
	}
}

// TestUserRateLimiter_Middleware_ConcurrentRequests tests thread-safety with concurrent requests.
func TestUserRateLimiter_Middleware_ConcurrentRequests(t *testing.T) {
	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{
			userID: "user@example.com",
			tier:   ratelimit.TierBasic,
			ok:     true,
		},
		TierLimits: map[ratelimit.UserTier]TierLimit{
			ratelimit.TierBasic: {
				Limit:  50,
				Window: 1 * time.Minute,
			},
		},
	}

	limiter := NewUserRateLimiter(config)

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

// TestUserRateLimiter_GetTierLimit tests tier limit retrieval.
func TestUserRateLimiter_GetTierLimit(t *testing.T) {
	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{},
		TierLimits: map[ratelimit.UserTier]TierLimit{
			ratelimit.TierPremium: {
				Limit:  5000,
				Window: 1 * time.Hour,
			},
		},
		DefaultLimit:  1000,
		DefaultWindow: 1 * time.Hour,
	}

	limiter := NewUserRateLimiter(config)

	testCases := []struct {
		name           string
		tier           ratelimit.UserTier
		expectedLimit  int
		expectedWindow time.Duration
	}{
		{
			name:           "configured tier",
			tier:           ratelimit.TierPremium,
			expectedLimit:  5000,
			expectedWindow: 1 * time.Hour,
		},
		{
			name:           "unconfigured tier falls back to default",
			tier:           ratelimit.TierBasic,
			expectedLimit:  1000,
			expectedWindow: 1 * time.Hour,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			limit, window := limiter.getTierLimit(tc.tier)

			if limit != tc.expectedLimit {
				t.Errorf("Expected limit %d, got %d", tc.expectedLimit, limit)
			}
			if window != tc.expectedWindow {
				t.Errorf("Expected window %s, got %s", tc.expectedWindow, window)
			}
		})
	}
}

// TestHashUserID tests user ID hashing.
func TestHashUserID(t *testing.T) {
	testCases := []struct {
		name     string
		userID   string
		expected string
	}{
		{
			name:     "simple email",
			userID:   "user@example.com",
			expected: "b4c9a289323b21a01c3e940f150eb9b8c542587f1abfd8f0e1cc1ffc5e475514", // SHA-256 of "user@example.com"
		},
		{
			name:     "empty string",
			userID:   "",
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", // SHA-256 of empty string
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hash := hashUserID(tc.userID)

			if hash != tc.expected {
				t.Errorf("Expected hash %s, got %s", tc.expected, hash)
			}

			// Verify hash is deterministic
			hash2 := hashUserID(tc.userID)
			if hash != hash2 {
				t.Error("Hash should be deterministic")
			}
		})
	}
}

// TestNewDefaultTierLimits tests default tier limits.
func TestNewDefaultTierLimits(t *testing.T) {
	limits := NewDefaultTierLimits()

	testCases := []struct {
		tier           ratelimit.UserTier
		expectedLimit  int
		expectedWindow time.Duration
	}{
		{ratelimit.TierAdmin, 10000, 1 * time.Hour},
		{ratelimit.TierPremium, 5000, 1 * time.Hour},
		{ratelimit.TierBasic, 1000, 1 * time.Hour},
		{ratelimit.TierViewer, 500, 1 * time.Hour},
	}

	for _, tc := range testCases {
		t.Run(tc.tier.String(), func(t *testing.T) {
			limit, ok := limits[tc.tier]
			if !ok {
				t.Fatalf("Expected tier %s to be configured", tc.tier)
			}

			if limit.Limit != tc.expectedLimit {
				t.Errorf("Expected limit %d, got %d", tc.expectedLimit, limit.Limit)
			}
			if limit.Window != tc.expectedWindow {
				t.Errorf("Expected window %s, got %s", tc.expectedWindow, limit.Window)
			}
		})
	}
}

// TestUserRateLimiter_Middleware_MetricsRecorded tests metrics are recorded correctly.
func TestUserRateLimiter_Middleware_MetricsRecorded(t *testing.T) {
	metrics := newMockRateLimitMetrics()

	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   metrics,
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{
			userID: "user@example.com",
			tier:   ratelimit.TierBasic,
			ok:     true,
		},
		TierLimits: map[ratelimit.UserTier]TierLimit{
			ratelimit.TierBasic: {
				Limit:  2,
				Window: 1 * time.Minute,
			},
		},
	}

	limiter := NewUserRateLimiter(config)

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

// TestUserRateLimiter_Middleware_ErrorResponseFormat tests 429 response format.
func TestUserRateLimiter_Middleware_ErrorResponseFormat(t *testing.T) {
	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{
			userID: "user@example.com",
			tier:   ratelimit.TierBasic,
			ok:     true,
		},
		TierLimits: map[ratelimit.UserTier]TierLimit{
			ratelimit.TierBasic: {
				Limit:  1,
				Window: 1 * time.Minute,
			},
		},
	}

	limiter := NewUserRateLimiter(config)

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

	if response["error"] != "rate limit exceeded" {
		t.Errorf("Expected error='rate limit exceeded', got %v", response["error"])
	}
	if response["message"] == nil {
		t.Error("Expected message field")
	}
	if response["retry_after_seconds"] == nil {
		t.Error("Expected retry_after_seconds field")
	}
	if response["limit"] == nil {
		t.Error("Expected limit field")
	}
}

// TestUserRateLimiter_Middleware_HeadersFormat tests rate limit headers format.
func TestUserRateLimiter_Middleware_HeadersFormat(t *testing.T) {
	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{
			userID: "user@example.com",
			tier:   ratelimit.TierBasic,
			ok:     true,
		},
		TierLimits: map[ratelimit.UserTier]TierLimit{
			ratelimit.TierBasic: {
				Limit:  5,
				Window: 1 * time.Minute,
			},
		},
	}

	limiter := NewUserRateLimiter(config)

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
	if rec.Header().Get("X-RateLimit-Type") != "user" {
		t.Errorf("Expected X-RateLimit-Type=user, got %s", rec.Header().Get("X-RateLimit-Type"))
	}

	// Reset header should be a valid value
	reset := rec.Header().Get("X-RateLimit-Reset")
	if reset == "" {
		t.Error("Expected X-RateLimit-Reset header")
	}
}

// TestUserRateLimiter_Middleware_AnonymousUser tests handling of anonymous users when not skipping.
func TestUserRateLimiter_Middleware_AnonymousUser(t *testing.T) {
	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{
			ok: false, // No user in context
		},
		TierLimits: map[ratelimit.UserTier]TierLimit{
			ratelimit.TierBasic: {
				Limit:  2,
				Window: 1 * time.Minute,
			},
		},
		SkipUnauthenticated: false, // Do not skip
	}

	limiter := NewUserRateLimiter(config)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Anonymous users should be rate limited as "anonymous" with basic tier
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429 for anonymous user, got %d", rec.Code)
	}
}

// TestUserRateLimiter_Middleware_DifferentUsers tests different users have independent limits.
func TestUserRateLimiter_Middleware_DifferentUsers(t *testing.T) {
	users := []string{"user1@example.com", "user2@example.com", "user3@example.com"}
	store := newMockRateLimitStore()
	algorithm := &mockRateLimitAlgorithm{}
	metrics := newMockRateLimitMetrics()

	for _, user := range users {
		currentUser := user

		config := UserRateLimiterConfig{
			Store:     store,
			Algorithm: algorithm,
			Metrics:   metrics,
			CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
				LimiterType: "user",
			}),
			UserExtractor: &mockUserExtractor{
				userID: currentUser,
				tier:   ratelimit.TierBasic,
				ok:     true,
			},
			TierLimits: map[ratelimit.UserTier]TierLimit{
				ratelimit.TierBasic: {
					Limit:  2,
					Window: 1 * time.Minute,
				},
			},
		}

		limiter := NewUserRateLimiter(config)

		handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// Each user should be able to make 2 requests
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("User %s request %d: expected 200, got %d", user, i+1, rec.Code)
			}
		}

		// 3rd request should be rate limited
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("User %s 3rd request: expected 429, got %d", user, rec.Code)
		}
	}
}

// TestUserRateLimiter_Middleware_NilDecision tests handling of nil decision from algorithm.
func TestUserRateLimiter_Middleware_NilDecision(t *testing.T) {
	config := UserRateLimiterConfig{
		Store: newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{
			decision: nil, // Will return nil decision
		},
		Metrics: newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{
			userID: "user@example.com",
			tier:   ratelimit.TierBasic,
			ok:     true,
		},
		TierLimits: map[ratelimit.UserTier]TierLimit{
			ratelimit.TierBasic: {
				Limit:  5,
				Window: 1 * time.Minute,
			},
		},
	}

	limiter := NewUserRateLimiter(config)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should fail-open when decision is nil
	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 (fail-open), got %d", rec.Code)
	}
}

// BenchmarkUserRateLimiter_Middleware benchmarks the user rate limiter middleware.
func BenchmarkUserRateLimiter_Middleware(b *testing.B) {
	config := UserRateLimiterConfig{
		Store:     newMockRateLimitStore(),
		Algorithm: &mockRateLimitAlgorithm{},
		Metrics:   newMockRateLimitMetrics(),
		CircuitBreaker: ratelimit.NewCircuitBreaker(ratelimit.CircuitBreakerConfig{
			LimiterType: "user",
		}),
		UserExtractor: &mockUserExtractor{
			userID: "user@example.com",
			tier:   ratelimit.TierBasic,
			ok:     true,
		},
		TierLimits: NewDefaultTierLimits(),
	}

	limiter := NewUserRateLimiter(config)

	handler := limiter.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// BenchmarkHashUserID benchmarks user ID hashing.
func BenchmarkHashUserID(b *testing.B) {
	userID := "user@example.com"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hashUserID(userID)
	}
}
