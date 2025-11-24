package fetcher_test

import (
	"os"
	"testing"
	"time"

	"catchup-feed/internal/infra/fetcher"
)

// ───────────────────────────────────────────────────────────────
// TASK-015: Configuration Unit Tests
// ───────────────────────────────────────────────────────────────

func TestDefaultConfig(t *testing.T) {
	cfg := fetcher.DefaultConfig()

	// Verify all default values
	if !cfg.Enabled {
		t.Error("expected Enabled=true by default")
	}

	if cfg.Threshold != 1500 {
		t.Errorf("expected Threshold=1500, got %d", cfg.Threshold)
	}

	if cfg.Timeout != 10*time.Second {
		t.Errorf("expected Timeout=10s, got %v", cfg.Timeout)
	}

	if cfg.Parallelism != 10 {
		t.Errorf("expected Parallelism=10, got %d", cfg.Parallelism)
	}

	if cfg.MaxBodySize != 10*1024*1024 {
		t.Errorf("expected MaxBodySize=10MB, got %d", cfg.MaxBodySize)
	}

	if cfg.MaxRedirects != 5 {
		t.Errorf("expected MaxRedirects=5, got %d", cfg.MaxRedirects)
	}

	if !cfg.DenyPrivateIPs {
		t.Error("expected DenyPrivateIPs=true by default (security)")
	}

	// Verify default config is valid
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config should be valid, got error: %v", err)
	}
}

func TestConfigValidate_Valid(t *testing.T) {
	cfg := fetcher.ContentFetchConfig{
		Enabled:        true,
		Threshold:      2000,
		Timeout:        15 * time.Second,
		Parallelism:    20,
		MaxBodySize:    20 * 1024 * 1024,
		MaxRedirects:   3,
		DenyPrivateIPs: true,
	}

	err := cfg.Validate()
	if err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestConfigValidate_InvalidThreshold(t *testing.T) {
	cfg := fetcher.DefaultConfig()
	cfg.Threshold = -1

	err := cfg.Validate()
	if err == nil {
		t.Error("expected validation error for negative threshold")
	}
	if err.Error() != "threshold must be non-negative, got -1" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestConfigValidate_InvalidTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{
			name:    "zero timeout",
			timeout: 0,
		},
		{
			name:    "negative timeout",
			timeout: -1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := fetcher.DefaultConfig()
			cfg.Timeout = tt.timeout

			err := cfg.Validate()
			if err == nil {
				t.Errorf("expected validation error for timeout=%v", tt.timeout)
			}
		})
	}
}

func TestConfigValidate_InvalidParallelism(t *testing.T) {
	tests := []struct {
		name        string
		parallelism int
		shouldFail  bool
	}{
		{
			name:        "zero parallelism",
			parallelism: 0,
			shouldFail:  true,
		},
		{
			name:        "negative parallelism",
			parallelism: -1,
			shouldFail:  true,
		},
		{
			name:        "parallelism too high",
			parallelism: 51,
			shouldFail:  true,
		},
		{
			name:        "parallelism at max boundary",
			parallelism: 50,
			shouldFail:  false,
		},
		{
			name:        "parallelism at min boundary",
			parallelism: 1,
			shouldFail:  false,
		},
		{
			name:        "parallelism way too high",
			parallelism: 100,
			shouldFail:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := fetcher.DefaultConfig()
			cfg.Parallelism = tt.parallelism

			err := cfg.Validate()
			if tt.shouldFail {
				if err == nil {
					t.Errorf("expected validation error for parallelism=%d", tt.parallelism)
				}
			} else {
				if err != nil {
					t.Errorf("expected valid config for parallelism=%d, got error: %v", tt.parallelism, err)
				}
			}
		})
	}
}

func TestConfigValidate_InvalidMaxBodySize(t *testing.T) {
	tests := []struct {
		name        string
		maxBodySize int64
		shouldFail  bool
	}{
		{
			name:        "zero size",
			maxBodySize: 0,
			shouldFail:  true,
		},
		{
			name:        "below minimum (1KB)",
			maxBodySize: 500,
			shouldFail:  true,
		},
		{
			name:        "at minimum boundary (1KB)",
			maxBodySize: 1024,
			shouldFail:  false,
		},
		{
			name:        "at maximum boundary (100MB)",
			maxBodySize: 100 * 1024 * 1024,
			shouldFail:  false,
		},
		{
			name:        "above maximum (200MB)",
			maxBodySize: 200 * 1024 * 1024,
			shouldFail:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := fetcher.DefaultConfig()
			cfg.MaxBodySize = tt.maxBodySize

			err := cfg.Validate()
			if tt.shouldFail {
				if err == nil {
					t.Errorf("expected validation error for MaxBodySize=%d", tt.maxBodySize)
				}
			} else {
				if err != nil {
					t.Errorf("expected valid config for MaxBodySize=%d, got error: %v", tt.maxBodySize, err)
				}
			}
		})
	}
}

