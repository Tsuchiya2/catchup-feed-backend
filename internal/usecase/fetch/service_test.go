package fetch_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
	fetchUC "catchup-feed/internal/usecase/fetch"
)

/* ───────── モック実装 ───────── */

// stubSourceRepo はSourceRepositoryのモック実装
type stubSourceRepo struct {
	sources       []*entity.Source
	listActiveErr error
}

func (s *stubSourceRepo) ListActive(_ context.Context) ([]*entity.Source, error) {
	return s.sources, s.listActiveErr
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubSourceRepo) Get(_ context.Context, _ int64) (*entity.Source, error) {
	return nil, nil
}
func (s *stubSourceRepo) List(_ context.Context) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubSourceRepo) Search(_ context.Context, _ string) ([]*entity.Source, error) {
	return nil, nil
}
func (s *stubSourceRepo) Create(_ context.Context, _ *entity.Source) error {
	return nil
}
func (s *stubSourceRepo) Update(_ context.Context, _ *entity.Source) error {
	return nil
}
func (s *stubSourceRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubSourceRepo) SearchWithFilters(_ context.Context, _ []string, _ repository.SourceSearchFilters) ([]*entity.Source, error) {
	return nil, nil
}

// transcribeJob は CreateWithTranscribeJob で記事と同時に enqueue された
// transcribe ジョブの記録（Phase 2 §5 の payload 検証用）。
type transcribeJob struct {
	ArticleID  int64
	MediaURL   string
	SourceKind string
}

// stubArticleRepo はArticleRepositoryのモック実装。
// summaries は CreateWithSummary で記事と同時に永続化された要約を
// article_id ごとに記録する（summaries.provider の検証用）。
// transcribeJobs は CreateWithTranscribeJob の enqueue 記録。
type stubArticleRepo struct {
	mu                  sync.Mutex
	articles            []*entity.Article
	summaries           map[int64]*entity.Summary
	transcribeJobs      []transcribeJob
	existsMap           map[string]bool
	existsErr           error
	createErr           error
	listUnsummarizedErr error
	nextID              int64
}

func (s *stubArticleRepo) ExistsByURLBatch(_ context.Context, urls []string) (map[string]bool, error) {
	if s.existsErr != nil {
		return nil, s.existsErr
	}
	result := make(map[string]bool)
	for _, url := range urls {
		if s.existsMap != nil {
			result[url] = s.existsMap[url]
		}
	}
	return result, nil
}

func (s *stubArticleRepo) Create(_ context.Context, a *entity.Article) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	a.ID = s.nextID
	s.articles = append(s.articles, a)
	return nil
}

func (s *stubArticleRepo) CreateWithSummary(_ context.Context, a *entity.Article, sum *entity.Summary) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	a.ID = s.nextID
	s.articles = append(s.articles, a)
	sum.ArticleID = a.ID
	if s.summaries == nil {
		s.summaries = make(map[int64]*entity.Summary)
	}
	s.summaries[a.ID] = sum
	return nil
}

