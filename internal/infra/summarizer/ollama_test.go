package summarizer_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/infra/summarizer"
)

// ollamaSuccessBody builds a minimal successful /api/generate response.
func ollamaSuccessBody(text string) string {
	body, _ := json.Marshal(map[string]any{
		"model":    "qwen2.5:7b",
		"response": text,
		"done":     true,
	})
	return string(body)
}

func newOllama(t *testing.T, host string, opts summarizer.Options) *summarizer.Ollama {
	t.Helper()
	return summarizer.NewOllama(summarizer.OllamaConfig{
		Host:    host,
		Model:   "qwen2.5:7b",
		Options: opts,
	})
}

func TestOllama_Summarize_Success(t *testing.T) {
	var gotPath string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(ollamaSuccessBody("ローカルモデルの日本語要約。")))
	}))
	defer srv.Close()

	o := newOllama(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second})

	summary, err := o.Summarize(context.Background(), "public article body")

	require.NoError(t, err)
	assert.Equal(t, "ローカルモデルの日本語要約。", summary)
	assert.Equal(t, "/api/generate", gotPath)
	assert.Contains(t, string(gotBody), `"model":"qwen2.5:7b"`)
	assert.Contains(t, string(gotBody), `"stream":false`)
	assert.Contains(t, string(gotBody), "900文字以内で要約")
	assert.Contains(t, string(gotBody), "public article body")
}

func TestOllama_Summarize_Errors(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErrSub string
	}{
		{
			name: "model not found (404)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"model 'qwen2.5:7b' not found"}`, http.StatusNotFound)
			},
			wantErrSub: "status 404",
		},
		{
			name: "server error (500)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "boom", http.StatusInternalServerError)
			},
			wantErrSub: "status 500",
		},
		{
			name: "empty response text",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(ollamaSuccessBody("")))
			},
			wantErrSub: "empty response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			o := newOllama(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second})

			summary, err := o.Summarize(context.Background(), "text")

			require.Error(t, err)
			assert.Empty(t, summary)
			assert.Contains(t, err.Error(), tt.wantErrSub)
			assert.Contains(t, err.Error(), "ollama")
		})
	}
}

func TestOllama_Summarize_ConnectionRefused(t *testing.T) {
	// Ollama not running (Mac asleep / not on tailnet): connection refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	o := newOllama(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: time.Second})

	_, err := o.Summarize(context.Background(), "text")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestLoadOllamaConfig(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		model     string
		wantHost  string
		wantModel string
	}{
		{"defaults", "", "", "http://localhost:11434", "qwen2.5:7b"},
		{"overrides", "http://mac.tailnet:11434", "gpt-oss:20b", "http://mac.tailnet:11434", "gpt-oss:20b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OLLAMA_HOST", tt.host)
			t.Setenv("OLLAMA_MODEL", tt.model)

			cfg := summarizer.LoadOllamaConfig(summarizer.DefaultOptions())

			assert.Equal(t, tt.wantHost, cfg.Host)
			assert.Equal(t, tt.wantModel, cfg.Model)
		})
	}
}
