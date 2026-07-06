package fetch_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	fetchUC "catchup-feed/internal/usecase/fetch"
)

/* ───────── SweepUnsummarized (Phase 2 §5.2b) ───────── */

// stubSummaryRepo は SummaryRepository のモック実装(掃き取りの Upsert 検証用)。
type stubSummaryRepo struct {
	mu        sync.Mutex
	upserts   map[int64]*entity.Summary
	upsertErr error
}

func (s *stubSummaryRepo) Upsert(_ context.Context, sum *entity.Summary) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upserts == nil {
		s.upserts = make(map[int64]*entity.Summary)
	}
	s.upserts[sum.ArticleID] = sum
	return nil
}

func (s *stubSummaryRepo) GetByArticleID(_ context.Context, articleID int64) (*entity.Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upserts[articleID], nil
}

func newSweepService(artRepo *stubArticleRepo, sumRepo *stubSummaryRepo, summarizer fetchUC.Summarizer) fetchUC.Service {
	svc := fetchUC.NewService(
		&stubSourceRepo{},
		artRepo,
		summarizer,
		&stubFeedFetcher{},
		nil,
		fetchUC.ContentFetchConfig{Parallelism: 1, Threshold: 1500},
	)
	svc.SummaryRepo = sumRepo
	return svc
}

func TestService_SweepUnsummarized(t *testing.T) {
	tests := []struct {
		name           string
		articles       []*entity.Article
		summaries      map[int64]*entity.Summary // pre-existing summaries (article_id keyed)
		summarizer     fetchUC.Summarizer
		wantSummarized int64
		wantFailed     int64
		wantUpserts    map[int64]string // article_id -> expected provider
	}{
		{
			name: "content-filled article without summary gets summarized with provider",
			articles: []*entity.Article{
				{ID: 1, Content: "transcribed text", URL: "https://example.com/1"},
			},
			summarizer:     &stubProviderSummarizer{provider: "gemini"},
			wantSummarized: 1,
			wantUpserts:    map[int64]string{1: "gemini"},
		},
		{
			name: "article without content is not a candidate",
			articles: []*entity.Article{
				{ID: 1, Content: "", URL: "https://example.com/1"},
			},
			summarizer:  &stubProviderSummarizer{provider: "gemini"},
			wantUpserts: map[int64]string{},
		},
		{
			name: "already-summarized article is not re-summarized",
			articles: []*entity.Article{
				{ID: 1, Content: "already done", URL: "https://example.com/1"},
			},
			summaries: map[int64]*entity.Summary{
				1: {ArticleID: 1, Body: "existing", Provider: "groq"},
			},
			summarizer:  &stubProviderSummarizer{provider: "gemini"},
			wantUpserts: map[int64]string{},
		},
		{
			name: "summarization failure leaves the article for the next cycle",
			articles: []*entity.Article{
				{ID: 1, Content: "poison", URL: "https://example.com/1"},
				{ID: 2, Content: "fine", URL: "https://example.com/2"},
			},
			summarizer:     &stubProviderSummarizer{provider: "ollama", failOn: "poison"},
			wantSummarized: 1,
			wantFailed:     1,
			wantUpserts:    map[int64]string{2: "ollama"},
		},
		{
			name: "plain summarizer without provider persists provider=unknown",
			articles: []*entity.Article{
				{ID: 1, Content: "transcribed text", URL: "https://example.com/1"},
			},
			summarizer:     &stubSummarizer{result: "summary"},
			wantSummarized: 1,
			wantUpserts:    map[int64]string{1: entity.SummaryProviderUnknown},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artRepo := &stubArticleRepo{articles: tt.articles, summaries: tt.summaries}
			sumRepo := &stubSummaryRepo{}
			svc := newSweepService(artRepo, sumRepo, tt.summarizer)

			stats, err := svc.SweepUnsummarized(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.wantSummarized, stats.Summarized, "Summarized")
			assert.Equal(t, tt.wantFailed, stats.Failed, "Failed")
			assert.False(t, stats.LimitHit, "LimitHit")

			assert.Len(t, sumRepo.upserts, len(tt.wantUpserts))
			for articleID, provider := range tt.wantUpserts {
				got := sumRepo.upserts[articleID]
				require.NotNil(t, got, "summary for article %d", articleID)
				assert.Equal(t, provider, got.Provider)
				assert.NotEmpty(t, got.Body)
			}
		})
	}
}

