package fetch_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	fetchUC "catchup-feed/internal/usecase/fetch"
)

/* ───────── Phase 2 §5.1 第1段: Gemini URL 直接入力 ───────── */

// stubVideoDescriber は VideoDescriber のモック実装。呼び出し URL を記録し、
// err が設定されていれば失敗(→ transcribe フォールバック)を再現する。
// cancel が設定されていれば呼び出し時に親 ctx を取り消す(B-1 の流儀:
// 生きた ctx のままセンチネルエラーを返すのはプロバイダ内部タイムアウトと
// 区別できないため、実際に cancel する)。
type stubVideoDescriber struct {
	mu         sync.Mutex
	calls      []string
	transcript string
	summary    string
	err        error
	cancel     context.CancelFunc
}

func (d *stubVideoDescriber) Name() string { return "gemini" }

func (d *stubVideoDescriber) DescribeVideo(_ context.Context, videoURL string) (string, string, error) {
	d.mu.Lock()
	d.calls = append(d.calls, videoURL)
	d.mu.Unlock()
	if d.cancel != nil {
		d.cancel()
	}
	if d.err != nil {
		return "", "", d.err
	}
	return d.transcript, d.summary, nil
}

func (d *stubVideoDescriber) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

// newYouTubeService builds a fetch service wired for the youtube stage-1
// tests: the summarizer and content fetcher must never be touched by the
// youtube path regardless of stage-1 outcome.
func newYouTubeService(t *testing.T, artRepo *stubArticleRepo, fetcher fetchUC.FeedFetcher, kind string) fetchUC.Service {
	t.Helper()
	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 7, FeedURL: "https://example.com/feed", Kind: kind, Active: true},
		},
	}
	return fetchUC.NewService(
		srcRepo,
		artRepo,
		&failingSummarizer{t: t},
		fetcher,
		&failingContentFetcher{t: t},
		fetchUC.ContentFetchConfig{Parallelism: 10, Threshold: 1500},
	)
}