func (s *stubArticleRepo) CreateWithTranscribeJob(_ context.Context, a *entity.Article, mediaURL, sourceKind string) error {
	if s.createErr != nil {
		return s.createErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	a.ID = s.nextID
	s.articles = append(s.articles, a)
	s.transcribeJobs = append(s.transcribeJobs, transcribeJob{
		ArticleID:  a.ID,
		MediaURL:   mediaURL,
		SourceKind: sourceKind,
	})
	return nil
}

// ListUnsummarized returns articles with content but no recorded summary
// (掃き取りパス §5.2b の対象抽出をメモリ上で再現).
func (s *stubArticleRepo) ListUnsummarized(_ context.Context, limit int) ([]*entity.Article, error) {
	if s.listUnsummarizedErr != nil {
		return nil, s.listUnsummarizedErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*entity.Article
	for _, a := range s.articles {
		if a.Content == "" {
			continue
		}
		if _, ok := s.summaries[a.ID]; ok {
			continue
		}
		out = append(out, a)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// 以下は未使用だが、インターフェース満たすために実装
func (s *stubArticleRepo) List(_ context.Context) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubArticleRepo) Get(_ context.Context, _ int64) (*entity.Article, error) {
	return nil, nil
}
func (s *stubArticleRepo) Search(_ context.Context, _ string) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubArticleRepo) Update(_ context.Context, _ *entity.Article) error {
	return nil
}
func (s *stubArticleRepo) Delete(_ context.Context, _ int64) error {
	return nil
}
func (s *stubArticleRepo) ExistsByURL(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *stubArticleRepo) GetWithSource(_ context.Context, _ int64) (*entity.Article, string, error) {
	return nil, "", nil
}
func (s *stubArticleRepo) ListWithSource(_ context.Context) ([]repository.ArticleWithSource, error) {
	return nil, nil
}
func (s *stubArticleRepo) SearchWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) ([]*entity.Article, error) {
	return nil, nil
}
func (s *stubArticleRepo) CountArticles(_ context.Context) (int64, error) {
	return 0, nil
}
func (s *stubArticleRepo) ListWithSourcePaginated(_ context.Context, _, _ int) ([]repository.ArticleWithSource, error) {
	return nil, nil
}
func (s *stubArticleRepo) CountArticlesWithFilters(_ context.Context, _ []string, _ repository.ArticleSearchFilters) (int64, error) {
	return 0, nil
}
func (s *stubArticleRepo) SearchWithFiltersPaginated(_ context.Context, _ []string, _ repository.ArticleSearchFilters, _, _ int) ([]repository.ArticleWithSource, error) {
	return nil, nil
}

// stubFeedFetcher はFeedFetcherのモック実装
type stubFeedFetcher struct {
	items []fetchUC.FeedItem
	err   error
}

func (s *stubFeedFetcher) Fetch(_ context.Context, _ string) ([]fetchUC.FeedItem, error) {
	return s.items, s.err
}

// stubSummarizer はSummarizerのモック実装
type stubSummarizer struct {
	result string
	err    error
}

func (s *stubSummarizer) Summarize(_ context.Context, text string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.result != "" {
		return s.result, nil
	}
	return "Summary of: " + text, nil
}

// multiSourceFetcher は複数ソース対応のFeedFetcherモック
type multiSourceFetcher struct {
	feeds map[string][]fetchUC.FeedItem
}

func (f *multiSourceFetcher) Fetch(_ context.Context, url string) ([]fetchUC.FeedItem, error) {
	if items, ok := f.feeds[url]; ok {
		return items, nil
	}
	return nil, errors.New("unknown feed URL")
}

// selectiveSummarizer は特定コンテンツで失敗するSummarizerモック
type selectiveSummarizer struct {
	failOn string
}

func (s *selectiveSummarizer) Summarize(_ context.Context, text string) (string, error) {
	if text == s.failOn {
		return "", errors.New("intentional summarization failure")
	}
	return "Summary: " + text, nil
}

// cancelingSummarizer は呼び出し時に親コンテキストを取消して
// context.Canceled を返す Summarizer モック（要約中のシャットダウンを再現）。
// 親 ctx が生きたままセンチネルエラーだけ返すのは「プロバイダ内部タイムアウト」
// と区別できないため、実際に cancel する。
type cancelingSummarizer struct {
	cancel context.CancelFunc
}

func (s *cancelingSummarizer) Summarize(_ context.Context, _ string) (string, error) {
	s.cancel()
	return "", context.Canceled
}

/* ───────── テストケース ───────── */

func TestService_CrawlAllSources_HappyPath(t *testing.T) {
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	artRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{
				Title:       "Article 1",
				URL:         "https://example.com/article1",
				Content:     "Content 1",
				PublishedAt: now,
			},
			{
				Title:       "Article 2",
				URL:         "https://example.com/article2",
				Content:     "Content 2",
				PublishedAt: now,
			},
		},
	}

	summarizer := &stubSummarizer{
		result: "Test summary",
	}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	stats, err := svc.CrawlAllSources(context.Background())
	if err != nil {
		t.Fatalf("CrawlAllSources() error = %v", err)
	}

	if stats.Sources != 1 {
		t.Errorf("Sources = %d, want 1", stats.Sources)
	}
	if stats.FeedItems != 2 {
		t.Errorf("FeedItems = %d, want 2", stats.FeedItems)
	}
	if stats.Inserted != 2 {
		t.Errorf("Inserted = %d, want 2", stats.Inserted)
	}
	if stats.Duplicated != 0 {
		t.Errorf("Duplicated = %d, want 0", stats.Duplicated)
	}
	if stats.SummarizeError != 0 {
		t.Errorf("SummarizeError = %d, want 0", stats.SummarizeError)
	}

	// 2つの記事が作成されたことを確認
	if len(artRepo.articles) != 2 {
		t.Errorf("created articles = %d, want 2", len(artRepo.articles))
	}

	// 要約が記事と同一トランザクションで永続化されたことを確認。
	// plain Summarizer はプロバイダ名を報告できないため "unknown" になる。
	if len(artRepo.summaries) != 2 {
		t.Errorf("persisted summaries = %d, want 2", len(artRepo.summaries))
	}
	for _, art := range artRepo.articles {
		sum := artRepo.summaries[art.ID]
		if sum == nil {
			t.Errorf("summary for article %d not persisted", art.ID)
			continue
		}
		if sum.Body != "Test summary" {
			t.Errorf("summary body = %q, want %q", sum.Body, "Test summary")
		}
		if sum.Provider != entity.SummaryProviderUnknown {
			t.Errorf("summary provider = %q, want %q", sum.Provider, entity.SummaryProviderUnknown)
		}
	}
}