func TestService_SweepUnsummarized_Errors(t *testing.T) {
	t.Run("missing SummaryRepo is an error", func(t *testing.T) {
		svc := fetchUC.NewService(
			&stubSourceRepo{}, &stubArticleRepo{}, &stubSummarizer{},
			&stubFeedFetcher{}, nil, fetchUC.ContentFetchConfig{Parallelism: 1},
		)
		_, err := svc.SweepUnsummarized(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SummaryRepo")
	})

	t.Run("candidate query error aborts the sweep", func(t *testing.T) {
		artRepo := &stubArticleRepo{listUnsummarizedErr: errors.New("db down")}
		svc := newSweepService(artRepo, &stubSummaryRepo{}, &stubSummarizer{})
		_, err := svc.SweepUnsummarized(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "db down")
	})

	t.Run("upsert error aborts the sweep", func(t *testing.T) {
		artRepo := &stubArticleRepo{articles: []*entity.Article{
			{ID: 1, Content: "text", URL: "https://example.com/1"},
		}}
		sumRepo := &stubSummaryRepo{upsertErr: errors.New("insert failed")}
		svc := newSweepService(artRepo, sumRepo, &stubSummarizer{})
		stats, err := svc.SweepUnsummarized(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "insert failed")
		assert.Equal(t, int64(0), stats.Summarized)
	})

	t.Run("dead context aborts instead of counting a soft failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		artRepo := &stubArticleRepo{articles: []*entity.Article{
			{ID: 1, Content: "text", URL: "https://example.com/1"},
			{ID: 2, Content: "more", URL: "https://example.com/2"},
		}}
		sumRepo := &stubSummaryRepo{}
		// cancels the context inside the first summarize call
		svc := newSweepService(artRepo, sumRepo, &stubProviderSummarizer{cancel: cancel})
		stats, err := svc.SweepUnsummarized(ctx)
		require.Error(t, err)
		assert.Equal(t, int64(0), stats.Failed, "cancellation is not a per-article failure")
		assert.Empty(t, sumRepo.upserts)
	})
}

func TestService_SweepUnsummarized_Limit(t *testing.T) {
	// More candidates than the per-cycle cap: exactly DefaultSweepLimit are
	// processed and the overflow is flagged for the log (§5.2b 上限で切っ
	// た場合はログ). The remainder is picked up by the next hourly cycle.
	articles := make([]*entity.Article, 0, fetchUC.DefaultSweepLimit+3)
	for i := range fetchUC.DefaultSweepLimit + 3 {
		articles = append(articles, &entity.Article{
			ID:      int64(i + 1),
			Content: fmt.Sprintf("transcript %d", i+1),
			URL:     fmt.Sprintf("https://example.com/%d", i+1),
		})
	}
	artRepo := &stubArticleRepo{articles: articles}
	sumRepo := &stubSummaryRepo{}
	svc := newSweepService(artRepo, sumRepo, &stubProviderSummarizer{provider: "gemini"})

	stats, err := svc.SweepUnsummarized(context.Background())
	require.NoError(t, err)
	assert.True(t, stats.LimitHit)
	assert.Equal(t, fetchUC.DefaultSweepLimit, stats.Candidates)
	assert.Equal(t, int64(fetchUC.DefaultSweepLimit), stats.Summarized)
	assert.Len(t, sumRepo.upserts, fetchUC.DefaultSweepLimit)
}
