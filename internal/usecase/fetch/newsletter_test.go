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

/* ───────── newsletter 用モック ───────── */

// newsletterContentFetcher is a per-URL ContentFetcher mock. It does NOT
// implement FetchHTML, so the issue-page fallback stays disabled unless the
// htmlPageFetcher wrapper below is used.
type newsletterContentFetcher struct {
	mu      sync.Mutex
	pages   map[string]string // URL → readability text (default: "content of <url>")
	errOn   map[string]bool   // URL → fetch failure
	fetched []string          // FetchContent 呼び出し記録(順序検証用)
}

func (f *newsletterContentFetcher) FetchContent(_ context.Context, url string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched = append(f.fetched, url)
	if f.errOn[url] {
		return "", errors.New("intentional fetch failure")
	}
	if c, ok := f.pages[url]; ok {
		return c, nil
	}
	return "content of " + url, nil
}

// htmlPageFetcher adds the optional HTMLFetcher capability: FetchHTML
// returns the raw issue page (リンク抽出用の生 HTML).
type htmlPageFetcher struct {
	newsletterContentFetcher
	htmlPages map[string]string
	htmlErr   error
	htmlCalls []string
}

func (f *htmlPageFetcher) FetchHTML(_ context.Context, url string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.htmlCalls = append(f.htmlCalls, url)
	if f.htmlErr != nil {
		return "", f.htmlErr
	}
	return f.htmlPages[url], nil
}

/* ───────── フィクスチャ ───────── */

const (
	nlFeedURL  = "https://weekly.example.com/rss"
	nlIssueURL = "https://weekly.example.com/issues/42"
)

// nlIssueHTML carries four candidate links (>= 3 なので号ページ fallback は
// 発火しない), one with a tracking param to prove that the INSERT and the
// dedupe key both use the normalized URL.
const nlIssueHTML = `
<h2><a href="https://alpha.dev/post1">Alpha Post</a></h2>
<h2><a href="https://beta.dev/post2?utm_source=nl">Beta Post</a></h2>
<p><a href="https://gamma.dev/post3">Gamma Post</a></p>
<p><a href="https://delta.dev/post4">Delta Post</a></p>
<p><a href="https://weekly.example.com/subscribe">Subscribe</a></p>`

var nlWantLinkURLs = []string{
	"https://alpha.dev/post1",
	"https://beta.dev/post2", // utm_source stripped
	"https://gamma.dev/post3",
	"https://delta.dev/post4",
}

func newNewsletterSourceRepo() *stubSourceRepo {
	return &stubSourceRepo{
		sources: []*entity.Source{
			{ID: 7, Name: "Some Weekly", FeedURL: nlFeedURL, Category: "dev",
				Kind: entity.SourceKindNewsletter, Active: true},
		},
	}
}

func newNewsletterService(artRepo *stubArticleRepo, cf fetchUC.ContentFetcher, sum fetchUC.Summarizer, issue fetchUC.FeedItem) fetchUC.Service {
	svc := fetchUC.NewService(
		newNewsletterSourceRepo(),
		artRepo,
		sum,
		&stubFeedFetcher{items: []fetchUC.FeedItem{issue}},
		cf,
		fetchUC.ContentFetchConfig{Parallelism: 10, Threshold: 1500},
	)
	return svc
}

// findArticleByURL returns the stored article with the given URL, or nil.
func findArticleByURL(arts []*entity.Article, url string) *entity.Article {
	for _, a := range arts {
		if a.URL == url {
			return a
		}
	}
	return nil
}

/* ───────── テストケース ───────── */