func TestConfigValidate_InvalidMaxRedirects(t *testing.T) {
	tests := []struct {
		name         string
		maxRedirects int
		shouldFail   bool
	}{
		{
			name:         "negative redirects",
			maxRedirects: -1,
			shouldFail:   true,
		},
		{
			name:         "at minimum boundary (0)",
			maxRedirects: 0,
			shouldFail:   false,
		},
		{
			name:         "at maximum boundary (10)",
			maxRedirects: 10,
			shouldFail:   false,
		},
		{
			name:         "above maximum (11)",
			maxRedirects: 11,
			shouldFail:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := fetcher.DefaultConfig()
			cfg.MaxRedirects = tt.maxRedirects

			err := cfg.Validate()
			if tt.shouldFail {
				if err == nil {
					t.Errorf("expected validation error for MaxRedirects=%d", tt.maxRedirects)
				}
			} else {
				if err != nil {
					t.Errorf("expected valid config for MaxRedirects=%d, got error: %v", tt.maxRedirects, err)
				}
			}
		})
	}
}

func TestLoadConfigFromEnv_Defaults(t *testing.T) {
	// Clear all environment variables
	envVars := []string{
		"CONTENT_FETCH_ENABLED",
		"CONTENT_FETCH_THRESHOLD",
		"CONTENT_FETCH_TIMEOUT",
		"CONTENT_FETCH_PARALLELISM",
		"CONTENT_FETCH_MAX_BODY_SIZE",
		"CONTENT_FETCH_MAX_REDIRECTS",
		"CONTENT_FETCH_DENY_PRIVATE_IPS",
	}

	for _, envVar := range envVars {
		_ = os.Unsetenv(envVar)
	}

	cfg, err := fetcher.LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}

	// Should match default config
	defaultCfg := fetcher.DefaultConfig()

	if cfg.Enabled != defaultCfg.Enabled {
		t.Errorf("expected Enabled=%v, got %v", defaultCfg.Enabled, cfg.Enabled)
	}

	if cfg.Threshold != defaultCfg.Threshold {
		t.Errorf("expected Threshold=%d, got %d", defaultCfg.Threshold, cfg.Threshold)
	}

	if cfg.Timeout != defaultCfg.Timeout {
		t.Errorf("expected Timeout=%v, got %v", defaultCfg.Timeout, cfg.Timeout)
	}

	if cfg.Parallelism != defaultCfg.Parallelism {
		t.Errorf("expected Parallelism=%d, got %d", defaultCfg.Parallelism, cfg.Parallelism)
	}

	if cfg.MaxBodySize != defaultCfg.MaxBodySize {
		t.Errorf("expected MaxBodySize=%d, got %d", defaultCfg.MaxBodySize, cfg.MaxBodySize)
	}

	if cfg.MaxRedirects != defaultCfg.MaxRedirects {
		t.Errorf("expected MaxRedirects=%d, got %d", defaultCfg.MaxRedirects, cfg.MaxRedirects)
	}

	if cfg.DenyPrivateIPs != defaultCfg.DenyPrivateIPs {
		t.Errorf("expected DenyPrivateIPs=%v, got %v", defaultCfg.DenyPrivateIPs, cfg.DenyPrivateIPs)
	}
}