func TestService_CrawlAllSources_DuplicateHandling(t *testing.T) {
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	// article1は既に存在すると設定
	artRepo := &stubArticleRepo{
		existsMap: map[string]bool{
			"https://example.com/article1": true,
		},
	}

	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{
				Title:       "Article 1",
				URL:         "https://example.com/article1",
				Content:     "Content 1",
				PublishedAt: now,
			},
			{
				Title:       "Article 2",
				URL:         "https://example.com/article2",
				Content:     "Content 2",
				PublishedAt: now,
			},
		},
	}

	summarizer := &stubSummarizer{
		result: "Test summary",
	}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	stats, err := svc.CrawlAllSources(context.Background())
	if err != nil {
		t.Fatalf("CrawlAllSources() error = %v", err)
	}

	if stats.FeedItems != 2 {
		t.Errorf("FeedItems = %d, want 2", stats.FeedItems)
	}
	if stats.Inserted != 1 {
		t.Errorf("Inserted = %d, want 1", stats.Inserted)
	}
	if stats.Duplicated != 1 {
		t.Errorf("Duplicated = %d, want 1", stats.Duplicated)
	}

	// 1つの新しい記事のみが作成されたことを確認
	if len(artRepo.articles) != 1 {
		t.Errorf("created articles = %d, want 1", len(artRepo.articles))
	}
	if artRepo.articles[0].URL != "https://example.com/article2" {
		t.Errorf("created article URL = %s, want https://example.com/article2", artRepo.articles[0].URL)
	}
}

func TestService_CrawlAllSources_EmptyFeed(t *testing.T) {
	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	artRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	// 空のフィード
	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{},
	}

	summarizer := &stubSummarizer{}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	stats, err := svc.CrawlAllSources(context.Background())
	if err != nil {
		t.Fatalf("CrawlAllSources() error = %v", err)
	}

	if stats.Sources != 1 {
		t.Errorf("Sources = %d, want 1", stats.Sources)
	}
	if stats.FeedItems != 0 {
		t.Errorf("FeedItems = %d, want 0", stats.FeedItems)
	}
	if stats.Inserted != 0 {
		t.Errorf("Inserted = %d, want 0", stats.Inserted)
	}

}

func TestService_CrawlAllSources_FetchError(t *testing.T) {
	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	artRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	// フェッチエラー
	fetcher := &stubFeedFetcher{
		err: errors.New("fetch failed"),
	}

	summarizer := &stubSummarizer{}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	// フェッチエラーは警告ログが出力されるだけで、エラーを返さない
	stats, err := svc.CrawlAllSources(context.Background())
	if err != nil {
		t.Fatalf("CrawlAllSources() error = %v, want nil", err)
	}

	if stats.Sources != 1 {
		t.Errorf("Sources = %d, want 1", stats.Sources)
	}
	if stats.FeedItems != 0 {
		t.Errorf("FeedItems = %d, want 0", stats.FeedItems)
	}

}

