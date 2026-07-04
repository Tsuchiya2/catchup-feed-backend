package summarizer

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
)

func TestTruncateInput(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantSame     bool // input returned unchanged
		wantMaxBytes int  // upper bound for the kept prefix (before the suffix)
	}{
		{
			name:     "short ascii unchanged",
			input:    strings.Repeat("a", 100),
			wantSame: true,
		},
		{
			name:     "exactly at limit unchanged",
			input:    strings.Repeat("a", maxInputChars),
			wantSame: true,
		},
		{
			name:         "ascii over limit truncated at limit",
			input:        strings.Repeat("a", maxInputChars+1),
			wantMaxBytes: maxInputChars,
		},
		{
			name: "multibyte over limit cut on rune boundary",
			// "あ" is 3 bytes; maxInputChars (10000) is not a multiple of 3,
			// so a naive byte cut would split a rune.
			input:        strings.Repeat("あ", maxInputChars),
			wantMaxBytes: maxInputChars,
		},
		{
			name:         "mixed multibyte over limit stays valid utf-8",
			input:        strings.Repeat("a記事🎙", 4000),
			wantMaxBytes: maxInputChars,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateInput("test", tt.input)

			if tt.wantSame {
				assert.Equal(t, tt.input, got)
				return
			}

			assert.True(t, utf8.ValidString(got), "truncated text must be valid UTF-8")
			assert.NotContains(t, got, string(utf8.RuneError))

			suffix := "...\n(内容が長いため切り詰めました)"
			assert.True(t, strings.HasSuffix(got, suffix), "truncation suffix missing")

			prefix := strings.TrimSuffix(got, suffix)
			assert.LessOrEqual(t, len(prefix), tt.wantMaxBytes)
			// The prefix must still be a prefix of the original input
			// (no corrupted trailing bytes).
			assert.True(t, strings.HasPrefix(tt.input, prefix))
		})
	}
}
