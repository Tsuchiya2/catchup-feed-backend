package summarizer_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/infra/summarizer"
)

// geminiSuccessBody builds a minimal successful generateContent response.
func geminiSuccessBody(text string) string {
	body, _ := json.Marshal(map[string]any{
		"candidates": []map[string]any{
			{"content": map[string]any{"parts": []map[string]any{{"text": text}}}},
		},
	})
	return string(body)
}

func newGemini(t *testing.T, baseURL string, opts summarizer.Options) *summarizer.Gemini {
	t.Helper()
	return summarizer.NewGemini(summarizer.GeminiConfig{
		APIKey:  "test-key",
		Model:   "gemini-2.5-flash",
		BaseURL: baseURL,
		Options: opts,
	})
}

func TestGemini_Summarize_Success(t *testing.T) {
	var gotPath, gotAPIKey string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get("x-goog-api-key")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(geminiSuccessBody("日本語の要約です。")))
	}))
	defer srv.Close()

	g := newGemini(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second})

	summary, err := g.Summarize(context.Background(), "public article body")

	require.NoError(t, err)
	assert.Equal(t, "日本語の要約です。", summary)
	assert.Equal(t, "/v1beta/models/gemini-2.5-flash:generateContent", gotPath)
	assert.Equal(t, "test-key", gotAPIKey)
	assert.Contains(t, string(gotBody), "900文字以内で要約")
	assert.Contains(t, string(gotBody), "public article body")
	// Thinking must be disabled: gemini-2.5-flash thinks by default and
	// would burn extra free-tier tokens (ゼロ円運用).
	assert.Contains(t, string(gotBody), `"thinkingBudget":0`)
}

func TestGemini_Summarize_Errors(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErrSub string
	}{
		{
			name: "rate limited (429)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":{"message":"quota exceeded"}}`, http.StatusTooManyRequests)
			},
			wantErrSub: "status 429",
		},
		{
			name: "server error (500)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "internal error", http.StatusInternalServerError)
			},
			wantErrSub: "status 500",
		},
		{
			name: "invalid api key (403)",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":{"message":"API key not valid"}}`, http.StatusForbidden)
			},
			wantErrSub: "status 403",
		},
		{
			name: "malformed JSON response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("{not json"))
			},
			wantErrSub: "decode response",
		},
		{
			name: "no candidates",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"candidates":[]}`))
			},
			wantErrSub: "no candidates",
		},
		{
			name: "empty summary text",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(geminiSuccessBody("   ")))
			},
			wantErrSub: "empty summary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			g := newGemini(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second})

			summary, err := g.Summarize(context.Background(), "text")

			require.Error(t, err)
			assert.Empty(t, summary)
			assert.Contains(t, err.Error(), tt.wantErrSub)
			assert.Contains(t, err.Error(), "gemini")
		})
	}
}

func TestGemini_Summarize_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(geminiSuccessBody("late")))
	}))
	defer srv.Close()

	g := newGemini(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 20 * time.Millisecond})

	_, err := g.Summarize(context.Background(), "text")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestGemini_Summarize_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close() // closed immediately: connection refused

	g := newGemini(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: time.Second})

	_, err := g.Summarize(context.Background(), "text")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestGemini_Summarize_TruncatesLongInput(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(geminiSuccessBody("要約")))
	}))
	defer srv.Close()

	g := newGemini(t, srv.URL, summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second})

	longText := strings.Repeat("a", 20000)
	_, err := g.Summarize(context.Background(), longText)

	require.NoError(t, err)
	assert.Contains(t, string(gotBody), "内容が長いため切り詰めました")
	// The raw 20k-char input must not be sent as-is.
	assert.NotContains(t, string(gotBody), longText)
}

func TestLoadGeminiConfig(t *testing.T) {
	tests := []struct {
		name      string
		apiKey    string
		model     string
		wantModel string
	}{
		{"defaults", "key", "", "gemini-2.5-flash"},
		{"model override", "key", "gemini-2.0-flash", "gemini-2.0-flash"},
		{"empty key preserved", "", "", "gemini-2.5-flash"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GEMINI_API_KEY", tt.apiKey)
			t.Setenv("GEMINI_MODEL", tt.model)

			cfg := summarizer.LoadGeminiConfig(summarizer.DefaultOptions())

			assert.Equal(t, tt.apiKey, cfg.APIKey)
			assert.Equal(t, tt.wantModel, cfg.Model)
			assert.Equal(t, "https://generativelanguage.googleapis.com", cfg.BaseURL)
		})
	}
}
