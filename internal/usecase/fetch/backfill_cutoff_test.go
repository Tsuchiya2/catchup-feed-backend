package fetch_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	fetchUC "catchup-feed/internal/usecase/fetch"
)

/* ───────── D-15: transcribe 経路のバックログカットオフ ───────── */

// TestService_CrawlAllSources_TranscribeBackfillCutoff fixes the D-15
// contract (Phase 2 §5.2): transcribe-path items (youtube/podcast) whose
// published_at is older than TranscribeBackfillCutoff (14 days) are skipped
// entirely — no articles INSERT, no transcribe job — and counted in
// SkippedBackfill. Unknown published_at (zero value) is treated as new.
func TestService_CrawlAllSources_TranscribeBackfillCutoff(t *testing.T) {
	now := time.Now()
	old15d := now.Add(-15 * 24 * time.Hour)
	new13d := now.Add(-13 * 24 * time.Hour)

	tests := []struct {
		name                string
		kind                string
		items               []fetchUC.FeedItem
		wantInserted        int64
		wantEnqueued        int64
		wantSkippedBackfill int64
		// wantArticleURLs は保存された articles.url の期待値(挿入順)。
		// nil なら検証しない。
		wantArticleURLs []string
	}{
		{
			name: "youtube: 15日前の item はスキップ(INSERT もジョブもなし)",
			kind: entity.SourceKindYouTube,
			items: []fetchUC.FeedItem{
				{Title: "Old video", URL: "https://www.youtube.com/watch?v=old", PublishedAt: old15d},
			},
			wantSkippedBackfill: 1,
		},
		{
			name: "youtube: 13日前の item は取り込む",
			kind: entity.SourceKindYouTube,
			items: []fetchUC.FeedItem{
				{Title: "Recent video", URL: "https://www.youtube.com/watch?v=recent", PublishedAt: new13d},
			},
			wantInserted: 1,
			wantEnqueued: 1,
		},
		{
			name: "youtube: published_at 不明(zero value)は新着扱い(D-15)",
			kind: entity.SourceKindYouTube,
			items: []fetchUC.FeedItem{
				{Title: "No date", URL: "https://www.youtube.com/watch?v=nodate"},
			},
			wantInserted: 1,
			wantEnqueued: 1,
		},
		{
			name: "podcast: 15日前の item はスキップ",
			kind: entity.SourceKindPodcast,
			items: []fetchUC.FeedItem{
				{Title: "Old ep", URL: "https://example.com/old", EnclosureURL: "https://cdn.example.com/old.mp3", PublishedAt: old15d},
			},
			wantSkippedBackfill: 1,
		},
		{
			name: "podcast: 全履歴フィード(多数の古い item + 少数の新しい item)は新しい item のみ取り込む",
			kind: entity.SourceKindPodcast,
			items: func() []fetchUC.FeedItem {
				// Latent Space 相当: 210 本の過去回 + 2 本の直近回。
				items := make([]fetchUC.FeedItem, 0, 212)
				for i := 0; i < 210; i++ {
					items = append(items, fetchUC.FeedItem{
						Title:        fmt.Sprintf("Backlog %d", i),
						URL:          fmt.Sprintf("https://example.com/backlog-%d", i),
						EnclosureURL: fmt.Sprintf("https://cdn.example.com/backlog-%d.mp3", i),
						PublishedAt:  now.Add(-time.Duration(15+i) * 24 * time.Hour),
					})
				}
				items = append(items,
					fetchUC.FeedItem{
						Title:        "Fresh 1",
						URL:          "https://example.com/fresh-1",
						EnclosureURL: "https://cdn.example.com/fresh-1.mp3",
						PublishedAt:  now.Add(-24 * time.Hour),
					},
					fetchUC.FeedItem{
						Title:        "Fresh 2",
						URL:          "https://example.com/fresh-2",
						EnclosureURL: "https://cdn.example.com/fresh-2.mp3",
						PublishedAt:  new13d,
					},
				)
				return items
			}(),
			wantInserted:        2,
			wantEnqueued:        2,
			wantSkippedBackfill: 210,
			wantArticleURLs:     []string{"https://example.com/fresh-1", "https://example.com/fresh-2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcRepo := &stubSourceRepo{
				sources: []*entity.Source{
					{ID: 7, FeedURL: "https://example.com/feed", Kind: tt.kind, Active: true},
				},
			}
			artRepo := &stubArticleRepo{}
			fetcher := &stubFeedFetcher{items: tt.items}

			svc := fetchUC.NewService(
				srcRepo,
				artRepo,
				&failingSummarizer{t: t},
				fetcher,
				&failingContentFetcher{t: t},
				fetchUC.ContentFetchConfig{Parallelism: 10, Threshold: 1500},
			)

			stats, err := svc.CrawlAllSources(context.Background())
			require.NoError(t, err)

			assert.Equal(t, int64(len(tt.items)), stats.FeedItems, "FeedItems (skipped items are still counted as seen)")
			assert.Equal(t, tt.wantInserted, stats.Inserted, "Inserted")
			assert.Equal(t, tt.wantEnqueued, stats.TranscribeEnqueued, "TranscribeEnqueued")
			assert.Equal(t, tt.wantSkippedBackfill, stats.SkippedBackfill, "SkippedBackfill")
			assert.Zero(t, stats.SkippedNoMedia, "SkippedNoMedia")
			assert.Zero(t, stats.Duplicated, "Duplicated")

			assert.Len(t, artRepo.articles, int(tt.wantInserted), "persisted articles")
			assert.Len(t, artRepo.transcribeJobs, int(tt.wantEnqueued), "transcribe jobs")
			if tt.wantArticleURLs != nil {
				gotURLs := make([]string, 0, len(artRepo.articles))
				for _, art := range artRepo.articles {
					gotURLs = append(gotURLs, art.URL)
				}
				assert.Equal(t, tt.wantArticleURLs, gotURLs, "articles.url")
			}
		})
	}
}