// 号 → N 記事: 各リンクが fetch → 要約 → 保存され、published_at は号の
// published を継承し、タイトルはアンカーテキスト、URL は正規化済み文字列。
// 号自体は content 空・要約なしのマーカー行として保存される。
func TestNewsletter_IssueExpandsIntoLinkedArticles(t *testing.T) {
	issuePublished := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL, Content: nlIssueHTML,
		PublishedAt: issuePublished,
	}
	artRepo := &stubArticleRepo{existsMap: map[string]bool{}}
	fetcher := &newsletterContentFetcher{}
	svc := newNewsletterService(artRepo, fetcher, &stubSummarizer{result: "日本語要約"}, issue)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(1), stats.FeedItems, "1 item = 1 号")
	assert.Equal(t, int64(4), stats.Inserted, "号マーカーは Inserted に数えない")
	assert.Equal(t, int64(0), stats.Duplicated)
	assert.Equal(t, int64(0), stats.SummarizeError)

	// リンク記事: 正規化済み URL で保存され、fetch も同じ文字列で行われる。
	assert.Equal(t, nlWantLinkURLs, fetcher.fetched, "文書順・正規化済み URL で fetch")
	require.Len(t, artRepo.articles, 5) // 4 記事 + 号マーカー

	alpha := findArticleByURL(artRepo.articles, "https://alpha.dev/post1")
	require.NotNil(t, alpha)
	assert.Equal(t, "Alpha Post", alpha.Title, "アンカーテキストがタイトル")
	assert.Equal(t, "content of https://alpha.dev/post1", alpha.Content)
	assert.Equal(t, issuePublished, alpha.PublishedAt, "published_at は号を継承")
	assert.Equal(t, int64(7), alpha.SourceID)
	require.NotNil(t, artRepo.summaries[alpha.ID], "リンク記事には要約が付く")
	assert.Equal(t, "日本語要約", artRepo.summaries[alpha.ID].Body)

	beta := findArticleByURL(artRepo.articles, "https://beta.dev/post2")
	require.NotNil(t, beta, "utm を剥いだ正規化済み URL で INSERT される")

	// 号マーカー: content 空・要約なし・published_at 継承。
	marker := findArticleByURL(artRepo.articles, nlIssueURL)
	require.NotNil(t, marker, "号マーカーが保存される")
	assert.Empty(t, marker.Content, "マーカーの content は空(DB では NULL)")
	assert.Equal(t, "Issue #42", marker.Title)
	assert.Equal(t, issuePublished, marker.PublishedAt)
	assert.Nil(t, artRepo.summaries[marker.ID], "マーカーに要約は付かない")
}

// 再クロール no-op: 号マーカーが ExistsByURLBatch に載る(existsMap)ため、
// 2回目のクロールでは号ごと Duplicated になりリンク展開は走らない。
func TestNewsletter_RecrawlIsNoOp(t *testing.T) {
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL, Content: nlIssueHTML,
		PublishedAt: time.Now(),
	}
	artRepo := &stubArticleRepo{existsMap: map[string]bool{nlIssueURL: true}}
	fetcher := &newsletterContentFetcher{}
	svc := newNewsletterService(artRepo, fetcher, &stubSummarizer{}, issue)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(1), stats.Duplicated, "号 URL の重複判定で落ちる")
	assert.Equal(t, int64(0), stats.Inserted)
	assert.Empty(t, fetcher.fetched, "リンクの fetch は一切走らない")
	assert.Empty(t, artRepo.articles)
}

// DB 既存リンクのスキップ: 他ソースが既に持つ URL は fetch 前に落ち、残りは
// 通常どおり保存される。
func TestNewsletter_ExistingLinkSkippedBeforeFetch(t *testing.T) {
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL, Content: nlIssueHTML,
		PublishedAt: time.Now(),
	}
	artRepo := &stubArticleRepo{existsMap: map[string]bool{
		"https://beta.dev/post2": true, // 正規化後の URL がキー
	}}
	fetcher := &newsletterContentFetcher{}
	svc := newNewsletterService(artRepo, fetcher, &stubSummarizer{}, issue)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(3), stats.Inserted)
	assert.Equal(t, int64(1), stats.Duplicated)
	assert.NotContains(t, fetcher.fetched, "https://beta.dev/post2", "既存リンクは fetch されない")
	assert.NotNil(t, findArticleByURL(artRepo.articles, nlIssueURL), "号マーカーは保存される")
}

