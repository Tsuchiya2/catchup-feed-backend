package fetch_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	fetchUC "catchup-feed/internal/usecase/fetch"
)

// orderRecordingFetcher records the order in which feed URLs are fetched.
// CrawlAllSources はソースを逐次処理するので、fetch 順 = ソース処理順。
type orderRecordingFetcher struct {
	mu    sync.Mutex
	order []string
	feeds map[string][]fetchUC.FeedItem
}

func (f *orderRecordingFetcher) Fetch(_ context.Context, url string) ([]fetchUC.FeedItem, error) {
	f.mu.Lock()
	f.order = append(f.order, url)
	f.mu.Unlock()
	if items, ok := f.feeds[url]; ok {
		return items, nil
	}
	return nil, errors.New("unknown feed URL")
}

// TestService_CrawlAllSources_TranscribeSourcesFirst fixes the crawl order:
// transcribe kind (youtube/podcast) sources are processed before rss sources
// even when their ids sort after every rss source (本番障害: id 305〜309 の
// youtube/podcast ソースが id 順逐次処理の末尾で毎サイクル未到達)。
// 同 kind 内は ListActive の返す id 順を維持する(安定ソート)。
func TestService_CrawlAllSources_TranscribeSourcesFirst(t *testing.T) {
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/rss1", Kind: entity.SourceKindRSS, Active: true},
			{ID: 2, FeedURL: "https://example.com/rss2", Kind: "", Active: true}, // kind 空 = rss 扱い
			{ID: 305, FeedURL: "https://example.com/yt305", Kind: entity.SourceKindYouTube, Active: true},
			{ID: 306, FeedURL: "https://example.com/pod306", Kind: entity.SourceKindPodcast, Active: true},
			{ID: 307, FeedURL: "https://example.com/yt307", Kind: entity.SourceKindYouTube, Active: true},
		},
	}
	artRepo := &stubArticleRepo{}
	fetcher := &orderRecordingFetcher{
		feeds: map[string][]fetchUC.FeedItem{
			"https://example.com/rss1":   {{Title: "R1", URL: "https://example.com/r1", Content: "c1", PublishedAt: now}},
			"https://example.com/rss2":   {{Title: "R2", URL: "https://example.com/r2", Content: "c2", PublishedAt: now}},
			"https://example.com/yt305":  {{Title: "V305", URL: "https://www.youtube.com/watch?v=v305", PublishedAt: now}},
			"https://example.com/pod306": {{Title: "E306", URL: "https://example.com/e306", EnclosureURL: "https://cdn.example.com/e306.mp3", PublishedAt: now}},
			"https://example.com/yt307":  {{Title: "V307", URL: "https://www.youtube.com/watch?v=v307", PublishedAt: now}},
		},
	}

	svc := fetchUC.NewService(
		srcRepo, artRepo, &stubSummarizer{}, fetcher, nil,
		fetchUC.ContentFetchConfig{Parallelism: 10, Threshold: 1500},
	)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	// transcribe kind が先、rss が後。各グループ内は id 順のまま。
	assert.Equal(t, []string{
		"https://example.com/yt305",
		"https://example.com/pod306",
		"https://example.com/yt307",
		"https://example.com/rss1",
		"https://example.com/rss2",
	}, fetcher.order, "source processing order (transcribe first, id order within kind)")

	assert.Equal(t, int64(5), stats.Inserted)
	assert.Equal(t, int64(3), stats.TranscribeEnqueued)
}

// TestService_CrawlAllSources_TranscribeSurvivesRSSAbort reproduces the
// production failure: 要約フォールバック連鎖の全滅で rss ソース処理中に
// クロール全体が中断しても(§8 の中断セマンティクスは変更しない)、
// 先行して処理された transcribe ソースの INSERT + transcribe ジョブ
// enqueue は完了済みであること。修正前は rss(id 1)が先に処理され、
// 中断により youtube(id 305)へ一度も到達しなかった。
func TestService_CrawlAllSources_TranscribeSurvivesRSSAbort(t *testing.T) {
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			// id 順では rss が先。ソートで youtube が先行するはず。
			{ID: 1, FeedURL: "https://example.com/rss1", Kind: entity.SourceKindRSS, Active: true},
			{ID: 305, FeedURL: "https://example.com/yt305", Kind: entity.SourceKindYouTube, Active: true},
		},
	}
	artRepo := &stubArticleRepo{}
	fetcher := &orderRecordingFetcher{
		feeds: map[string][]fetchUC.FeedItem{
			"https://example.com/rss1":  {{Title: "R1", URL: "https://example.com/r1", Content: "c1", PublishedAt: now}},
			"https://example.com/yt305": {{Title: "V305", URL: "https://www.youtube.com/watch?v=v305", PublishedAt: now}},
		},
	}

	// 要約全滅 → クロール中断の再現: summarizer が親 ctx を cancel して
	// context.Canceled を返す(既存 ContextCancellation テストと同じ流儀。
	// 親 ctx が死んだ場合のみ processFeedItems は中断エラーを返す)。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	summarizer := &cancelingSummarizer{cancel: cancel}

	svc := fetchUC.NewService(
		srcRepo, artRepo, summarizer, fetcher, nil,
		fetchUC.ContentFetchConfig{Parallelism: 10, Threshold: 1500},
	)

	_, err := svc.CrawlAllSources(ctx)
	require.Error(t, err, "rss summarize abort must still abort the crawl (§8 unchanged)")
	require.ErrorIs(t, err, context.Canceled)

	// youtube ソースが rss より先に処理されている。
	require.GreaterOrEqual(t, len(fetcher.order), 1)
	assert.Equal(t, "https://example.com/yt305", fetcher.order[0], "transcribe source must be crawled before rss")

	// 中断前に transcribe ソースの INSERT + enqueue は完了済み。
	require.Len(t, artRepo.transcribeJobs, 1)
	assert.Equal(t, transcribeJob{
		ArticleID:  1,
		MediaURL:   "https://www.youtube.com/watch?v=v305",
		SourceKind: entity.SourceKindYouTube,
	}, artRepo.transcribeJobs[0])

	require.Len(t, artRepo.articles, 1)
	assert.Equal(t, "https://www.youtube.com/watch?v=v305", artRepo.articles[0].URL)
	assert.Equal(t, int64(305), artRepo.articles[0].SourceID)
}
