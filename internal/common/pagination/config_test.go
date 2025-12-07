package pagination_test

import (
	"testing"

	"catchup-feed/internal/common/pagination"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	config := pagination.DefaultConfig()

	if config.DefaultPage != 1 {
		t.Errorf("DefaultConfig() DefaultPage = %d, want 1", config.DefaultPage)
	}
	if config.DefaultLimit != 20 {
		t.Errorf("DefaultConfig() DefaultLimit = %d, want 20", config.DefaultLimit)
	}
	if config.MaxLimit != 100 {
		t.Errorf("DefaultConfig() MaxLimit = %d, want 100", config.MaxLimit)
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Note: This test modifies environment variables
	// We'll test each scenario independently

	t.Run("with all env vars set", func(t *testing.T) {
		// Set environment variables (t.Setenv handles cleanup automatically)
		t.Setenv("PAGINATION_DEFAULT_PAGE", "2")
		t.Setenv("PAGINATION_DEFAULT_LIMIT", "30")
		t.Setenv("PAGINATION_MAX_LIMIT", "200")

		config := pagination.LoadFromEnv()

		if config.DefaultPage != 2 {
			t.Errorf("LoadFromEnv() DefaultPage = %d, want 2", config.DefaultPage)
		}
		if config.DefaultLimit != 30 {
			t.Errorf("LoadFromEnv() DefaultLimit = %d, want 30", config.DefaultLimit)
		}
		if config.MaxLimit != 200 {
			t.Errorf("LoadFromEnv() MaxLimit = %d, want 200", config.MaxLimit)
		}
	})

	t.Run("with no env vars (fallback to defaults)", func(t *testing.T) {
		// Ensure env vars are not set (set to empty then t.Setenv restores)
		t.Setenv("PAGINATION_DEFAULT_PAGE", "")
		t.Setenv("PAGINATION_DEFAULT_LIMIT", "")
		t.Setenv("PAGINATION_MAX_LIMIT", "")

		config := pagination.LoadFromEnv()

		// Should fallback to defaults
		if config.DefaultPage != 1 {
			t.Errorf("LoadFromEnv() DefaultPage = %d, want 1 (default)", config.DefaultPage)
		}
		if config.DefaultLimit != 20 {
			t.Errorf("LoadFromEnv() DefaultLimit = %d, want 20 (default)", config.DefaultLimit)
		}
		if config.MaxLimit != 100 {
			t.Errorf("LoadFromEnv() MaxLimit = %d, want 100 (default)", config.MaxLimit)
		}
	})

	t.Run("with invalid env vars (fallback to defaults)", func(t *testing.T) {
		// Set invalid environment variables (t.Setenv handles cleanup automatically)
		t.Setenv("PAGINATION_DEFAULT_PAGE", "invalid")
		t.Setenv("PAGINATION_DEFAULT_LIMIT", "abc")
		t.Setenv("PAGINATION_MAX_LIMIT", "xyz")

		config := pagination.LoadFromEnv()

		// Should fallback to defaults when parsing fails
		if config.DefaultPage != 1 {
			t.Errorf("LoadFromEnv() DefaultPage = %d, want 1 (default on invalid)", config.DefaultPage)
		}
		if config.DefaultLimit != 20 {
			t.Errorf("LoadFromEnv() DefaultLimit = %d, want 20 (default on invalid)", config.DefaultLimit)
		}
		if config.MaxLimit != 100 {
			t.Errorf("LoadFromEnv() MaxLimit = %d, want 100 (default on invalid)", config.MaxLimit)
		}
	})

	t.Run("with partial env vars", func(t *testing.T) {
		// Set only some environment variables (t.Setenv handles cleanup automatically)
		t.Setenv("PAGINATION_DEFAULT_PAGE", "3")
		t.Setenv("PAGINATION_DEFAULT_LIMIT", "")
		t.Setenv("PAGINATION_MAX_LIMIT", "")

		config := pagination.LoadFromEnv()

		if config.DefaultPage != 3 {
			t.Errorf("LoadFromEnv() DefaultPage = %d, want 3", config.DefaultPage)
		}
		// These should use defaults
		if config.DefaultLimit != 20 {
			t.Errorf("LoadFromEnv() DefaultLimit = %d, want 20 (default)", config.DefaultLimit)
		}
		if config.MaxLimit != 100 {
			t.Errorf("LoadFromEnv() MaxLimit = %d, want 100 (default)", config.MaxLimit)
		}
	})
}
