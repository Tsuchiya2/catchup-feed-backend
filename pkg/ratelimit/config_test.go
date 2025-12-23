package ratelimit

import (
	"testing"
	"time"
)

func TestUserTier_String(t *testing.T) {
	tests := []struct {
		name string
		tier UserTier
		want string
	}{
		{"admin tier", TierAdmin, "admin"},
		{"premium tier", TierPremium, "premium"},
		{"basic tier", TierBasic, "basic"},
		{"viewer tier", TierViewer, "viewer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tier.String(); got != tt.want {
				t.Errorf("String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUserTier_IsValid(t *testing.T) {
	tests := []struct {
		name string
		tier UserTier
		want bool
	}{
		{"admin is valid", TierAdmin, true},
		{"premium is valid", TierPremium, true},
		{"basic is valid", TierBasic, true},
		{"viewer is valid", TierViewer, true},
		{"empty string is invalid", UserTier(""), false},
		{"unknown tier is invalid", UserTier("unknown"), false},
		{"uppercase is invalid", UserTier("ADMIN"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tier.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRateLimitConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *RateLimitConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &RateLimitConfig{
				DefaultIPLimit:                 100,
				DefaultIPWindow:                1 * time.Minute,
				DefaultUserLimit:               1000,
				DefaultUserWindow:              1 * time.Hour,
				MaxActiveKeys:                  10000,
				CleanupInterval:                5 * time.Minute,
				CleanupMaxAge:                  1 * time.Hour,
				CircuitBreakerFailureThreshold: 10,
				CircuitBreakerResetTimeout:     30 * time.Second,
				Enabled:                        true,
			},
			wantErr: false,
		},
		{
			name: "negative IP limit",
			config: &RateLimitConfig{
				DefaultIPLimit: -1,
			},
			wantErr: true,
		},
		{
			name: "negative IP window",
			config: &RateLimitConfig{
				DefaultIPLimit:  100,
				DefaultIPWindow: -1 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "negative user limit",
			config: &RateLimitConfig{
				DefaultIPLimit:   100,
				DefaultIPWindow:  1 * time.Minute,
				DefaultUserLimit: -1,
			},
			wantErr: true,
		},
		{
			name: "negative user window",
			config: &RateLimitConfig{
				DefaultIPLimit:    100,
				DefaultIPWindow:   1 * time.Minute,
				DefaultUserLimit:  1000,
				DefaultUserWindow: -1 * time.Hour,
			},
			wantErr: true,
		},
		{
			name: "negative max active keys",
			config: &RateLimitConfig{
				DefaultIPLimit:    100,
				DefaultIPWindow:   1 * time.Minute,
				DefaultUserLimit:  1000,
				DefaultUserWindow: 1 * time.Hour,
				MaxActiveKeys:     -1,
			},
			wantErr: true,
		},
		{
			name: "negative cleanup interval",
			config: &RateLimitConfig{
				DefaultIPLimit:    100,
				DefaultIPWindow:   1 * time.Minute,
				DefaultUserLimit:  1000,
				DefaultUserWindow: 1 * time.Hour,
				MaxActiveKeys:     10000,
				CleanupInterval:   -1 * time.Minute,
			},
			wantErr: true,
		},
		{
			name: "negative cleanup max age",
			config: &RateLimitConfig{
				DefaultIPLimit:    100,
				DefaultIPWindow:   1 * time.Minute,
				DefaultUserLimit:  1000,
				DefaultUserWindow: 1 * time.Hour,
				MaxActiveKeys:     10000,
				CleanupInterval:   5 * time.Minute,
				CleanupMaxAge:     -1 * time.Hour,
			},
			wantErr: true,
		},
		{
			name: "negative circuit breaker failure threshold",
			config: &RateLimitConfig{
				DefaultIPLimit:                 100,
				DefaultIPWindow:                1 * time.Minute,
				DefaultUserLimit:               1000,
				DefaultUserWindow:              1 * time.Hour,
				MaxActiveKeys:                  10000,
				CleanupInterval:                5 * time.Minute,
				CleanupMaxAge:                  1 * time.Hour,
				CircuitBreakerFailureThreshold: -1,
			},
			wantErr: true,
		},
		{
			name: "negative circuit breaker reset timeout",
			config: &RateLimitConfig{
				DefaultIPLimit:                 100,
				DefaultIPWindow:                1 * time.Minute,
				DefaultUserLimit:               1000,
				DefaultUserWindow:              1 * time.Hour,
				MaxActiveKeys:                  10000,
				CleanupInterval:                5 * time.Minute,
				CleanupMaxAge:                  1 * time.Hour,
				CircuitBreakerFailureThreshold: 10,
				CircuitBreakerResetTimeout:     -1 * time.Second,
			},
			wantErr: true,
		},
		{
			name: "endpoint override with empty path pattern",
			config: &RateLimitConfig{
				DefaultIPLimit:    100,
				DefaultIPWindow:   1 * time.Minute,
				DefaultUserLimit:  1000,
				DefaultUserWindow: 1 * time.Hour,
				EndpointOverrides: []EndpointRateLimitConfig{
					{
						PathPattern: "",
						IPLimit:     10,
						IPWindow:    1 * time.Minute,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "endpoint override with negative IP limit",
			config: &RateLimitConfig{
				DefaultIPLimit:    100,
				DefaultIPWindow:   1 * time.Minute,
				DefaultUserLimit:  1000,
				DefaultUserWindow: 1 * time.Hour,
				EndpointOverrides: []EndpointRateLimitConfig{
					{
						PathPattern: "/api/auth",
						IPLimit:     -1,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "tier limit with invalid tier",
			config: &RateLimitConfig{
				DefaultIPLimit:    100,
				DefaultIPWindow:   1 * time.Minute,
				DefaultUserLimit:  1000,
				DefaultUserWindow: 1 * time.Hour,
				TierLimits: []TierRateLimitConfig{
					{
						Tier:   UserTier("invalid"),
						Limit:  100,
						Window: 1 * time.Minute,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "tier limit with negative limit",
			config: &RateLimitConfig{
				DefaultIPLimit:    100,
				DefaultIPWindow:   1 * time.Minute,
				DefaultUserLimit:  1000,
				DefaultUserWindow: 1 * time.Hour,
				TierLimits: []TierRateLimitConfig{
					{
						Tier:   TierAdmin,
						Limit:  -1,
						Window: 1 * time.Minute,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "zero values should pass validation",
			config: &RateLimitConfig{
				DefaultIPLimit:    0,
				DefaultIPWindow:   0,
				DefaultUserLimit:  0,
				DefaultUserWindow: 0,
				MaxActiveKeys:     0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRateLimitConfig_ApplyDefaults(t *testing.T) {
	config := &RateLimitConfig{}
	config.ApplyDefaults()

	// Check that defaults are applied
	if config.DefaultIPLimit == 0 {
		t.Error("DefaultIPLimit should have a default value")
	}
	if config.DefaultIPWindow == 0 {
		t.Error("DefaultIPWindow should have a default value")
	}
	if config.DefaultUserLimit == 0 {
		t.Error("DefaultUserLimit should have a default value")
	}
	if config.DefaultUserWindow == 0 {
		t.Error("DefaultUserWindow should have a default value")
	}
	if config.MaxActiveKeys == 0 {
		t.Error("MaxActiveKeys should have a default value")
	}
	if config.CleanupInterval == 0 {
		t.Error("CleanupInterval should have a default value")
	}
	if config.CleanupMaxAge == 0 {
		t.Error("CleanupMaxAge should have a default value")
	}
	if config.CircuitBreakerFailureThreshold == 0 {
		t.Error("CircuitBreakerFailureThreshold should have a default value")
	}
	if config.CircuitBreakerResetTimeout == 0 {
		t.Error("CircuitBreakerResetTimeout should have a default value")
	}
	if !config.Enabled {
		t.Error("Enabled should be true by default")
	}

	// Check specific values
	expectedIPLimit := 100
	if config.DefaultIPLimit != expectedIPLimit {
		t.Errorf("DefaultIPLimit = %v, want %v", config.DefaultIPLimit, expectedIPLimit)
	}

	expectedUserLimit := 1000
	if config.DefaultUserLimit != expectedUserLimit {
		t.Errorf("DefaultUserLimit = %v, want %v", config.DefaultUserLimit, expectedUserLimit)
	}
}

func TestRateLimitConfig_GetTierLimit(t *testing.T) {
	config := &RateLimitConfig{
		DefaultUserLimit:  1000,
		DefaultUserWindow: 1 * time.Hour,
		TierLimits: []TierRateLimitConfig{
			{
				Tier:   TierAdmin,
				Limit:  10000,
				Window: 1 * time.Hour,
			},
			{
				Tier:   TierPremium,
				Limit:  5000,
				Window: 1 * time.Hour,
			},
		},
	}

	tests := []struct {
		name       string
		tier       UserTier
		wantLimit  int
		wantWindow time.Duration
	}{
		{
			name:       "admin tier returns custom limit",
			tier:       TierAdmin,
			wantLimit:  10000,
			wantWindow: 1 * time.Hour,
		},
		{
			name:       "premium tier returns custom limit",
			tier:       TierPremium,
			wantLimit:  5000,
			wantWindow: 1 * time.Hour,
		},
		{
			name:       "basic tier returns default limit",
			tier:       TierBasic,
			wantLimit:  1000,
			wantWindow: 1 * time.Hour,
		},
		{
			name:       "viewer tier returns default limit",
			tier:       TierViewer,
			wantLimit:  1000,
			wantWindow: 1 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotLimit, gotWindow := config.GetTierLimit(tt.tier)
			if gotLimit != tt.wantLimit {
				t.Errorf("GetTierLimit() limit = %v, want %v", gotLimit, tt.wantLimit)
			}
			if gotWindow != tt.wantWindow {
				t.Errorf("GetTierLimit() window = %v, want %v", gotWindow, tt.wantWindow)
			}
		})
	}
}

func TestRateLimitConfig_GetEndpointLimit(t *testing.T) {
	config := &RateLimitConfig{
		DefaultIPLimit:    100,
		DefaultIPWindow:   1 * time.Minute,
		DefaultUserLimit:  1000,
		DefaultUserWindow: 1 * time.Hour,
		EndpointOverrides: []EndpointRateLimitConfig{
			{
				PathPattern: "/api/auth",
				IPLimit:     10,
				IPWindow:    1 * time.Minute,
				UserLimit:   50,
				UserWindow:  1 * time.Hour,
			},
			{
				PathPattern: "/api/upload",
				IPLimit:     5,
				IPWindow:    1 * time.Minute,
				UserLimit:   20,
				UserWindow:  1 * time.Hour,
			},
		},
	}

	tests := []struct {
		name           string
		pathPattern    string
		wantIPLimit    int
		wantIPWindow   time.Duration
		wantUserLimit  int
		wantUserWindow time.Duration
	}{
		{
			name:           "auth endpoint returns custom limits",
			pathPattern:    "/api/auth",
			wantIPLimit:    10,
			wantIPWindow:   1 * time.Minute,
			wantUserLimit:  50,
			wantUserWindow: 1 * time.Hour,
		},
		{
			name:           "upload endpoint returns custom limits",
			pathPattern:    "/api/upload",
			wantIPLimit:    5,
			wantIPWindow:   1 * time.Minute,
			wantUserLimit:  20,
			wantUserWindow: 1 * time.Hour,
		},
		{
			name:           "unknown endpoint returns defaults",
			pathPattern:    "/api/articles",
			wantIPLimit:    100,
			wantIPWindow:   1 * time.Minute,
			wantUserLimit:  1000,
			wantUserWindow: 1 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIPLimit, gotIPWindow, gotUserLimit, gotUserWindow := config.GetEndpointLimit(tt.pathPattern)

			if gotIPLimit != tt.wantIPLimit {
				t.Errorf("GetEndpointLimit() IPLimit = %v, want %v", gotIPLimit, tt.wantIPLimit)
			}
			if gotIPWindow != tt.wantIPWindow {
				t.Errorf("GetEndpointLimit() IPWindow = %v, want %v", gotIPWindow, tt.wantIPWindow)
			}
			if gotUserLimit != tt.wantUserLimit {
				t.Errorf("GetEndpointLimit() UserLimit = %v, want %v", gotUserLimit, tt.wantUserLimit)
			}
			if gotUserWindow != tt.wantUserWindow {
				t.Errorf("GetEndpointLimit() UserWindow = %v, want %v", gotUserWindow, tt.wantUserWindow)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// Verify that all defaults are applied
	if config.DefaultIPLimit == 0 {
		t.Error("DefaultConfig() should set DefaultIPLimit")
	}
	if config.DefaultUserLimit == 0 {
		t.Error("DefaultConfig() should set DefaultUserLimit")
	}
	if !config.Enabled {
		t.Error("DefaultConfig() should enable rate limiting")
	}

	// Verify that the config is valid
	if err := config.Validate(); err != nil {
		t.Errorf("DefaultConfig() should return valid config, got error: %v", err)
	}
}
