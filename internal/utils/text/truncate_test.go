package text_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"catchup-feed/internal/utils/text"
)

func TestTruncateBytes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		limit   int
		want    string
		wantCut bool
	}{
		{name: "under the limit is untouched", input: "hello", limit: 10, want: "hello"},
		{name: "exactly at the limit is untouched", input: "hello", limit: 5, want: "hello"},
		{name: "ascii is cut at the limit", input: "hello", limit: 3, want: "hel", wantCut: true},
		{
			// "あ" は3バイト。limit=4 は境界をまたぐので3バイトまで戻る。
			name:  "multibyte cut backs off to a rune boundary",
			input: "ああ", limit: 4, want: "あ", wantCut: true,
		},
		{name: "limit smaller than the first rune yields empty", input: "あ", limit: 2, want: "", wantCut: true},
		{name: "non-positive limit means no limit", input: "ああ", limit: 0, want: "ああ"},
		{name: "negative limit means no limit", input: "ああ", limit: -1, want: "ああ"},
		{name: "empty input", input: "", limit: 5, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cut := text.TruncateBytes(tt.input, tt.limit)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantCut, cut)
			assert.True(t, utf8.ValidString(got), "切り詰め結果は常に妥当な UTF-8")
			assert.True(t, strings.HasPrefix(tt.input, got), "元テキストの接頭辞であること")
		})
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		limit   int
		want    string
		wantCut bool
	}{
		{name: "under the limit is untouched", input: "要約です", limit: 10, want: "要約です"},
		{name: "exactly at the limit is untouched", input: "要約です", limit: 4, want: "要約です"},
		{name: "japanese is cut by characters, not bytes", input: "要約です", limit: 2, want: "要約", wantCut: true},
		{name: "mixed text counts runes", input: "Go 1.27 の要約", limit: 8, want: "Go 1.27 ", wantCut: true},
		{name: "emoji is one rune", input: "🎙🎙🎙", limit: 2, want: "🎙🎙", wantCut: true},
		{name: "non-positive limit means no limit", input: "要約です", limit: 0, want: "要約です"},
		{name: "empty input", input: "", limit: 3, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cut := text.TruncateRunes(tt.input, tt.limit)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantCut, cut)
			assert.True(t, utf8.ValidString(got))
			if tt.limit > 0 {
				assert.LessOrEqual(t, text.CountRunes(got), tt.limit)
			}
		})
	}
}