// TestService_CrawlAllSources_TranscribeBackfillCutoff_BeforeYouTubeDirectCap:
// the cutoff check sits BEFORE the §5.1 stage-1 cap, so old items never
// consume the per-cycle Gemini budget (YouTubeDirectMaxPerCycle) — the one
// fresh item in a backlog-heavy feed still gets its stage-1 attempt.
func TestService_CrawlAllSources_TranscribeBackfillCutoff_BeforeYouTubeDirectCap(t *testing.T) {
	now := time.Now()

	// 古い動画を cap(3)より多く並べ、新しい動画を末尾に1本置く。
	// 古い item が cap を消費するバグがあれば、新しい item は第1段に
	// 到達できず describer は呼ばれない。
	items := make([]fetchUC.FeedItem, 0, 6)
	for i := 0; i < 5; i++ {
		items = append(items, fetchUC.FeedItem{
			Title:       fmt.Sprintf("Old video %d", i),
			URL:         fmt.Sprintf("https://www.youtube.com/watch?v=old%d", i),
			PublishedAt: now.Add(-time.Duration(20+i) * 24 * time.Hour),
		})
	}
	items = append(items, fetchUC.FeedItem{
		Title:       "Fresh video",
		URL:         "https://www.youtube.com/watch?v=fresh",
		PublishedAt: now.Add(-24 * time.Hour),
	})

	artRepo := &stubArticleRepo{}
	describer := &stubVideoDescriber{transcript: "書き起こし", summary: "要約"}
	svc := newYouTubeService(t, artRepo, &stubFeedFetcher{items: items}, entity.SourceKindYouTube)
	svc.VideoDescriber = describer

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(5), stats.SkippedBackfill, "SkippedBackfill")
	assert.Equal(t, int64(1), stats.YouTubeDirectAttempts, "YouTubeDirectAttempts")
	assert.Equal(t, int64(1), stats.YouTubeDirectSucceeded, "YouTubeDirectSucceeded")
	assert.Equal(t, int64(1), stats.Inserted, "Inserted")
	assert.Zero(t, stats.TranscribeEnqueued, "TranscribeEnqueued")
	assert.Equal(t, []string{"https://www.youtube.com/watch?v=fresh"}, describer.calls, "describer must only see the fresh item")
}

// TestService_CrawlAllSources_TranscribeBackfillCutoff_RSSUnaffected: the
// rss path (processFeedItems) ingests items regardless of published_at —
// D-15 scopes the cutoff to the transcribe path only.
func TestService_CrawlAllSources_TranscribeBackfillCutoff_RSSUnaffected(t *testing.T) {
	old15d := time.Now().Add(-15 * 24 * time.Hour)

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 7, FeedURL: "https://example.com/feed", Kind: entity.SourceKindRSS, Active: true},
		},
	}
	artRepo := &stubArticleRepo{}
	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{Title: "Old article", URL: "https://example.com/old-article", Content: "本文", PublishedAt: old15d},
		},
	}

	svc := fetchUC.NewService(
		srcRepo, artRepo, &stubSummarizer{}, fetcher, nil,
		fetchUC.ContentFetchConfig{Parallelism: 10, Threshold: 1500},
	)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(1), stats.Inserted, "Inserted (rss path must ignore the cutoff)")
	assert.Zero(t, stats.SkippedBackfill, "SkippedBackfill")
	require.Len(t, artRepo.articles, 1)
	assert.Equal(t, "https://example.com/old-article", artRepo.articles[0].URL)
	assert.Len(t, artRepo.summaries, 1, "rss path persists a summary as usual")
}
