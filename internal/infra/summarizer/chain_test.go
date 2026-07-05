package summarizer_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/infra/summarizer"
)

// fakeProvider is a scriptable Provider for chain-order tests.
type fakeProvider struct {
	name       string
	summary    string
	err        error
	calls      int
	lastPrompt string // prompt received by Generate
	onCall     func() // optional hook (e.g. cancel the parent context)
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Summarize(_ context.Context, _ string) (string, error) {
	f.calls++
	if f.onCall != nil {
		f.onCall()
	}
	if f.err != nil {
		return "", f.err
	}
	return f.summary, nil
}

func (f *fakeProvider) Generate(_ context.Context, prompt string) (string, error) {
	f.calls++
	f.lastPrompt = prompt
	if f.onCall != nil {
		f.onCall()
	}
	if f.err != nil {
		return "", f.err
	}
	return f.summary, nil
}

func TestNewChain_NoProviders(t *testing.T) {
	chain, err := summarizer.NewChain()

	require.ErrorIs(t, err, summarizer.ErrNoProviders)
	assert.Nil(t, chain)
}

func TestChain_SummarizeWithProvider(t *testing.T) {
	errGemini := errors.New("gemini: api error: status 429: quota exceeded")
	errGroq := errors.New("groq: api error: status 503: unavailable")
	errOllama := errors.New("ollama: request failed: connection refused")

	tests := []struct {
		name         string
		providers    []*fakeProvider
		wantSummary  string
		wantProvider string
		wantErrSubs  []string
		wantCalls    []int
	}{
		{
			name: "first provider succeeds, rest untouched",
			providers: []*fakeProvider{
				{name: "gemini", summary: "gemini 要約"},
				{name: "groq", summary: "groq 要約"},
				{name: "ollama", summary: "ollama 要約"},
			},
			wantSummary:  "gemini 要約",
			wantProvider: "gemini",
			wantCalls:    []int{1, 0, 0},
		},
		{
			name: "first fails, second succeeds",
			providers: []*fakeProvider{
				{name: "gemini", err: errGemini},
				{name: "groq", summary: "groq 要約"},
				{name: "ollama", summary: "ollama 要約"},
			},
			wantSummary:  "groq 要約",
			wantProvider: "groq",
			wantCalls:    []int{1, 1, 0},
		},
		{
			name: "first two fail, last succeeds",
			providers: []*fakeProvider{
				{name: "gemini", err: errGemini},
				{name: "groq", err: errGroq},
				{name: "ollama", summary: "ollama 要約"},
			},
			wantSummary:  "ollama 要約",
			wantProvider: "ollama",
			wantCalls:    []int{1, 1, 1},
		},
		{
			name: "all providers fail",
			providers: []*fakeProvider{
				{name: "gemini", err: errGemini},
				{name: "groq", err: errGroq},
				{name: "ollama", err: errOllama},
			},
			wantErrSubs: []string{
				"all summarize providers failed",
				"status 429",
				"status 503",
				"connection refused",
			},
			wantCalls: []int{1, 1, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := make([]summarizer.Provider, len(tt.providers))
			for i, p := range tt.providers {
				providers[i] = p
			}
			chain, err := summarizer.NewChain(providers...)
			require.NoError(t, err)

			summary, provider, err := chain.SummarizeWithProvider(context.Background(), "text")

			if len(tt.wantErrSubs) > 0 {
				require.Error(t, err)
				for _, sub := range tt.wantErrSubs {
					assert.Contains(t, err.Error(), sub)
				}
				assert.Empty(t, summary)
				assert.Empty(t, provider)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantSummary, summary)
				assert.Equal(t, tt.wantProvider, provider)
			}

			for i, p := range tt.providers {
				assert.Equal(t, tt.wantCalls[i], p.calls,
					"unexpected call count for provider %s", p.name)
			}
		})
	}
}

func TestChain_Summarize_DelegatesToChain(t *testing.T) {
	chain, err := summarizer.NewChain(
		&fakeProvider{name: "gemini", err: errors.New("gemini down")},
		&fakeProvider{name: "ollama", summary: "要約"},
	)
	require.NoError(t, err)

	summary, err := chain.Summarize(context.Background(), "text")

	require.NoError(t, err)
	assert.Equal(t, "要約", summary)
}

