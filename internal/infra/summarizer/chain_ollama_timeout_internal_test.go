package summarizer

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ollamaStageTimeout digs the Ollama stage's effective per-request timeout out
// of a built chain. An internal test because the whole point of D-46 (2) is a
// per-provider timeout that the public surface does not expose.
func ollamaStageTimeout(t *testing.T, chain *Chain) time.Duration {
	t.Helper()
	for _, p := range chain.providers {
		if o, ok := p.(*Ollama); ok {
			return o.config.Options.Timeout
		}
	}
	t.Fatal("chain has no Ollama stage")
	return 0
}

// geminiStageTimeout is the control: the override must not leak into the cloud
// stages, which keep SUMMARIZER_TIMEOUT.
func geminiStageTimeout(t *testing.T, chain *Chain) time.Duration {
	t.Helper()
	for _, p := range chain.providers {
		if g, ok := p.(*Gemini); ok {
			return g.config.Options.Timeout
		}
	}
	t.Fatal("chain has no Gemini stage")
	return 0
}

// TestNewChainFromEnv_OllamaTimeoutOverride pins D-46 (2): radio が渡した
// Ollama 専用のタイムアウトが Ollama 段にだけ効き、SUMMARIZER_TIMEOUT を
// 上書きしないこと。2026-09-25 の欠番は最終段が 60.002秒 で
// context deadline exceeded になったもので、この分離がその恒久対応。
func TestNewChainFromEnv_OllamaTimeoutOverride(t *testing.T) {
	tests := []struct {
		name              string
		summarizerTimeout string
		options           []ChainEnvOption
		wantOllama        time.Duration
		wantGemini        time.Duration
	}{
		{
			name:              "オプションなし(worker): 両段とも SUMMARIZER_TIMEOUT",
			summarizerTimeout: "60s",
			wantOllama:        60 * time.Second,
			wantGemini:        60 * time.Second,
		},
		{
			name:              "radio: Ollama 段だけ専用予算",
			summarizerTimeout: "60s",
			options:           []ChainEnvOption{WithOllamaTimeout(240 * time.Second)},
			wantOllama:        240 * time.Second,
			wantGemini:        60 * time.Second,
		},
		{
			name:              "0 は無視(誤設定でクラウド段まで壊さない)",
			summarizerTimeout: "45s",
			options:           []ChainEnvOption{WithOllamaTimeout(0)},
			wantOllama:        45 * time.Second,
			wantGemini:        45 * time.Second,
		},
		{
			name:              "負値も無視",
			summarizerTimeout: "45s",
			options:           []ChainEnvOption{WithOllamaTimeout(-1 * time.Second)},
			wantOllama:        45 * time.Second,
			wantGemini:        45 * time.Second,
		},
		{
			name:              "最後に渡した値が勝つ",
			summarizerTimeout: "60s",
			options: []ChainEnvOption{
				WithOllamaTimeout(120 * time.Second),
				WithOllamaTimeout(300 * time.Second),
			},
			wantOllama: 300 * time.Second,
			wantGemini: 60 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("SUMMARIZER_TIMEOUT", tt.summarizerTimeout)
			t.Setenv("GEMINI_API_KEY", "test-key")
			t.Setenv("GROQ_API_KEY", "")
			t.Setenv("OLLAMA_ENABLED", "true")

			chain, err := NewChainFromEnv(slog.Default(), tt.options...)
			require.NoError(t, err)

			assert.Equal(t, tt.wantOllama, ollamaStageTimeout(t, chain))
			assert.Equal(t, tt.wantGemini, geminiStageTimeout(t, chain),
				"クラウド段は SUMMARIZER_TIMEOUT のまま(D-46 (2) は Ollama 段限定)")
		})
	}
}
