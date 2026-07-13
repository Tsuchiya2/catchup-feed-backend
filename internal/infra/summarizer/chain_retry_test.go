package summarizer

// D-26 (2) — 2026-07-13 欠番障害の恒久対応: a 429 carrying the provider's
// retry hint waits min(hint, 60s) and retries the same provider once before
// falling back, bounded by a per-process cumulative budget of 5 minutes.
// These tests live in the package (not summarizer_test) to inject the sleep
// function and preload the budget.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stepResult is one scripted provider response.
type stepResult struct {
	out string
	err error
}

// scriptedProvider pops one stepResult per call; past the end it repeats
// the last one.
type scriptedProvider struct {
	name    string
	results []stepResult
	calls   int
}

func (p *scriptedProvider) Name() string { return p.name }

func (p *scriptedProvider) step() (string, error) {
	i := p.calls
	if i >= len(p.results) {
		i = len(p.results) - 1
	}
	p.calls++
	r := p.results[i]
	return r.out, r.err
}

func (p *scriptedProvider) Summarize(_ context.Context, _ string) (string, error) {
	return p.step()
}

func (p *scriptedProvider) Generate(_ context.Context, _ string) (string, error) {
	return p.step()
}

func hinted429(provider string, hint time.Duration) *rateLimitError {
	return &rateLimitError{
		provider:   provider,
		retryAfter: hint,
		msg:        fmt.Sprintf("%s: api error: status 429: rate limited", provider),
	}
}

func TestChain_RateLimitRetry(t *testing.T) {
	tests := []struct {
		name         string
		first        *scriptedProvider
		second       *scriptedProvider
		preWaited    time.Duration // budget already spent before the call
		wantSleeps   []time.Duration
		wantOut      string
		wantProvider string
		wantErr      bool
		wantCalls    [2]int
		wantWaited   time.Duration
	}{
		{
			name: "hinted 429 waits and retries the same provider once",
			first: &scriptedProvider{name: "gemini", results: []stepResult{
				{err: hinted429("gemini", 8*time.Second)},
				{out: "回復した台本"},
			}},
			second:       &scriptedProvider{name: "groq", results: []stepResult{{out: "unused"}}},
			wantSleeps:   []time.Duration{8 * time.Second},
			wantOut:      "回復した台本",
			wantProvider: "gemini",
			wantCalls:    [2]int{2, 0},
			wantWaited:   8 * time.Second,
		},
		{
			name: "hint above the cap waits only maxRetryAfterWait",
			first: &scriptedProvider{name: "gemini", results: []stepResult{
				{err: hinted429("gemini", 5*time.Minute)},
				{out: "回復した台本"},
			}},
			second:       &scriptedProvider{name: "groq", results: []stepResult{{out: "unused"}}},
			wantSleeps:   []time.Duration{maxRetryAfterWait},
			wantOut:      "回復した台本",
			wantProvider: "gemini",
			wantCalls:    [2]int{2, 0},
			wantWaited:   maxRetryAfterWait,
		},
		{
			name: "cumulative budget exceeded falls back immediately",
			first: &scriptedProvider{name: "gemini", results: []stepResult{
				{err: hinted429("gemini", 60*time.Second)},
			}},
			second:       &scriptedProvider{name: "groq", results: []stepResult{{out: "groq 台本"}}},
			preWaited:    retryWaitBudget - 30*time.Second, // 残り30s < 60s
			wantSleeps:   nil,
			wantOut:      "groq 台本",
			wantProvider: "groq",
			wantCalls:    [2]int{1, 1},
			wantWaited:   retryWaitBudget - 30*time.Second,
		},
		{
			name: "429 without a usable hint falls back immediately (current behavior)",
			first: &scriptedProvider{name: "gemini", results: []stepResult{
				{err: hinted429("gemini", 0)},
			}},
			second:       &scriptedProvider{name: "groq", results: []stepResult{{out: "groq 台本"}}},
			wantSleeps:   nil,
			wantOut:      "groq 台本",
			wantProvider: "groq",
			wantCalls:    [2]int{1, 1},
		},
		{
			name: "non-429 error falls back immediately (current behavior)",
			first: &scriptedProvider{name: "gemini", results: []stepResult{
				{err: errors.New("gemini: api error: status 503: unavailable")},
			}},
			second:       &scriptedProvider{name: "groq", results: []stepResult{{out: "groq 台本"}}},
			wantSleeps:   nil,
			wantOut:      "groq 台本",
			wantProvider: "groq",
			wantCalls:    [2]int{1, 1},
		},
		{
			name: "retry failing again falls back — never a third attempt",
			first: &scriptedProvider{name: "gemini", results: []stepResult{
				{err: hinted429("gemini", 5*time.Second)},
				{err: hinted429("gemini", 5*time.Second)},
			}},
			second:       &scriptedProvider{name: "groq", results: []stepResult{{out: "groq 台本"}}},
			wantSleeps:   []time.Duration{5 * time.Second},
			wantOut:      "groq 台本",
			wantProvider: "groq",
			wantCalls:    [2]int{2, 1},
			wantWaited:   5 * time.Second,
		},
		{
			name: "all providers keep failing after a retry",
			first: &scriptedProvider{name: "gemini", results: []stepResult{
				{err: hinted429("gemini", 3*time.Second)},
				{err: hinted429("gemini", 3*time.Second)},
			}},
			second: &scriptedProvider{name: "groq", results: []stepResult{
				{err: errors.New("groq: api error: status 503: down")},
			}},
			wantSleeps: []time.Duration{3 * time.Second},
			wantErr:    true,
			wantCalls:  [2]int{2, 1},
			wantWaited: 3 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain, err := NewChain(tt.first, tt.second)
			require.NoError(t, err)

			var sleeps []time.Duration
			chain.sleep = func(_ context.Context, d time.Duration) error {
				sleeps = append(sleeps, d)
				return nil
			}
			chain.retryWaited.Store(int64(tt.preWaited))

			out, provider, err := chain.Generate(context.Background(), "prompt")

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "all generate providers failed")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantOut, out)
				assert.Equal(t, tt.wantProvider, provider)
			}
			assert.Equal(t, tt.wantSleeps, sleeps, "unexpected retry waits")
			assert.Equal(t, tt.wantCalls[0], tt.first.calls, "first provider call count")
			assert.Equal(t, tt.wantCalls[1], tt.second.calls, "second provider call count")
			assert.Equal(t, int64(tt.wantWaited), chain.retryWaited.Load(),
				"cumulative wait budget accounting")
		})
	}
}