// TestChain_Generate covers the generic generation entry point used by the
// radio script generator (D-3): same fallback order, prompt passed verbatim.
func TestChain_Generate(t *testing.T) {
	tests := []struct {
		name         string
		providers    []*fakeProvider
		wantOut      string
		wantProvider string
		wantErrSubs  []string
		wantCalls    []int
	}{
		{
			name: "first provider succeeds",
			providers: []*fakeProvider{
				{name: "gemini", summary: "台本原稿"},
				{name: "groq", summary: "unused"},
			},
			wantOut:      "台本原稿",
			wantProvider: "gemini",
			wantCalls:    []int{1, 0},
		},
		{
			name: "first fails, second succeeds",
			providers: []*fakeProvider{
				{name: "gemini", err: errors.New("gemini: api error: status 429")},
				{name: "groq", summary: "groq 台本"},
			},
			wantOut:      "groq 台本",
			wantProvider: "groq",
			wantCalls:    []int{1, 1},
		},
		{
			name: "all providers fail",
			providers: []*fakeProvider{
				{name: "gemini", err: errors.New("gemini: down")},
				{name: "groq", err: errors.New("groq: down")},
			},
			wantErrSubs: []string{"all generate providers failed", "gemini: down", "groq: down"},
			wantCalls:   []int{1, 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providers := make([]summarizer.Provider, len(tt.providers))
			for i, p := range tt.providers {
				providers[i] = p
			}
			chain, err := summarizer.NewChain(providers...)
			require.NoError(t, err)

			out, provider, err := chain.Generate(context.Background(), "ラジオ台本プロンプト")

			if len(tt.wantErrSubs) > 0 {
				require.Error(t, err)
				for _, sub := range tt.wantErrSubs {
					assert.Contains(t, err.Error(), sub)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantOut, out)
				assert.Equal(t, tt.wantProvider, provider)
			}
			for i, p := range tt.providers {
				assert.Equal(t, tt.wantCalls[i], p.calls,
					"unexpected call count for provider %s", p.name)
				if tt.wantCalls[i] > 0 {
					assert.Equal(t, "ラジオ台本プロンプト", p.lastPrompt,
						"Generate must pass the prompt verbatim")
				}
			}
		})
	}
}

func TestChain_ParentContextCanceled_AbortsChain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	first := &fakeProvider{
		name:   "gemini",
		err:    errors.New("gemini: request failed: context canceled"),
		onCall: cancel, // parent context dies while the first provider runs
	}
	second := &fakeProvider{name: "groq", summary: "should not run"}

	chain, err := summarizer.NewChain(first, second)
	require.NoError(t, err)

	summary, provider, err := chain.SummarizeWithProvider(ctx, "text")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "summarize aborted")
	assert.Empty(t, summary)
	assert.Empty(t, provider)
	assert.Equal(t, 1, first.calls)
	assert.Equal(t, 0, second.calls, "chain must not try the next provider after parent context cancellation")
}

// TestChain_RealProviders_Fallback exercises the chain with real HTTP
// providers: Gemini rate-limited -> Groq succeeds.
func TestChain_RealProviders_Fallback(t *testing.T) {
	geminiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"quota exceeded"}}`, http.StatusTooManyRequests)
	}))
	defer geminiSrv.Close()

	groqSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(groqSuccessBody("フォールバック成功。")))
	}))
	defer groqSrv.Close()

	opts := summarizer.Options{CharacterLimit: 900, Timeout: 5 * time.Second}
	chain, err := summarizer.NewChain(
		summarizer.NewGemini(summarizer.GeminiConfig{APIKey: "k", BaseURL: geminiSrv.URL, Options: opts}),
		summarizer.NewGroq(summarizer.GroqConfig{APIKey: "k", BaseURL: groqSrv.URL, Options: opts}),
	)
	require.NoError(t, err)

	summary, provider, err := chain.SummarizeWithProvider(context.Background(), "public article")

	require.NoError(t, err)
	assert.Equal(t, "フォールバック成功。", summary)
	assert.Equal(t, summarizer.ProviderGroq, provider)
}

func TestNewChainFromEnv_Composition(t *testing.T) {
	tests := []struct {
		name          string
		geminiKey     string
		groqKey       string
		ollamaEnabled string
		wantOrder     []string
		wantErr       error
	}{
		{
			name:      "all providers configured",
			geminiKey: "gk", groqKey: "qk", ollamaEnabled: "",
			wantOrder: []string{"gemini", "groq", "ollama"},
		},
		{
			name:      "gemini key missing drops gemini",
			geminiKey: "", groqKey: "qk", ollamaEnabled: "",
			wantOrder: []string{"groq", "ollama"},
		},
		{
			name:      "groq key missing drops groq",
			geminiKey: "gk", groqKey: "", ollamaEnabled: "",
			wantOrder: []string{"gemini", "ollama"},
		},
		{
			name:      "no cloud keys leaves ollama only",
			geminiKey: "", groqKey: "", ollamaEnabled: "",
			wantOrder: []string{"ollama"},
		},
		{
			name:      "ollama disabled with keys",
			geminiKey: "gk", groqKey: "qk", ollamaEnabled: "false",
			wantOrder: []string{"gemini", "groq"},
		},
		{
			name:      "ollama disabled via ParseBool alias 0",
			geminiKey: "gk", groqKey: "", ollamaEnabled: "0",
			wantOrder: []string{"gemini"},
		},
		{
			name:      "ollama explicitly enabled via ParseBool alias TRUE",
			geminiKey: "", groqKey: "", ollamaEnabled: "TRUE",
			wantOrder: []string{"ollama"},
		},
		{
			name:      "unparsable OLLAMA_ENABLED falls back to enabled",
			geminiKey: "", groqKey: "", ollamaEnabled: "yes-please",
			wantOrder: []string{"ollama"},
		},
		{
			name:      "nothing configured is an error",
			geminiKey: "", groqKey: "", ollamaEnabled: "false",
			wantErr: summarizer.ErrNoProviders,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GEMINI_API_KEY", tt.geminiKey)
			t.Setenv("GROQ_API_KEY", tt.groqKey)
			t.Setenv("OLLAMA_ENABLED", tt.ollamaEnabled)

			chain, err := summarizer.NewChainFromEnv(nil)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, chain)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOrder, chain.ProviderNames())
		})
	}
}
