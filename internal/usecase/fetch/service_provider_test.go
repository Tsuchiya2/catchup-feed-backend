package fetch_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	fetchUC "catchup-feed/internal/usecase/fetch"
)

/* ───────── ProviderSummarizer（フォールバック連鎖)経路のテスト ───────── */

// chainAllFailedErr mimics the summarizer chain's all-providers-failed error.
// The gemini leg wraps context.DeadlineExceeded (provider-internal timeout),
// which is the exact shape that must NOT be mistaken for a dead crawl context.
func chainAllFailedErr() error {
	return fmt.Errorf("all summarizer providers failed: %w", errors.Join(
		fmt.Errorf("gemini: request failed: %w", context.DeadlineExceeded),
		errors.New("groq: api error: status 429: rate limit reached"),
		errors.New("ollama: request failed: connection refused"),
	))
}

// stubProviderSummarizer mimics the fallback chain: it implements both
// Summarize and SummarizeWithProvider, failing for failOn content with an
// error that wraps context.DeadlineExceeded.
type stubProviderSummarizer struct {
	failOn   string
	provider string
	cancel   context.CancelFunc // optional: cancel the parent context on call
}

func (s *stubProviderSummarizer) Summarize(ctx context.Context, text string) (string, error) {
	summary, _, err := s.SummarizeWithProvider(ctx, text)
	return summary, err
}

func (s *stubProviderSummarizer) SummarizeWithProvider(_ context.Context, text string) (string, string, error) {
	if s.cancel != nil {
		s.cancel()
		return "", "", fmt.Errorf("summarize aborted: %w", context.Canceled)
	}
	if text == s.failOn {
		return "", "", chainAllFailedErr()
	}
	return "Summary: " + text, s.provider, nil
}

func newProviderTestService(summarizer fetchUC.Summarizer, artRepo *stubArticleRepo, items []fetchUC.FeedItem) fetchUC.Service {
	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}
	fetcher := &stubFeedFetcher{items: items}

	return fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{Parallelism: 10, Threshold: 1500},
	)
}

// TestService_CrawlAllSources_ProviderChainAllFailed_ContinuesCrawl is the
// B-1 regression test: an all-providers-failed error that wraps
// context.DeadlineExceeded (gemini timed out internally) must skip that one
// article and keep crawling — not abort the whole crawl (§8 縮退許容).
func TestService_CrawlAllSources_ProviderChainAllFailed_ContinuesCrawl(t *testing.T) {
	now := time.Now()
	items := []fetchUC.FeedItem{
		{Title: "Doomed", URL: "https://example.com/doomed", Content: "doomed content", PublishedAt: now},
		{Title: "Fine", URL: "https://example.com/fine", Content: "fine content", PublishedAt: now},
	}
	artRepo := &stubArticleRepo{existsMap: make(map[string]bool)}
	summarizer := &stubProviderSummarizer{failOn: "doomed content", provider: "groq"}

	svc := newProviderTestService(summarizer, artRepo, items)

	stats, err := svc.CrawlAllSources(context.Background())

	require.NoError(t, err, "provider-internal timeout in an all-failed chain must not abort the crawl")
	require.NotNil(t, stats)
	assert.Equal(t, int64(1), stats.SummarizeError, "the doomed article is counted as a summarize error")
	assert.Equal(t, int64(1), stats.Inserted, "the other article is still inserted")
	require.Len(t, artRepo.articles, 1)
	assert.Equal(t, "https://example.com/fine", artRepo.articles[0].URL)
	assert.Equal(t, "Summary: fine content", artRepo.articles[0].Summary)
}

// TestService_CrawlAllSources_ProviderChainParentCanceled_Aborts is the
// contrast case: when the parent context itself dies (shutdown / crawl
// deadline), the crawl must abort instead of skipping articles.
func TestService_CrawlAllSources_ProviderChainParentCanceled_Aborts(t *testing.T) {
	now := time.Now()
	items := []fetchUC.FeedItem{
		{Title: "Article 1", URL: "https://example.com/article1", Content: "content 1", PublishedAt: now},
		{Title: "Article 2", URL: "https://example.com/article2", Content: "content 2", PublishedAt: now},
	}
	artRepo := &stubArticleRepo{existsMap: make(map[string]bool)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	summarizer := &stubProviderSummarizer{cancel: cancel}

	svc := newProviderTestService(summarizer, artRepo, items)

	_, err := svc.CrawlAllSources(ctx)

	require.Error(t, err, "a dead parent context must abort the crawl")
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, artRepo.articles, "no article should be inserted after cancellation")
}

// TestService_CrawlAllSources_ProviderRecorded verifies the happy path of the
// ProviderSummarizer interface: the summary body AND the provider name
// reported by the fallback chain are persisted to summaries (§4).
func TestService_CrawlAllSources_ProviderRecorded(t *testing.T) {
	now := time.Now()
	items := []fetchUC.FeedItem{
		{Title: "Article 1", URL: "https://example.com/article1", Content: "content 1", PublishedAt: now},
	}
	artRepo := &stubArticleRepo{existsMap: make(map[string]bool)}
	summarizer := &stubProviderSummarizer{provider: "gemini"}

	svc := newProviderTestService(summarizer, artRepo, items)

	stats, err := svc.CrawlAllSources(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Inserted)
	require.Len(t, artRepo.articles, 1)
	assert.Equal(t, "Summary: content 1", artRepo.articles[0].Summary)

	sum := artRepo.summaries[artRepo.articles[0].ID]
	require.NotNil(t, sum, "summary must be persisted")
	assert.Equal(t, "Summary: content 1", sum.Body)
	assert.Equal(t, "gemini", sum.Provider, "summaries.provider records the chain leg that succeeded")
}