func TestService_CrawlAllSources_SummarizerError(t *testing.T) {
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	artRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{
				Title:       "Article 1",
				URL:         "https://example.com/article1",
				Content:     "Content 1",
				PublishedAt: now,
			},
		},
	}

	// Summarizerがエラーを返す
	summarizer := &stubSummarizer{
		err: errors.New("summarize failed"),
	}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	// Summarizerエラーでもクロール全体は継続する（エラーを返さない）
	stats, err := svc.CrawlAllSources(context.Background())
	if err != nil {
		t.Fatalf("CrawlAllSources() unexpected error = %v", err)
	}

	// 統計にsummarize errorが記録されていることを確認
	if stats.SummarizeError != 1 {
		t.Errorf("stats.SummarizeError = %d, want 1", stats.SummarizeError)
	}

	// 記事は挿入されていない（要約に失敗したため）
	if stats.Inserted != 0 {
		t.Errorf("stats.Inserted = %d, want 0", stats.Inserted)
	}

	// feed itemsは処理された
	if stats.FeedItems != 1 {
		t.Errorf("stats.FeedItems = %d, want 1", stats.FeedItems)
	}
}

func TestService_CrawlAllSources_ExistsByURLBatchError(t *testing.T) {
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	// ExistsByURLBatchがエラーを返す
	artRepo := &stubArticleRepo{
		existsErr: errors.New("database error"),
	}

	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{
				Title:       "Article 1",
				URL:         "https://example.com/article1",
				Content:     "Content 1",
				PublishedAt: now,
			},
		},
	}

	summarizer := &stubSummarizer{}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	// ExistsByURLBatchエラーは警告ログが出力されるだけで、エラーを返さない
	stats, err := svc.CrawlAllSources(context.Background())
	if err != nil {
		t.Fatalf("CrawlAllSources() error = %v, want nil", err)
	}

	if stats.Sources != 1 {
		t.Errorf("Sources = %d, want 1", stats.Sources)
	}
	// ExistsByURLBatchエラーでcontinueするため、FeedItemsはカウントされない
	if stats.FeedItems != 0 {
		t.Errorf("FeedItems = %d, want 0", stats.FeedItems)
	}
}

func TestService_CrawlAllSources_NoActiveSources(t *testing.T) {
	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{},
	}

	artRepo := &stubArticleRepo{}
	fetcher := &stubFeedFetcher{}
	summarizer := &stubSummarizer{}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	stats, err := svc.CrawlAllSources(context.Background())
	if err != nil {
		t.Fatalf("CrawlAllSources() error = %v", err)
	}

	if stats.Sources != 0 {
		t.Errorf("Sources = %d, want 0", stats.Sources)
	}
	if stats.FeedItems != 0 {
		t.Errorf("FeedItems = %d, want 0", stats.FeedItems)
	}
	if stats.Inserted != 0 {
		t.Errorf("Inserted = %d, want 0", stats.Inserted)
	}
}

func TestService_CrawlAllSources_ListActiveError(t *testing.T) {
	srcRepo := &stubSourceRepo{
		listActiveErr: errors.New("database error"),
	}

	artRepo := &stubArticleRepo{}
	fetcher := &stubFeedFetcher{}
	summarizer := &stubSummarizer{}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	_, err := svc.CrawlAllSources(context.Background())
	if err == nil {
		t.Fatal("CrawlAllSources() error = nil, want error")
	}

	// エラーメッセージに"list active sources"が含まれることを確認
	if err.Error() != "list active sources: database error" {
		t.Errorf("error message = %q, want 'list active sources: database error'", err.Error())
	}
}

/* ───────── Phase 2 §5: youtube / podcast の kind 分岐 ───────── */

// failingSummarizer panics if called: youtube/podcast items must never
// reach the summarizer (content NULL のうちは要約対象外, Phase 2 §4).
type failingSummarizer struct{ t *testing.T }

func (s *failingSummarizer) Summarize(_ context.Context, _ string) (string, error) {
	s.t.Error("summarizer must not be called for youtube/podcast sources")
	return "", errors.New("unexpected call")
}

// failingContentFetcher fails the test if called: youtube/podcast items
// must never reach go-readability (Phase 2 §5).
type failingContentFetcher struct{ t *testing.T }

func (f *failingContentFetcher) FetchContent(_ context.Context, _ string) (string, error) {
	f.t.Error("content fetcher must not be called for youtube/podcast sources")
	return "", errors.New("unexpected call")
}

