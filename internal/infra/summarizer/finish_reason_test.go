package summarizer_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/infra/summarizer"
)

// 2026-09-25: 出力上限で打ち切られた応答が「完全な応答」として黙って採用され、
// news セグメントが文の途中で終わった台本のまま番組に載った。ここでは
// finish_reason 相当のフィールドが WARN として可視化されること、そして
// **挙動は一切変わらない**(切り詰められた応答もそのまま返る)ことを固定する。
//
// 応答 JSON は 2026-09-25 に各プロバイダの実 API で確認した形を写したもの:
//   - groq   choices[].finish_reason  "stop" / "length"
//   - gemini candidates[].finishReason "STOP" / "MAX_TOKENS"
//   - ollama done_reason               "stop" / "length"

// captureWarnings redirects the default slog logger into a buffer for the
// duration of the test. The providers log through slog.Default() (same as
// the existing truncateInput warning), so this is how the WARN is observed.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func groqFinishBody(t *testing.T, content, finishReason string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": finishReason,
		}},
		"usage": map[string]any{"completion_tokens": 16},
	})
	require.NoError(t, err)
	return string(body)
}

func geminiFinishBody(t *testing.T, text, finishReason string) string {
	t.Helper()
	candidate := map[string]any{
		"content": map[string]any{
			"parts": []map[string]any{{"text": text}},
			"role":  "model",
		},
		"index": 0,
	}
	if finishReason != "" {
		candidate["finishReason"] = finishReason
	}
	body, err := json.Marshal(map[string]any{"candidates": []map[string]any{candidate}})
	require.NoError(t, err)
	return string(body)
}

func ollamaFinishBody(t *testing.T, response, doneReason string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"model":       "qwen2.5:7b",
		"response":    response,
		"done":        true,
		"done_reason": doneReason,
	})
	require.NoError(t, err)
	return string(body)
}

// truncatedOutput ends mid-sentence, like the 2026-09-25 news segment.
const truncatedOutput = "続いてのニュースです。記事は MITRE ATT&CK、攻"

