package tts_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"catchup-feed/internal/tts"
)

func TestSplitSentences(t *testing.T) {
	tests := []struct {
		name   string
		script string
		want   []string
	}{
		{
			name:   "japanese periods",
			script: "おはようございます。今日のニュースです。",
			want:   []string{"おはようございます。", "今日のニュースです。"},
		},
		{
			name:   "exclamation and question marks",
			script: "すごいですね!本当でしょうか?次いきましょう。",
			want:   []string{"すごいですね!", "本当でしょうか?", "次いきましょう。"},
		},
		{
			name:   "consecutive terminators stay together",
			script: "え、まじで!?そうなんです。",
			want:   []string{"え、まじで!?", "そうなんです。"},
		},
		{
			name:   "newlines split without being read",
			script: "一行目\n二行目",
			want:   []string{"一行目", "二行目"},
		},
		{
			name:   "trailing text without terminator is kept",
			script: "終わりました。それでは",
			want:   []string{"終わりました。", "それでは"},
		},
		{
			name:   "whitespace-only fragments are dropped",
			script: "こんにちは。\n\n  \nさようなら。",
			want:   []string{"こんにちは。", "さようなら。"},
		},
		{
			name:   "empty script",
			script: "",
			want:   nil,
		},
		{
			name:   "ascii terminators",
			script: "Go 1.26 is out! Really? Yes.",
			want:   []string{"Go 1.26 is out!", "Really?", "Yes."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tts.SplitSentences(tt.script))
		})
	}
}