// 1リンクの失敗はそのリンクのみスキップ: fetch 失敗1件・要約失敗1件が
// あっても残りは保存され、クロールはエラーにならない。部分成功なので号
// マーカーも入る。
func TestNewsletter_FailedLinksAreSkipped(t *testing.T) {
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL, Content: nlIssueHTML,
		PublishedAt: time.Now(),
	}
	artRepo := &stubArticleRepo{existsMap: map[string]bool{}}
	fetcher := &newsletterContentFetcher{
		errOn: map[string]bool{"https://alpha.dev/post1": true},
		pages: map[string]string{"https://gamma.dev/post3": "unsummarizable"},
	}
	svc := newNewsletterService(artRepo, fetcher,
		&selectiveSummarizer{failOn: "unsummarizable"}, issue)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err, "リンク失敗はクロールを止めない")

	assert.Equal(t, int64(2), stats.Inserted, "beta と delta のみ")
	assert.Equal(t, int64(1), stats.SummarizeError)
	assert.Nil(t, findArticleByURL(artRepo.articles, "https://alpha.dev/post1"))
	assert.Nil(t, findArticleByURL(artRepo.articles, "https://gamma.dev/post3"))
	assert.NotNil(t, findArticleByURL(artRepo.articles, "https://beta.dev/post2"))
	assert.NotNil(t, findArticleByURL(artRepo.articles, nlIssueURL), "部分成功なら号マーカーは入る")
}

// 全リンク失敗(要約プロバイダ全滅 §8 相当)では号マーカーを入れず、次
// サイクルで号ごと再試行させる。
func TestNewsletter_AllLinksFailedLeavesIssueForRetry(t *testing.T) {
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL, Content: nlIssueHTML,
		PublishedAt: time.Now(),
	}
	artRepo := &stubArticleRepo{existsMap: map[string]bool{}}
	fetcher := &newsletterContentFetcher{}
	svc := newNewsletterService(artRepo, fetcher,
		&stubSummarizer{err: errors.New("all providers down")}, issue)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err, "全プロバイダ全滅でもクロール自体は完走する(§8)")

	assert.Equal(t, int64(0), stats.Inserted)
	assert.Equal(t, int64(4), stats.SummarizeError)
	assert.Empty(t, artRepo.articles, "記事もマーカーも入らない → 次サイクルで再試行")
}

// バッチ既存チェック後に他ソースが同 URL を入れたレース: ON CONFLICT DO
// NOTHING 相当で握って続行し、号マーカーも入る。
func TestNewsletter_URLConflictIsToleratedAndContinues(t *testing.T) {
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL, Content: nlIssueHTML,
		PublishedAt: time.Now(),
	}
	artRepo := &stubArticleRepo{
		existsMap:    map[string]bool{},
		conflictURLs: map[string]bool{"https://beta.dev/post2": true},
	}
	fetcher := &newsletterContentFetcher{}
	svc := newNewsletterService(artRepo, fetcher, &stubSummarizer{}, issue)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err, "UNIQUE 衝突はクロールを止めない")

	assert.Equal(t, int64(3), stats.Inserted)
	assert.Equal(t, int64(1), stats.Duplicated, "衝突リンクは重複として数える")
	assert.Nil(t, findArticleByURL(artRepo.articles, "https://beta.dev/post2"))
	assert.NotNil(t, findArticleByURL(artRepo.articles, "https://delta.dev/post4"), "衝突後のリンクも処理される")
	assert.NotNil(t, findArticleByURL(artRepo.articles, nlIssueURL))
}