func TestProviders_FinishReasonVisibility(t *testing.T) {
	opts := summarizer.Options{CharacterLimit: 500, Timeout: 5 * time.Second}

	tests := []struct {
		name         string
		provider     string
		body         func(t *testing.T) string
		newProvider  func(baseURL string) summarizer.Provider
		wantOutput   string
		wantWarn     bool
		wantReason   string
		wantTailPart string
	}{
		{
			name:     "groq truncated by output ceiling",
			provider: "groq",
			body:     func(t *testing.T) string { return groqFinishBody(t, truncatedOutput, "length") },
			newProvider: func(baseURL string) summarizer.Provider {
				return summarizer.NewGroq(summarizer.GroqConfig{APIKey: "k", BaseURL: baseURL, Options: opts})
			},
			wantOutput:   truncatedOutput,
			wantWarn:     true,
			wantReason:   "length",
			wantTailPart: "MITRE ATT&CK、攻",
		},
		{
			name:     "groq finished normally",
			provider: "groq",
			body:     func(t *testing.T) string { return groqFinishBody(t, "完全な応答です。", "stop") },
			newProvider: func(baseURL string) summarizer.Provider {
				return summarizer.NewGroq(summarizer.GroqConfig{APIKey: "k", BaseURL: baseURL, Options: opts})
			},
			wantOutput: "完全な応答です。",
		},
		{
			name:     "gemini truncated by output ceiling",
			provider: "gemini",
			body:     func(t *testing.T) string { return geminiFinishBody(t, truncatedOutput, "MAX_TOKENS") },
			newProvider: func(baseURL string) summarizer.Provider {
				return summarizer.NewGemini(summarizer.GeminiConfig{APIKey: "k", BaseURL: baseURL, Options: opts})
			},
			wantOutput:   truncatedOutput,
			wantWarn:     true,
			wantReason:   "MAX_TOKENS",
			wantTailPart: "MITRE ATT&CK、攻",
		},
		{
			name:     "gemini stopped for a non-length reason",
			provider: "gemini",
			body:     func(t *testing.T) string { return geminiFinishBody(t, "途中まで", "SAFETY") },
			newProvider: func(baseURL string) summarizer.Provider {
				return summarizer.NewGemini(summarizer.GeminiConfig{APIKey: "k", BaseURL: baseURL, Options: opts})
			},
			wantOutput: "途中まで",
			wantWarn:   true,
			wantReason: "SAFETY",
		},
		{
			name:     "gemini finished normally",
			provider: "gemini",
			body:     func(t *testing.T) string { return geminiFinishBody(t, "完全な応答です。", "STOP") },
			newProvider: func(baseURL string) summarizer.Provider {
				return summarizer.NewGemini(summarizer.GeminiConfig{APIKey: "k", BaseURL: baseURL, Options: opts})
			},
			wantOutput: "完全な応答です。",
		},
		{
			name:     "gemini response without a finish reason stays silent",
			provider: "gemini",
			body:     func(t *testing.T) string { return geminiFinishBody(t, "完全な応答です。", "") },
			newProvider: func(baseURL string) summarizer.Provider {
				return summarizer.NewGemini(summarizer.GeminiConfig{APIKey: "k", BaseURL: baseURL, Options: opts})
			},
			wantOutput: "完全な応答です。",
		},
		{
			name:     "ollama truncated by num_predict",
			provider: "ollama",
			body:     func(t *testing.T) string { return ollamaFinishBody(t, truncatedOutput, "length") },
			newProvider: func(baseURL string) summarizer.Provider {
				return summarizer.NewOllama(summarizer.OllamaConfig{Host: baseURL, Options: opts})
			},
			wantOutput:   truncatedOutput,
			wantWarn:     true,
			wantReason:   "length",
			wantTailPart: "MITRE ATT&CK、攻",
		},
		{
			name:     "ollama finished normally",
			provider: "ollama",
			body:     func(t *testing.T) string { return ollamaFinishBody(t, "完全な応答です。", "stop") },
			newProvider: func(baseURL string) summarizer.Provider {
				return summarizer.NewOllama(summarizer.OllamaConfig{Host: baseURL, Options: opts})
			},
			wantOutput: "完全な応答です。",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := tt.body(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(body))
			}))
			defer srv.Close()

			logs := captureWarnings(t)
			out, err := tt.newProvider(srv.URL).Generate(context.Background(), "prompt")

			// 挙動は不変: 切り詰められた応答もそのまま採用される。
			require.NoError(t, err)
			assert.Equal(t, tt.wantOutput, out)

			if !tt.wantWarn {
				assert.NotContains(t, logs.String(), "did not finish normally")
				return
			}

			record := findWarning(t, logs, "llm response did not finish normally, output may be truncated")
			assert.Equal(t, tt.provider, record["provider"])
			assert.Equal(t, tt.wantReason, record["finish_reason"])
			assert.Equal(t, float64(len([]rune(tt.wantOutput))), record["output_length"])
			tail, ok := record["output_tail"].(string)
			require.True(t, ok, "output_tail must be logged")
			assert.True(t, strings.HasSuffix(tt.wantOutput, tail), "tail must be the end of the output")
			if tt.wantTailPart != "" {
				assert.Contains(t, tail, tt.wantTailPart)
			}
		})
	}
}

// TestGroq_FinishReasonLengthWithEmptyContent pins the 実測 shape where the
// reasoning tokens alone hit the ceiling: content comes back empty with
// finish_reason "length". The existing empty-response error is unchanged,
// but the reason is now visible.
func TestGroq_FinishReasonLengthWithEmptyContent(t *testing.T) {
	body := groqFinishBody(t, "", "length")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	logs := captureWarnings(t)
	g := summarizer.NewGroq(summarizer.GroqConfig{APIKey: "k", BaseURL: srv.URL, Options: summarizer.Options{CharacterLimit: 500, Timeout: 5 * time.Second}})

	_, err := g.Generate(context.Background(), "prompt")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty response")
	record := findWarning(t, logs, "llm response did not finish normally, output may be truncated")
	assert.Equal(t, "length", record["finish_reason"])
	assert.Equal(t, float64(0), record["output_length"])
}

// findWarning returns the first captured record whose msg matches.
func findWarning(t *testing.T, logs *bytes.Buffer, msg string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		if record["msg"] == msg {
			return record
		}
	}
	t.Fatalf("expected a WARN with msg %q, got: %s", msg, logs.String())
	return nil
}