func TestService_CrawlAllSources_TranscribeKinds(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		kind           string
		items          []fetchUC.FeedItem
		existsMap      map[string]bool
		wantInserted   int64
		wantEnqueued   int64
		wantSkipped    int64
		wantDuplicated int64
		wantJobs       []transcribeJob
		// wantArticleURLs は保存された articles.url の期待値(挿入順)。
		// nil なら検証しない。
		wantArticleURLs []string
	}{
		{
			name: "youtube: media_url is the video URL",
			kind: entity.SourceKindYouTube,
			items: []fetchUC.FeedItem{
				{Title: "Video 1", URL: "https://www.youtube.com/watch?v=abc", PublishedAt: now},
			},
			wantInserted: 1,
			wantEnqueued: 1,
			wantJobs: []transcribeJob{
				{ArticleID: 1, MediaURL: "https://www.youtube.com/watch?v=abc", SourceKind: entity.SourceKindYouTube},
			},
		},
		{
			name: "podcast: media_url is the enclosure URL",
			kind: entity.SourceKindPodcast,
			items: []fetchUC.FeedItem{
				{Title: "Ep 1", URL: "https://example.com/ep1", EnclosureURL: "https://cdn.example.com/ep1.mp3", PublishedAt: now},
			},
			wantInserted: 1,
			wantEnqueued: 1,
			wantJobs: []transcribeJob{
				{ArticleID: 1, MediaURL: "https://cdn.example.com/ep1.mp3", SourceKind: entity.SourceKindPodcast},
			},
		},
		{
			name: "podcast: items without enclosure are skipped with log (§5.2)",
			kind: entity.SourceKindPodcast,
			items: []fetchUC.FeedItem{
				{Title: "No enclosure", URL: "https://example.com/ep1", PublishedAt: now},
				{Title: "Ep 2", URL: "https://example.com/ep2", EnclosureURL: "https://cdn.example.com/ep2.mp3", PublishedAt: now},
			},
			wantInserted: 1,
			wantEnqueued: 1,
			wantSkipped:  1,
			wantJobs: []transcribeJob{
				{ArticleID: 1, MediaURL: "https://cdn.example.com/ep2.mp3", SourceKind: entity.SourceKindPodcast},
			},
		},
		{
			name: "duplicates are not re-enqueued",
			kind: entity.SourceKindYouTube,
			items: []fetchUC.FeedItem{
				{Title: "Known", URL: "https://www.youtube.com/watch?v=known", PublishedAt: now},
			},
			existsMap:      map[string]bool{"https://www.youtube.com/watch?v=known": true},
			wantDuplicated: 1,
		},
		{
			// CodeRabbit 指摘(PR #77): <link> なし podcast エピソードで
			// articles.url が空のまま INSERT されないこと。enclosure URL に
			// フォールバックする。
			name: "podcast: link なしは enclosure URL を articles.url に使う",
			kind: entity.SourceKindPodcast,
			items: []fetchUC.FeedItem{
				{Title: "No link", URL: "", EnclosureURL: "https://cdn.example.com/nolink.mp3", PublishedAt: now},
			},
			wantInserted: 1,
			wantEnqueued: 1,
			wantJobs: []transcribeJob{
				{ArticleID: 1, MediaURL: "https://cdn.example.com/nolink.mp3", SourceKind: entity.SourceKindPodcast},
			},
			wantArticleURLs: []string{"https://cdn.example.com/nolink.mp3"},
		},
		{
			// 不変条件: フォールバック後の URL が dedupe キーにもなる。
			// 前サイクルで enclosure URL として INSERT 済みの link なし
			// エピソードは、次サイクルで Duplicated になり再 INSERT
			// (= articles.url UNIQUE 制約違反)を起こさない。
			name: "podcast: link なしの既存エピソードは次サイクルで dedupe される",
			kind: entity.SourceKindPodcast,
			items: []fetchUC.FeedItem{
				{Title: "No link", URL: "", EnclosureURL: "https://cdn.example.com/nolink.mp3", PublishedAt: now},
			},
			existsMap:      map[string]bool{"https://cdn.example.com/nolink.mp3": true},
			wantDuplicated: 1,
		},
		{
			// link も enclosure も無い podcast item は従来どおりスキップ
			// (フォールバックしても空のまま → SkippedNoMedia)。
			name: "podcast: link も enclosure も無い item はスキップ",
			kind: entity.SourceKindPodcast,
			items: []fetchUC.FeedItem{
				{Title: "Nothing", URL: "", EnclosureURL: "", PublishedAt: now},
			},
			wantSkipped: 1,
		},
		{
			// youtube は URL 空なら従来どおり SkippedNoMedia(enclosure への
			// フォールバックは podcast 限定であることの確認)。
			name: "youtube: URL 空は enclosure があっても SkippedNoMedia",
			kind: entity.SourceKindYouTube,
			items: []fetchUC.FeedItem{
				{Title: "No URL", URL: "", EnclosureURL: "https://cdn.example.com/should-not-be-used.mp3", PublishedAt: now},
			},
			wantSkipped: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcRepo := &stubSourceRepo{
				sources: []*entity.Source{
					{ID: 7, FeedURL: "https://example.com/feed", Kind: tt.kind, Active: true},
				},
			}
			artRepo := &stubArticleRepo{existsMap: tt.existsMap}
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
			if err != nil {
				t.Fatalf("CrawlAllSources() error = %v", err)
			}

			if stats.Inserted != tt.wantInserted {
				t.Errorf("Inserted = %d, want %d", stats.Inserted, tt.wantInserted)
			}
			if stats.TranscribeEnqueued != tt.wantEnqueued {
				t.Errorf("TranscribeEnqueued = %d, want %d", stats.TranscribeEnqueued, tt.wantEnqueued)
			}
			if stats.SkippedNoMedia != tt.wantSkipped {
				t.Errorf("SkippedNoMedia = %d, want %d", stats.SkippedNoMedia, tt.wantSkipped)
			}
			if stats.Duplicated != tt.wantDuplicated {
				t.Errorf("Duplicated = %d, want %d", stats.Duplicated, tt.wantDuplicated)
			}
			if stats.SummarizeError != 0 {
				t.Errorf("SummarizeError = %d, want 0", stats.SummarizeError)
			}

			// 記事は content 空(=NULL)・要約なしで保存される。
			// articles.url は必ず非空(UNIQUE 制約のキー。link なし podcast
			// は enclosure URL にフォールバックしている)
			for _, art := range artRepo.articles {
				if art.Content != "" {
					t.Errorf("article content = %q, want empty (content NULL until transcribed)", art.Content)
				}
				if art.URL == "" {
					t.Errorf("article %q has empty URL, want non-empty (fallback to media URL)", art.Title)
				}
			}
			if tt.wantArticleURLs != nil {
				gotURLs := make([]string, 0, len(artRepo.articles))
				for _, art := range artRepo.articles {
					gotURLs = append(gotURLs, art.URL)
				}
				assert.Equal(t, tt.wantArticleURLs, gotURLs, "articles.url")
			}
			if len(artRepo.summaries) != 0 {
				t.Errorf("persisted summaries = %d, want 0", len(artRepo.summaries))
			}

			if len(artRepo.transcribeJobs) != len(tt.wantJobs) {
				t.Fatalf("transcribe jobs = %d, want %d", len(artRepo.transcribeJobs), len(tt.wantJobs))
			}
			for i, want := range tt.wantJobs {
				got := artRepo.transcribeJobs[i]
				if got != want {
					t.Errorf("transcribe job[%d] = %+v, want %+v", i, got, want)
				}
			}
		})
	}
}