func TestLoadConfigFromEnv_CustomValues(t *testing.T) {
	// Set custom environment variables
	_ = os.Setenv("CONTENT_FETCH_ENABLED", "false")
	_ = os.Setenv("CONTENT_FETCH_THRESHOLD", "2000")
	_ = os.Setenv("CONTENT_FETCH_TIMEOUT", "20s")
	_ = os.Setenv("CONTENT_FETCH_PARALLELISM", "15")
	_ = os.Setenv("CONTENT_FETCH_MAX_BODY_SIZE", "20971520") // 20MB
	_ = os.Setenv("CONTENT_FETCH_MAX_REDIRECTS", "3")
	_ = os.Setenv("CONTENT_FETCH_DENY_PRIVATE_IPS", "false")

	defer func() {
		// Clean up
		_ = os.Unsetenv("CONTENT_FETCH_ENABLED")
		_ = os.Unsetenv("CONTENT_FETCH_THRESHOLD")
		_ = os.Unsetenv("CONTENT_FETCH_TIMEOUT")
		_ = os.Unsetenv("CONTENT_FETCH_PARALLELISM")
		_ = os.Unsetenv("CONTENT_FETCH_MAX_BODY_SIZE")
		_ = os.Unsetenv("CONTENT_FETCH_MAX_REDIRECTS")
		_ = os.Unsetenv("CONTENT_FETCH_DENY_PRIVATE_IPS")
	}()

	cfg, err := fetcher.LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}

	// Verify custom values
	if cfg.Enabled != false {
		t.Errorf("expected Enabled=false, got %v", cfg.Enabled)
	}

	if cfg.Threshold != 2000 {
		t.Errorf("expected Threshold=2000, got %d", cfg.Threshold)
	}

	if cfg.Timeout != 20*time.Second {
		t.Errorf("expected Timeout=20s, got %v", cfg.Timeout)
	}

	if cfg.Parallelism != 15 {
		t.Errorf("expected Parallelism=15, got %d", cfg.Parallelism)
	}

	if cfg.MaxBodySize != 20971520 {
		t.Errorf("expected MaxBodySize=20971520, got %d", cfg.MaxBodySize)
	}

	if cfg.MaxRedirects != 3 {
		t.Errorf("expected MaxRedirects=3, got %d", cfg.MaxRedirects)
	}

	if cfg.DenyPrivateIPs != false {
		t.Errorf("expected DenyPrivateIPs=false, got %v", cfg.DenyPrivateIPs)
	}
}

func TestLoadConfigFromEnv_InvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
		value  string
	}{
		{
			name:   "invalid threshold (not a number)",
			envVar: "CONTENT_FETCH_THRESHOLD",
			value:  "abc",
		},
		{
			name:   "invalid timeout (wrong format)",
			envVar: "CONTENT_FETCH_TIMEOUT",
			value:  "10",
		},
		{
			name:   "invalid parallelism (not a number)",
			envVar: "CONTENT_FETCH_PARALLELISM",
			value:  "many",
		},
		{
			name:   "invalid max body size (not a number)",
			envVar: "CONTENT_FETCH_MAX_BODY_SIZE",
			value:  "huge",
		},
		{
			name:   "invalid max redirects (not a number)",
			envVar: "CONTENT_FETCH_MAX_REDIRECTS",
			value:  "few",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Setenv(tt.envVar, tt.value)
			defer func() { _ = os.Unsetenv(tt.envVar) }()

			_, err := fetcher.LoadConfigFromEnv()
			if err == nil {
				t.Errorf("expected error for invalid %s=%q, got nil", tt.envVar, tt.value)
			}
		})
	}
}

func TestLoadConfigFromEnv_InvalidValidation(t *testing.T) {
	// Set value that parses correctly but fails validation
	_ = os.Setenv("CONTENT_FETCH_THRESHOLD", "-100")
	defer func() { _ = os.Unsetenv("CONTENT_FETCH_THRESHOLD") }()

	_, err := fetcher.LoadConfigFromEnv()
	if err == nil {
		t.Error("expected validation error for negative threshold, got nil")
	}
}

func TestConfigValidate_ZeroThreshold(t *testing.T) {
	// Zero threshold is valid (means always fetch)
	cfg := fetcher.DefaultConfig()
	cfg.Threshold = 0

	err := cfg.Validate()
	if err != nil {
		t.Errorf("expected valid config for Threshold=0, got error: %v", err)
	}
}

func TestLoadConfigFromEnv_PartialCustom(t *testing.T) {
	// Set only some environment variables, others should use defaults
	_ = os.Setenv("CONTENT_FETCH_THRESHOLD", "3000")
	_ = os.Setenv("CONTENT_FETCH_PARALLELISM", "20")
	defer func() {
		_ = os.Unsetenv("CONTENT_FETCH_THRESHOLD")
		_ = os.Unsetenv("CONTENT_FETCH_PARALLELISM")
	}()

	cfg, err := fetcher.LoadConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadConfigFromEnv() error = %v", err)
	}

	// Verify custom values
	if cfg.Threshold != 3000 {
		t.Errorf("expected Threshold=3000, got %d", cfg.Threshold)
	}

	if cfg.Parallelism != 20 {
		t.Errorf("expected Parallelism=20, got %d", cfg.Parallelism)
	}

	// Verify defaults for unset values
	defaultCfg := fetcher.DefaultConfig()
	if cfg.Timeout != defaultCfg.Timeout {
		t.Errorf("expected Timeout=%v (default), got %v", defaultCfg.Timeout, cfg.Timeout)
	}

	if cfg.MaxBodySize != defaultCfg.MaxBodySize {
		t.Errorf("expected MaxBodySize=%d (default), got %d", defaultCfg.MaxBodySize, cfg.MaxBodySize)
	}
}
