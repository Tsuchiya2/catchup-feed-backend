package summarizer_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"catchup-feed/internal/infra/summarizer"
)

func TestLoadOptions(t *testing.T) {
	tests := []struct {
		name          string
		charLimit     string
		timeout       string
		wantCharLimit int
		wantTimeout   time.Duration
	}{
		{"defaults", "", "", 900, 60 * time.Second},
		{"valid char limit", "500", "", 500, 60 * time.Second},
		{"char limit at min", "100", "", 100, 60 * time.Second},
		{"char limit at max", "5000", "", 5000, 60 * time.Second},
		{"char limit below min falls back", "50", "", 900, 60 * time.Second},
		{"char limit above max falls back", "6000", "", 900, 60 * time.Second},
		{"non-numeric char limit falls back", "abc", "", 900, 60 * time.Second},
		{"valid timeout", "", "30s", 900, 30 * time.Second},
		{"invalid timeout falls back", "", "not-a-duration", 900, 60 * time.Second},
		{"negative timeout falls back", "", "-5s", 900, 60 * time.Second},
		{"both overridden", "1200", "2m", 1200, 2 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SUMMARIZER_CHAR_LIMIT", tt.charLimit)
			t.Setenv("SUMMARIZER_TIMEOUT", tt.timeout)

			opts := summarizer.LoadOptions()

			assert.Equal(t, tt.wantCharLimit, opts.CharacterLimit)
			assert.Equal(t, tt.wantTimeout, opts.Timeout)
		})
	}
}

func TestValidateCharacterLimit(t *testing.T) {
	tests := []struct {
		name    string
		limit   int
		wantErr bool
	}{
		{"minimum boundary", 100, false},
		{"maximum boundary", 5000, false},
		{"typical value", 900, false},
		{"below minimum", 99, true},
		{"above maximum", 5001, true},
		{"zero", 0, true},
		{"negative", -1, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := summarizer.ValidateCharacterLimit(tt.limit)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