// TestService_CrawlAllSources_TranscribeDBError: DB エラーはクロール中断
// (既存 rss パスの CreateWithSummary エラーと同じ扱い)。
func TestService_CrawlAllSources_TranscribeDBError(t *testing.T) {
	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 7, FeedURL: "https://example.com/feed", Kind: entity.SourceKindYouTube, Active: true},
		},
	}
	artRepo := &stubArticleRepo{createErr: errors.New("database down")}
	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{Title: "Video", URL: "https://www.youtube.com/watch?v=abc", PublishedAt: time.Now()},
		},
	}

	svc := fetchUC.NewService(
		srcRepo, artRepo, &stubSummarizer{}, fetcher, nil,
		fetchUC.ContentFetchConfig{Parallelism: 10, Threshold: 1500},
	)

	_, err := svc.CrawlAllSources(context.Background())
	if err == nil {
		t.Fatal("CrawlAllSources() error = nil, want error")
	}
}

// TASK-003: Multiple source with partial summarization failure test
func TestService_CrawlAllSources_PartialSummarizationFailure(t *testing.T) {
	now := time.Now()

	// Setup: 2 sources
	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed1", Active: true},
			{ID: 2, FeedURL: "https://example.com/feed2", Active: true},
		},
	}

	artRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	// Create a fetcher that returns different items per source URL
	feedData := map[string][]fetchUC.FeedItem{
		"https://example.com/feed1": {
			{Title: "S1-A1", URL: "https://example.com/s1a1", Content: "fail-this", PublishedAt: now},
			{Title: "S1-A2", URL: "https://example.com/s1a2", Content: "Content S1-A2", PublishedAt: now},
			{Title: "S1-A3", URL: "https://example.com/s1a3", Content: "Content S1-A3", PublishedAt: now},
		},
		"https://example.com/feed2": {
			{Title: "S2-A1", URL: "https://example.com/s2a1", Content: "Content S2-A1", PublishedAt: now},
			{Title: "S2-A2", URL: "https://example.com/s2a2", Content: "Content S2-A2", PublishedAt: now},
		},
	}

	fetcher := &multiSourceFetcher{
		feeds: feedData,
	}

	// Create a summarizer that fails on "fail-this" content
	summarizer := &selectiveSummarizer{
		failOn: "fail-this",
	}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	// Execute crawl
	stats, err := svc.CrawlAllSources(context.Background())
	if err != nil {
		t.Fatalf("CrawlAllSources() unexpected error: %v", err)
	}

	// Verify: All sources processed despite partial failure
	if stats.Sources != 2 {
		t.Errorf("stats.Sources = %d, want 2", stats.Sources)
	}

	if stats.FeedItems != 5 {
		t.Errorf("stats.FeedItems = %d, want 5 (3 from source1 + 2 from source2)", stats.FeedItems)
	}

	if stats.Inserted != 4 {
		t.Errorf("stats.Inserted = %d, want 4 (2 from source1 + 2 from source2)", stats.Inserted)
	}

	if stats.SummarizeError != 1 {
		t.Errorf("stats.SummarizeError = %d, want 1 (1 failure from source1)", stats.SummarizeError)
	}

	// Verify: 4 articles actually created
	if len(artRepo.articles) != 4 {
		t.Errorf("created articles = %d, want 4", len(artRepo.articles))
	}
}