// TestChain_RateLimitRetry_ContextCanceledDuringWait: cancellation while
// waiting on a retry hint aborts the chain — no retry, no further provider.
func TestChain_RateLimitRetry_ContextCanceledDuringWait(t *testing.T) {
	first := &scriptedProvider{name: "gemini", results: []stepResult{
		{err: hinted429("gemini", 30*time.Second)},
	}}
	second := &scriptedProvider{name: "groq", results: []stepResult{{out: "unused"}}}
	chain, err := NewChain(first, second)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	chain.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel() // shutdown arrives mid-wait
		return ctx.Err()
	}

	out, provider, err := chain.Generate(ctx, "prompt")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "generate aborted")
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, out)
	assert.Empty(t, provider)
	assert.Equal(t, 1, first.calls, "no retry after cancellation")
	assert.Equal(t, 0, second.calls, "no fallback after cancellation")
}

// TestChain_RateLimitRetry_SummarizePath: the retry applies to the worker's
// summarize path too (D-26: チェーンは radio/worker 共用のまま).
func TestChain_RateLimitRetry_SummarizePath(t *testing.T) {
	first := &scriptedProvider{name: "gemini", results: []stepResult{
		{err: hinted429("gemini", 2*time.Second)},
		{out: "回復した要約"},
	}}
	chain, err := NewChain(first)
	require.NoError(t, err)

	var sleeps []time.Duration
	chain.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	summary, provider, err := chain.SummarizeWithProvider(context.Background(), "text")

	require.NoError(t, err)
	assert.Equal(t, "回復した要約", summary)
	assert.Equal(t, "gemini", provider)
	assert.Equal(t, []time.Duration{2 * time.Second}, sleeps)
	assert.Equal(t, 2, first.calls)
}

// TestChain_RateLimitRetry_RealProvider wires a real HTTP Gemini provider:
// the first response is the 7/13-style 429 with a body hint, the second
// succeeds — the chain must wait the hinted seconds and recover on Gemini
// without falling back.
func TestChain_RateLimitRetry_RealProvider(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"message":"You exceeded your current quota. Please retry in 37.837394382s.","status":"RESOURCE_EXHAUSTED"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"回復した台本"}]}}]}`))
	}))
	defer srv.Close()

	opts := Options{CharacterLimit: 900, Timeout: 5 * time.Second}
	chain, err := NewChain(NewGemini(GeminiConfig{APIKey: "k", BaseURL: srv.URL, Options: opts}))
	require.NoError(t, err)

	var sleeps []time.Duration
	chain.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		return nil
	}

	out, provider, err := chain.Generate(context.Background(), "prompt")

	require.NoError(t, err)
	assert.Equal(t, "回復した台本", out)
	assert.Equal(t, ProviderGemini, provider)
	assert.Equal(t, []time.Duration{38 * time.Second}, sleeps,
		"37.837s hint rounds up to 38s (min(38s, 60s) = 38s)")
	assert.Equal(t, int32(2), requests.Load())
}