func TestService_CrawlAllSources_YouTubeDirect(t *testing.T) {
	now := time.Now()
	videoItem := func(i int) fetchUC.FeedItem {
		return fetchUC.FeedItem{
			Title:       fmt.Sprintf("Video %d", i),
			URL:         fmt.Sprintf("https://www.youtube.com/watch?v=v%d", i),
			PublishedAt: now,
		}
	}

	tests := []struct {
		name          string
		kind          string
		items         []fetchUC.FeedItem
		existsMap     map[string]bool
		describer     *stubVideoDescriber
		nilDescriber  bool
		wantCalls     int
		wantAttempts  int64
		wantSucceeded int64
		wantInserted  int64
		wantEnqueued  int64
		wantJobs      int
		wantSummaries int
		wantNoMedia   int64
	}{
		{
			name:          "第1段成功: CreateWithSummary 経路、transcribe ジョブなし",
			kind:          entity.SourceKindYouTube,
			items:         []fetchUC.FeedItem{videoItem(1)},
			describer:     &stubVideoDescriber{transcript: "詳細な書き起こし", summary: "日本語要約"},
			wantCalls:     1,
			wantAttempts:  1,
			wantSucceeded: 1,
			wantInserted:  1,
			wantEnqueued:  0,
			wantJobs:      0,
			wantSummaries: 1,
		},
		{
			name:          "第1段失敗: CreateWithTranscribeJob へフォールバック(再試行なし)",
			kind:          entity.SourceKindYouTube,
			items:         []fetchUC.FeedItem{videoItem(1)},
			describer:     &stubVideoDescriber{err: errors.New("gemini: api error: status 429: quota exceeded")},
			wantCalls:     1,
			wantAttempts:  1,
			wantSucceeded: 0,
			wantInserted:  1,
			wantEnqueued:  1,
			wantJobs:      1,
			wantSummaries: 0,
		},
		{
			name: "上限超過: 4本目以降は第1段を試さず transcribe へ",
			kind: entity.SourceKindYouTube,
			items: []fetchUC.FeedItem{
				videoItem(1), videoItem(2), videoItem(3), videoItem(4), videoItem(5),
			},
			describer:     &stubVideoDescriber{transcript: "書き起こし", summary: "要約"},
			wantCalls:     fetchUC.YouTubeDirectMaxPerCycle,
			wantAttempts:  fetchUC.YouTubeDirectMaxPerCycle,
			wantSucceeded: fetchUC.YouTubeDirectMaxPerCycle,
			wantInserted:  5,
			wantEnqueued:  2,
			wantJobs:      2,
			wantSummaries: fetchUC.YouTubeDirectMaxPerCycle,
		},
		{
			name:          "Gemini 無効(describer nil): 第1段スキップで全件 transcribe へ",
			kind:          entity.SourceKindYouTube,
			items:         []fetchUC.FeedItem{videoItem(1), videoItem(2)},
			nilDescriber:  true,
			wantCalls:     0,
			wantAttempts:  0,
			wantSucceeded: 0,
			wantInserted:  2,
			wantEnqueued:  2,
			wantJobs:      2,
			wantSummaries: 0,
		},
		{
			name: "podcast は第1段を通らない(従来どおり直接 transcribe)",
			kind: entity.SourceKindPodcast,
			items: []fetchUC.FeedItem{
				{Title: "Ep 1", URL: "https://example.com/ep1", EnclosureURL: "https://cdn.example.com/ep1.mp3", PublishedAt: now},
			},
			describer:     &stubVideoDescriber{transcript: "書き起こし", summary: "要約"},
			wantCalls:     0,
			wantAttempts:  0,
			wantSucceeded: 0,
			wantInserted:  1,
			wantEnqueued:  1,
			wantJobs:      1,
			wantSummaries: 0,
		},
		{
			// URL 空の item は動画を特定できないため、第1段(Gemini 呼び出し
			// +cap 1枠)を消費せず既存の SkippedNoMedia 経路へ直行する。
			name:        "URL 空の youtube item は第1段を消費せず SkippedNoMedia へ",
			kind:        entity.SourceKindYouTube,
			items:       []fetchUC.FeedItem{{Title: "No URL", PublishedAt: now}},
			describer:   &stubVideoDescriber{transcript: "書き起こし", summary: "要約"},
			wantCalls:   0,
			wantNoMedia: 1,
		},
		{
			name:      "既存 URL は dedupe され第1段も呼ばれない",
			kind:      entity.SourceKindYouTube,
			items:     []fetchUC.FeedItem{videoItem(1)},
			existsMap: map[string]bool{"https://www.youtube.com/watch?v=v1": true},
			describer: &stubVideoDescriber{transcript: "書き起こし", summary: "要約"},
			wantCalls: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artRepo := &stubArticleRepo{existsMap: tt.existsMap}
			fetcher := &stubFeedFetcher{items: tt.items}
			svc := newYouTubeService(t, artRepo, fetcher, tt.kind)
			if !tt.nilDescriber {
				svc.VideoDescriber = tt.describer
			}

			stats, err := svc.CrawlAllSources(context.Background())
			require.NoError(t, err)

			if tt.describer != nil {
				assert.Equal(t, tt.wantCalls, tt.describer.callCount(), "describer calls")
			}
			assert.Equal(t, tt.wantAttempts, stats.YouTubeDirectAttempts, "YouTubeDirectAttempts")
			assert.Equal(t, tt.wantSucceeded, stats.YouTubeDirectSucceeded, "YouTubeDirectSucceeded")
			assert.Equal(t, tt.wantInserted, stats.Inserted, "Inserted")
			assert.Equal(t, tt.wantEnqueued, stats.TranscribeEnqueued, "TranscribeEnqueued")
			assert.Len(t, artRepo.transcribeJobs, tt.wantJobs, "transcribe jobs")
			assert.Len(t, artRepo.summaries, tt.wantSummaries, "persisted summaries")
			assert.Equal(t, tt.wantNoMedia, stats.SkippedNoMedia, "SkippedNoMedia")
		})
	}
}

