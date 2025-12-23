package ratelimit

import (
	"testing"
	"time"
)

func TestNewAllowedDecision(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		limiterType   string
		limit         int
		remaining     int
		resetAt       time.Time
		wantAllowed   bool
		wantRemaining int
	}{
		{
			name:          "allowed decision with positive remaining",
			key:           "user-123",
			limiterType:   "user",
			limit:         100,
			remaining:     75,
			resetAt:       time.Now().Add(1 * time.Minute),
			wantAllowed:   true,
			wantRemaining: 75,
		},
		{
			name:          "allowed decision with zero remaining",
			key:           "192.168.1.1",
			limiterType:   "ip",
			limit:         10,
			remaining:     0,
			resetAt:       time.Now().Add(30 * time.Second),
			wantAllowed:   true,
			wantRemaining: 0,
		},
		{
			name:          "allowed decision with past reset time",
			key:           "user-456",
			limiterType:   "user",
			limit:         50,
			remaining:     25,
			resetAt:       time.Now().Add(-5 * time.Minute),
			wantAllowed:   true,
			wantRemaining: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := NewAllowedDecision(tt.key, tt.limiterType, tt.limit, tt.remaining, tt.resetAt)

			if decision.Key != tt.key {
				t.Errorf("Key = %v, want %v", decision.Key, tt.key)
			}
			if decision.Allowed != tt.wantAllowed {
				t.Errorf("Allowed = %v, want %v", decision.Allowed, tt.wantAllowed)
			}
			if decision.Limit != tt.limit {
				t.Errorf("Limit = %v, want %v", decision.Limit, tt.limit)
			}
			if decision.Remaining != tt.wantRemaining {
				t.Errorf("Remaining = %v, want %v", decision.Remaining, tt.wantRemaining)
			}
			if decision.LimiterType != tt.limiterType {
				t.Errorf("LimiterType = %v, want %v", decision.LimiterType, tt.limiterType)
			}
			if !decision.ResetAt.Equal(tt.resetAt) {
				t.Errorf("ResetAt = %v, want %v", decision.ResetAt, tt.resetAt)
			}
			// RetryAfter should be non-negative
			if decision.RetryAfter < 0 {
				t.Errorf("RetryAfter = %v, should be non-negative", decision.RetryAfter)
			}
		})
	}
}

func TestNewDeniedDecision(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		limiterType string
		limit       int
		resetAt     time.Time
		wantAllowed bool
	}{
		{
			name:        "denied decision with future reset time",
			key:         "user-789",
			limiterType: "user",
			limit:       100,
			resetAt:     time.Now().Add(2 * time.Minute),
			wantAllowed: false,
		},
		{
			name:        "denied decision with past reset time",
			key:         "192.168.1.2",
			limiterType: "ip",
			limit:       10,
			resetAt:     time.Now().Add(-1 * time.Minute),
			wantAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := NewDeniedDecision(tt.key, tt.limiterType, tt.limit, tt.resetAt)

			if decision.Key != tt.key {
				t.Errorf("Key = %v, want %v", decision.Key, tt.key)
			}
			if decision.Allowed != tt.wantAllowed {
				t.Errorf("Allowed = %v, want %v", decision.Allowed, tt.wantAllowed)
			}
			if decision.Limit != tt.limit {
				t.Errorf("Limit = %v, want %v", decision.Limit, tt.limit)
			}
			if decision.Remaining != 0 {
				t.Errorf("Remaining = %v, want 0", decision.Remaining)
			}
			if decision.LimiterType != tt.limiterType {
				t.Errorf("LimiterType = %v, want %v", decision.LimiterType, tt.limiterType)
			}
			// RetryAfter should be non-negative
			if decision.RetryAfter < 0 {
				t.Errorf("RetryAfter = %v, should be non-negative", decision.RetryAfter)
			}
		})
	}
}

func TestRateLimitDecision_IsAllowed(t *testing.T) {
	allowed := &RateLimitDecision{Allowed: true}
	denied := &RateLimitDecision{Allowed: false}

	if !allowed.IsAllowed() {
		t.Error("IsAllowed() should return true for allowed decision")
	}
	if denied.IsAllowed() {
		t.Error("IsAllowed() should return false for denied decision")
	}
}

func TestRateLimitDecision_IsDenied(t *testing.T) {
	allowed := &RateLimitDecision{Allowed: true}
	denied := &RateLimitDecision{Allowed: false}

	if allowed.IsDenied() {
		t.Error("IsDenied() should return false for allowed decision")
	}
	if !denied.IsDenied() {
		t.Error("IsDenied() should return true for denied decision")
	}
}

func TestRateLimitDecision_HasRemaining(t *testing.T) {
	tests := []struct {
		name      string
		remaining int
		want      bool
	}{
		{"positive remaining", 10, true},
		{"zero remaining", 0, false},
		{"negative remaining", -5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := &RateLimitDecision{Remaining: tt.remaining}
			if got := decision.HasRemaining(); got != tt.want {
				t.Errorf("HasRemaining() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRateLimitDecision_ResetAtUnix(t *testing.T) {
	now := time.Now()
	decision := &RateLimitDecision{
		ResetAt: now,
	}

	got := decision.ResetAtUnix()
	want := now.Unix()

	if got != want {
		t.Errorf("ResetAtUnix() = %v, want %v", got, want)
	}
}

func TestRateLimitDecision_RetryAfterSeconds(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter time.Duration
		wantMin    int64
	}{
		{"positive duration", 30 * time.Second, 30},
		{"zero duration", 0, 0},
		{"negative duration should return zero", -10 * time.Second, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := &RateLimitDecision{
				RetryAfter: tt.retryAfter,
			}

			got := decision.RetryAfterSeconds()
			if got < tt.wantMin {
				t.Errorf("RetryAfterSeconds() = %v, want >= %v", got, tt.wantMin)
			}
			// Negative durations should return 0
			if tt.retryAfter < 0 && got != 0 {
				t.Errorf("RetryAfterSeconds() = %v, want 0 for negative duration", got)
			}
		})
	}
}

func TestRateLimitDecision_String(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		decision *RateLimitDecision
		contains []string
	}{
		{
			name: "allowed decision string",
			decision: &RateLimitDecision{
				Key:         "user-123",
				Allowed:     true,
				Limit:       100,
				Remaining:   75,
				ResetAt:     now,
				LimiterType: "user",
			},
			contains: []string{"Allowed: true", "user-123", "user", "75", "100"},
		},
		{
			name: "denied decision string",
			decision: &RateLimitDecision{
				Key:         "192.168.1.1",
				Allowed:     false,
				Limit:       10,
				Remaining:   0,
				ResetAt:     now,
				RetryAfter:  30 * time.Second,
				LimiterType: "ip",
			},
			contains: []string{"Allowed: false", "192.168.1.1", "ip", "10"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.decision.String()
			for _, substr := range tt.contains {
				if !contains(got, substr) {
					t.Errorf("String() = %v, should contain %q", got, substr)
				}
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
