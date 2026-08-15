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

// groqReasoningBody builds a response in the shape returned by reasoning models
// such as the default openai/gpt-oss-120b: the thought process is isolated in
// message.reasoning, a field the provider does not decode.
func groqReasoningBody(content, reasoning string) string {
	body, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{
				"role":      "assistant",
				"content":   content,
				"reasoning": reasoning,
			}},
		},
	})
	return string(body)
}

func newGroq(t *testing.T, baseURL string, opts summarizer.Options) *summarizer.Groq {
	t.Helper()
	return summarizer.NewGroq(summarizer.GroqConfig{
		APIKey:  "test-key",
		Model:   "openai/gpt-oss-120b",
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
	assert.Contains(t, string(gotBody), `"model":"openai/gpt-oss-120b"`)
	assert.Contains(t, string(gotBody), "500文字以内で要約")
	assert.Contains(t, string(gotBody), "public article body")
}

// TestGroq_Summarize_ReasoningField pins the premise D-41 chose the default
// model on: the response decoder ignores unknown fields, so message.reasoning is
// discarded and only message.content becomes the summary. A model that moves the
// answer into reasoning (leaving content empty) must fail loudly instead of
// returning the thought process as a summary.
func TestGroq_Summarize_ReasoningField(t *testing.T) {
	const reasoning = "We need a Japanese summary. First, identify the key claim... <think>"

	tests := []struct {
		name        string
		content     string
		wantSummary string
		wantErrSub  string
	}{
		{
			name:        "reasoning is dropped and content is the summary",
			content:     "Groq からの日本語要約。",
			wantSummary: "Groq からの日本語要約。",
		},
		{
			name:       "answer moved into reasoning is not salvaged",
			content:    "",
			wantErrSub: "empty response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(groqReasoningBody(tt.content, reasoning)))
			}))
			defer srv.Close()

			g := newGroq(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second})

			summary, err := g.Summarize(context.Background(), "public article body")

			if tt.wantErrSub != "" {
				require.Error(t, err)
				assert.Empty(t, summary)
				assert.Contains(t, err.Error(), tt.wantErrSub)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantSummary, summary)
			assert.NotContains(t, summary, reasoning)
			assert.NotContains(t, summary, "<think>")
		})
	}
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
			wantErrSub: "empty response",
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
		{"defaults", "key", "", "openai/gpt-oss-120b"},
		{"model override", "key", "llama-3.1-8b-instant", "llama-3.1-8b-instant"},
		{"empty key preserved", "", "", "openai/gpt-oss-120b"},
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