// TestService_CrawlAllSources_YouTubeDirect_Persistence: 第1段成功時の
// 保存内容の検証 — 書き起こしが articles.content に、要約が summaries に
// provider='gemini' で入り、記事と要約は同一トランザクション経路
// (CreateWithSummary)で永続化される。
func TestService_CrawlAllSources_YouTubeDirect_Persistence(t *testing.T) {
	artRepo := &stubArticleRepo{}
	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{Title: "Video", URL: "https://www.youtube.com/watch?v=abc", PublishedAt: time.Now()},
		},
	}
	svc := newYouTubeService(t, artRepo, fetcher, entity.SourceKindYouTube)
	svc.VideoDescriber = &stubVideoDescriber{transcript: "詳細な内容書き起こし", summary: "日本語要約"}

	_, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	require.Len(t, artRepo.articles, 1)
	art := artRepo.articles[0]
	assert.Equal(t, "詳細な内容書き起こし", art.Content, "transcript goes to articles.content")
	assert.Equal(t, "https://www.youtube.com/watch?v=abc", art.URL)

	sum := artRepo.summaries[art.ID]
	require.NotNil(t, sum, "summary persisted with the article (CreateWithSummary path)")
	assert.Equal(t, "日本語要約", sum.Body)
	assert.Equal(t, "gemini", sum.Provider)
	assert.Empty(t, artRepo.transcribeJobs, "no transcribe job when stage 1 succeeds")
}

// TestService_CrawlAllSources_YouTubeDirect_ContextCanceled: 第1段実行中の
// シャットダウン(親 ctx 消滅)はフォールバックではなくクロール中断。
// 判定は ctx.Err() 直接(B-1: errors.Is はプロバイダ内部タイムアウトと
// 区別できない)。
func TestService_CrawlAllSources_YouTubeDirect_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	artRepo := &stubArticleRepo{}
	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{Title: "Video", URL: "https://www.youtube.com/watch?v=abc", PublishedAt: time.Now()},
		},
	}
	svc := newYouTubeService(t, artRepo, fetcher, entity.SourceKindYouTube)
	svc.VideoDescriber = &stubVideoDescriber{cancel: cancel, err: context.Canceled}

	_, err := svc.CrawlAllSources(ctx)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, artRepo.articles, "no article persisted after shutdown")
	assert.Empty(t, artRepo.transcribeJobs, "no fallback enqueue after shutdown")
}

// TestService_CrawlAllSources_YouTubeDirect_DeadlineWrappedError: describer が
// context.DeadlineExceeded を包んだエラーを返しても、親 ctx が生きていれば
// クロールは中断せず transcribe フォールバックに落ちる(B-1 教訓)。
func TestService_CrawlAllSources_YouTubeDirect_DeadlineWrappedError(t *testing.T) {
	artRepo := &stubArticleRepo{}
	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{Title: "Video", URL: "https://www.youtube.com/watch?v=abc", PublishedAt: time.Now()},
		},
	}
	svc := newYouTubeService(t, artRepo, fetcher, entity.SourceKindYouTube)
	svc.VideoDescriber = &stubVideoDescriber{
		err: fmt.Errorf("gemini: request failed: %w", context.DeadlineExceeded),
	}

	stats, err := svc.CrawlAllSources(context.Background())

	require.NoError(t, err, "per-request timeout must not abort the crawl")
	assert.Equal(t, int64(1), stats.TranscribeEnqueued)
	assert.Len(t, artRepo.transcribeJobs, 1, "falls back to the transcribe queue")
}

// TestService_CrawlAllSources_YouTubeDirect_DBError: 第1段成功後の
// CreateWithSummary の DB エラーはクロール中断(既存経路と同じ扱い)。
func TestService_CrawlAllSources_YouTubeDirect_DBError(t *testing.T) {
	artRepo := &stubArticleRepo{createErr: errors.New("database down")}
	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{Title: "Video", URL: "https://www.youtube.com/watch?v=abc", PublishedAt: time.Now()},
		},
	}
	svc := newYouTubeService(t, artRepo, fetcher, entity.SourceKindYouTube)
	svc.VideoDescriber = &stubVideoDescriber{transcript: "書き起こし", summary: "要約"}

	_, err := svc.CrawlAllSources(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create article with summary")
}
