package fetch_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
	fetchUC "catchup-feed/internal/usecase/fetch"
	"catchup-feed/internal/usecase/notify"
)

/* ───────── モック実装 ───────── */

// mockNotifyService はnotify.Serviceのモック実装
type mockNotifyService struct {
	notifyCalled int32
	notifyError  error
}

func (m *mockNotifyService) NotifyNewArticle(ctx context.Context, article *entity.Article, source *entity.Source) error {
	atomic.AddInt32(&m.notifyCalled, 1)
	return m.notifyError
}

func (m *mockNotifyService) Shutdown(ctx context.Context) error {
	return nil
}

func (m *mockNotifyService) GetChannelHealth() []notify.ChannelHealthStatus {
	return nil
}

// stubSourceRepo はSourceRepositoryのモック実装
type stubSourceRepo struct {
	sources       []*entity.Source
	listActiveErr error
	touchErr      error
	touched       map[int64]time.Time
}

func (s *stubSourceRepo) ListActive(_ context.Context) ([]*entity.Source, error) {
	return s.sources, s.listActiveErr
}

func (s *stubSourceRepo) TouchCrawledAt(_ context.Context, id int64, t time.Time) error {
	if s.touchErr != nil {
		return s.touchErr
	}
	if s.touched == nil {
		s.touched = make(map[int64]time.Time)
	}
	s.touched[id] = t
	return nil
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

// stubArticleRepo はArticleRepositoryのモック実装
type stubArticleRepo struct {
	mu        sync.Mutex
	articles  []*entity.Article
	existsMap map[string]bool
	existsErr error
	createErr error
	nextID    int64
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

// cancelingSummarizer はcontext.Canceledエラーを返すSummarizerモック
type cancelingSummarizer struct{}

func (s *cancelingSummarizer) Summarize(_ context.Context, _ string) (string, error) {
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
		nil, // webScrapers
		&mockNotifyService{},
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

	// TouchCrawledAtが呼ばれたことを確認
	if _, ok := srcRepo.touched[1]; !ok {
		t.Errorf("TouchCrawledAt was not called for source 1")
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
		nil, // webScrapers
		&mockNotifyService{},
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
		nil, // webScrapers
		&mockNotifyService{},
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

	// 空のフィードでもTouchCrawledAtは呼ばれない（continueで処理をスキップするため）
	if _, ok := srcRepo.touched[1]; ok {
		t.Errorf("TouchCrawledAt should not be called for empty feed")
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
		nil, // webScrapers
		&mockNotifyService{},
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

	// フェッチエラーでもTouchCrawledAtは呼ばれない
	if _, ok := srcRepo.touched[1]; ok {
		t.Errorf("TouchCrawledAt should not be called when fetch fails")
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
		nil, // webScrapers
		&mockNotifyService{},
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
		nil, // webScrapers
		&mockNotifyService{},
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
		nil, // webScrapers
		&mockNotifyService{},
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
		nil, // webScrapers
		&mockNotifyService{},
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
		nil, // webScrapers
		&mockNotifyService{},
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

	// Verify: Both sources fully processed
	if len(srcRepo.touched) != 2 {
		t.Errorf("touched sources = %d, want 2 (both sources should be marked as crawled)", len(srcRepo.touched))
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
		nil, // webScrapers
		&mockNotifyService{},
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
	if !errors.Is(err, artRepo.createErr) && err.Error() != "process feed items: create article in repository: database connection failed" {
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

	// Create a summarizer that returns context.Canceled error
	summarizer := &cancelingSummarizer{}

	svc := fetchUC.NewService(
		srcRepo,
		artRepo,
		summarizer,
		fetcher,
		nil, // ContentFetcher
		nil, // webScrapers
		&mockNotifyService{},
		fetchUC.ContentFetchConfig{
			Parallelism: 10,
			Threshold:   1500,
		},
	)

	// Context cancellation should stop processing immediately
	ctx := context.Background()
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
