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

// groqSuccessBody builds a minimal successful chat/completions response.
func groqSuccessBody(text string) string {
	body, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": text}},
		},
	})
	return string(body)
}

func newGroq(t *testing.T, baseURL string, opts summarizer.Options) *summarizer.Groq {
	t.Helper()
	return summarizer.NewGroq(summarizer.GroqConfig{
		APIKey:  "test-key",
		Model:   "llama-3.3-70b-versatile",
		BaseURL: baseURL,
		Options: opts,
	})
}

func TestGroq_Summarize_Success(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(groqSuccessBody("Groq からの日本語要約。")))
	}))
	defer srv.Close()

	g := newGroq(t, srv.URL, summarizer.Options{CharacterLimit: 500, Timeout: 5 * time.Second})

	summary, err := g.Summarize(context.Background(), "public article body")

	require.NoError(t, err)
	assert.Equal(t, "Groq からの日本語要約。", summary)
	assert.Equal(t, "/openai/v1/chat/completions", gotPath)
	assert.Equal(t, "Bearer test-key", gotAuth)
	assert.Contains(t, string(gotBody), `"model":"llama-3.3-70b-versatile"`)
	assert.Contains(t, string(gotBody), "500文字以内で要約")
	assert.Contains(t, string(gotBody), "public article body")
}

func TestGroq_Summarize_Errors(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErrSub string
	}{
		{
			name: "unauthorized (401)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
			},
			wantErrSub: "status 401",
		},
		{
			name: "rate limited (429)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":{"message":"rate limit reached"}}`, http.StatusTooManyRequests)
			},
			wantErrSub: "status 429",
		},
		{
			name: "server error (503)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			},
			wantErrSub: "status 503",
		},
		{
			name: "empty choices",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"choices":[]}`))
			},
			wantErrSub: "no choices",
		},
		{
			name: "empty summary content",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(groqSuccessBody("")))
			},
			wantErrSub: "empty summary",
		},
		{
			name: "malformed JSON response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("<html>gateway error</html>"))
			},
			wantErrSub: "decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			g := newGroq(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second})

			summary, err := g.Summarize(context.Background(), "text")

			require.Error(t, err)
			assert.Empty(t, summary)
			assert.Contains(t, err.Error(), tt.wantErrSub)
			assert.Contains(t, err.Error(), "groq")
		})
	}
}

func TestGroq_Summarize_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(groqSuccessBody("late")))
	}))
	defer srv.Close()

	g := newGroq(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 20 * time.Millisecond})

	_, err := g.Summarize(context.Background(), "text")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestLoadGroqConfig(t *testing.T) {
	tests := []struct {
		name      string
		apiKey    string
		model     string
		wantModel string
	}{
		{"defaults", "key", "", "llama-3.3-70b-versatile"},
		{"model override", "key", "openai/gpt-oss-20b", "openai/gpt-oss-20b"},
		{"empty key preserved", "", "", "llama-3.3-70b-versatile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GROQ_API_KEY", tt.apiKey)
			t.Setenv("GROQ_MODEL", tt.model)

			cfg := summarizer.LoadGroqConfig(summarizer.DefaultOptions())

			assert.Equal(t, tt.apiKey, cfg.APIKey)
			assert.Equal(t, tt.wantModel, cfg.Model)
			assert.Equal(t, "https://api.groq.com", cfg.BaseURL)
		})
	}
}