// TASK-004: Database error test - verifies critical errors still stop processing
func TestService_CrawlAllSources_DatabaseError(t *testing.T) {
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	// ArticleRepo will return error on Create
	artRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
		createErr: errors.New("database connection failed"),
	}

	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{
				Title:       "Article 1",
				URL:         "https://example.com/article1",
				Content:     "Content 1",
				PublishedAt: now,
			},
		},
	}

	// Summarizer succeeds
	summarizer := &stubSummarizer{
		result: "Test summary",
	}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	// Database error should be returned (critical error)
	stats, err := svc.CrawlAllSources(context.Background())
	if err == nil {
		t.Fatal("CrawlAllSources() error = nil, want error")
	}

	// Verify error message indicates database issue
	if !errors.Is(err, artRepo.createErr) && err.Error() != "process feed items: create article with summary in repository: database connection failed" {
		t.Errorf("unexpected error message: %v", err)
	}

	// Verify summarization succeeded (error count is 0)
	if stats.SummarizeError != 0 {
		t.Errorf("stats.SummarizeError = %d, want 0 (summarization succeeded)", stats.SummarizeError)
	}

	// Verify no article was inserted
	if len(artRepo.articles) != 0 {
		t.Errorf("created articles = %d, want 0 (database error prevented insert)", len(artRepo.articles))
	}
}

// TASK-005: Context cancellation test - verifies context cancellation stops processing
func TestService_CrawlAllSources_ContextCancellation(t *testing.T) {
	now := time.Now()

	srcRepo := &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 1, FeedURL: "https://example.com/feed", Active: true},
		},
	}

	artRepo := &stubArticleRepo{
		existsMap: make(map[string]bool),
	}

	fetcher := &stubFeedFetcher{
		items: []fetchUC.FeedItem{
			{
				Title:       "Article 1",
				URL:         "https://example.com/article1",
				Content:     "Content 1",
				PublishedAt: now,
			},
			{
				Title:       "Article 2",
				URL:         "https://example.com/article2",
				Content:     "Content 2",
				PublishedAt: now,
			},
		},
	}

	// Cancel the parent context from inside the summarizer, then return
	// context.Canceled (a real shutdown while summarization is in flight).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	summarizer := &cancelingSummarizer{cancel: cancel}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	// Context cancellation should stop processing immediately
	stats, err := svc.CrawlAllSources(ctx)

	if err == nil {
		t.Fatal("CrawlAllSources() error = nil, want context.Canceled")
	}

	// Verify error is context.Canceled
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}

	// The test verifies that context cancellation is respected
	// Stats may vary depending on timing, so we just verify error was returned
	_ = stats // Stats are not deterministic with concurrent operations
}
