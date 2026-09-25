package radio_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/radio"
)

// TestLoadConfig_OllamaTimeout pins D-46 (2): radio の Ollama 呼び出しの
// タイムアウトは要約連鎖の SUMMARIZER_TIMEOUT から独立した専用 env
// (RADIO_OLLAMA_TIMEOUT)で、既定は 240秒。worker と違って radio には
// 「次回持ち越し」がなく、最終段で諦めた日は欠番になる。
func TestLoadConfig_OllamaTimeout(t *testing.T) {
	tests := []struct {
		name              string
		value             string
		summarizerTimeout string
		want              time.Duration
	}{
		{name: "未設定は既定 240秒", summarizerTimeout: "60s", want: 240 * time.Second},
		{name: "SUMMARIZER_TIMEOUT には連動しない", summarizerTimeout: "10s", want: 240 * time.Second},
		{name: "上書き", value: "6m", want: 6 * time.Minute},
		{name: "0 は既定へフォールバック(縮退)", value: "0s", want: 240 * time.Second},
		{name: "負値も既定へ", value: "-30s", want: 240 * time.Second},
		{name: "duration でない値も既定へ", value: "soon", want: 240 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.summarizerTimeout != "" {
				t.Setenv("SUMMARIZER_TIMEOUT", tt.summarizerTimeout)
			}
			if tt.value != "" {
				t.Setenv("RADIO_OLLAMA_TIMEOUT", tt.value)
			}
			cfg, err := radio.LoadConfig(nil)
			require.NoError(t, err)
			assert.Equal(t, tt.want, cfg.OllamaTimeout)
		})
	}
}