// 上限: NEWSLETTER_MAX_ARTICLES 相当の cap で文書順の先頭 N 件だけが処理
// される(超過分はログのみ — silent truncation 禁止はログで担保)。
func TestNewsletter_LinkCapTakesHeadOfDocumentOrder(t *testing.T) {
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL, Content: nlIssueHTML,
		PublishedAt: time.Now(),
	}
	artRepo := &stubArticleRepo{existsMap: map[string]bool{}}
	fetcher := &newsletterContentFetcher{}
	svc := newNewsletterService(artRepo, fetcher, &stubSummarizer{}, issue)
	svc.NewsletterMaxArticles = 2

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(2), stats.Inserted)
	assert.Equal(t, []string{"https://alpha.dev/post1", "https://beta.dev/post2"},
		fetcher.fetched, "文書順の先頭から cap 件のみ")
	assert.NotNil(t, findArticleByURL(artRepo.articles, nlIssueURL))
}

// RSS description がリンク貧弱(ティーザーのみ)の場合は号ページ HTML を
// 取得してリンク抽出する(HTMLFetcher fallback)。
func TestNewsletter_TeaserDescriptionFallsBackToIssuePage(t *testing.T) {
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL,
		Content:     `<p>Read this week's issue on the web!</p>`,
		PublishedAt: time.Now(),
	}
	artRepo := &stubArticleRepo{existsMap: map[string]bool{}}
	fetcher := &htmlPageFetcher{htmlPages: map[string]string{nlIssueURL: nlIssueHTML}}
	svc := newNewsletterService(artRepo, fetcher, &stubSummarizer{}, issue)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	assert.Equal(t, []string{nlIssueURL}, fetcher.htmlCalls, "号ページを HTML として取得")
	assert.Equal(t, int64(4), stats.Inserted, "号ページから抽出したリンクが展開される")
}

// 号ページ fetch が失敗しても縮退: インラインのリンクのみで処理を続ける
// (この場合 0 リンク → 記事なしで号マーカーだけ入る)。
func TestNewsletter_IssuePageFetchFailureDegrades(t *testing.T) {
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL,
		Content:     `<p>Teaser only.</p>`,
		PublishedAt: time.Now(),
	}
	artRepo := &stubArticleRepo{existsMap: map[string]bool{}}
	fetcher := &htmlPageFetcher{htmlErr: errors.New("issue page 503")}
	svc := newNewsletterService(artRepo, fetcher, &stubSummarizer{}, issue)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(0), stats.Inserted)
	require.Len(t, artRepo.articles, 1)
	assert.Equal(t, nlIssueURL, artRepo.articles[0].URL, "リンク 0 件でも号マーカーは入る")
}

// ContentFetcher が無効(nil)の場合はリンク展開できないためソースごと
// スキップし、号マーカーも入れない(有効化後に追い付ける)。
func TestNewsletter_NilContentFetcherSkipsSource(t *testing.T) {
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL, Content: nlIssueHTML,
		PublishedAt: time.Now(),
	}
	artRepo := &stubArticleRepo{existsMap: map[string]bool{}}
	svc := newNewsletterService(artRepo, nil, &stubSummarizer{}, issue)

	stats, err := svc.CrawlAllSources(context.Background())
	require.NoError(t, err)

	assert.Equal(t, int64(0), stats.Inserted)
	assert.Empty(t, artRepo.articles, "マーカーも入れない → fetcher 有効化後に処理される")
}

// ctx キャンセル(シャットダウン)だけがクロール全体を止める(B-1 の流儀)。
func TestNewsletter_ContextCancellationAbortsCrawl(t *testing.T) {
	issue := fetchUC.FeedItem{
		Title: "Issue #42", URL: nlIssueURL, Content: nlIssueHTML,
		PublishedAt: time.Now(),
	}
	artRepo := &stubArticleRepo{existsMap: map[string]bool{}}
	fetcher := &newsletterContentFetcher{}
	ctx, cancel := context.WithCancel(context.Background())
	svc := newNewsletterService(artRepo, fetcher, &cancelingSummarizer{cancel: cancel}, issue)

	_, err := svc.CrawlAllSources(ctx)
	require.Error(t, err, "ctx が死んだときだけクロールを中断する")
	assert.Nil(t, findArticleByURL(artRepo.articles, nlIssueURL), "中断した号にマーカーは入らない")
}
