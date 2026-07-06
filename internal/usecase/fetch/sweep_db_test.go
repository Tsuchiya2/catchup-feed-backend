package fetch_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/infra/db"
	fetchUC "catchup-feed/internal/usecase/fetch"
)

// recordingSummarizer records which contents it was asked to summarize, so
// assertions can prove an article was (or was not) re-summarized even when
// the shared test database contains rows from other runs.
type recordingSummarizer struct {
	mu     sync.Mutex
	seen   []string
	failOn string
}

func (s *recordingSummarizer) Summarize(ctx context.Context, text string) (string, error) {
	sum, _, err := s.SummarizeWithProvider(ctx, text)
	return sum, err
}

func (s *recordingSummarizer) SummarizeWithProvider(_ context.Context, text string) (string, string, error) {
	s.mu.Lock()
	s.seen = append(s.seen, text)
	s.mu.Unlock()
	if s.failOn != "" && text == s.failOn {
		return "", "", fmt.Errorf("all providers failed (simulated free-tier outage)")
	}
	return "要約: " + text, "gemini", nil
}

func (s *recordingSummarizer) sawContent(text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.seen {
		if v == text {
			return true
		}
	}
	return false
}

// TestSweepUnsummarized_RealPostgres proves the §5.2b sweep contract against
// the real schema: an article whose content is filled in after insert (the
// transcribe path) is summarized by the next sweep cycle; content-NULL
// articles and already-summarized articles are never candidates; a
// summarization failure leaves the article in place for the following
// cycle. Skipped unless TEST_DATABASE_URL is set (same convention as
// internal/infra/db).
func TestSweepUnsummarized_RealPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping real-postgres sweep test")
	}
	conn, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	require.NoError(t, conn.Ping())
	require.NoError(t, db.MigrateUp(conn))

	ctx := context.Background()
	run := time.Now().UnixNano() // unique URLs: the DB may be shared across runs

	// One podcast source and three articles in the three §5.2b states.
	var sourceID int64
	require.NoError(t, conn.QueryRowContext(ctx, `
INSERT INTO sources (name, feed_url, category, kind)
VALUES ($1, $2, 'tech', 'podcast') RETURNING id`,
		fmt.Sprintf("sweep-test-%d", run),
		fmt.Sprintf("https://sweep.test/%d/feed", run),
	).Scan(&sourceID))

	insertArticle := func(name string, content any) int64 {
		var id int64
		require.NoError(t, conn.QueryRowContext(ctx, `
INSERT INTO articles (source_id, url, title, content)
VALUES ($1, $2, $3, $4) RETURNING id`,
			sourceID, fmt.Sprintf("https://sweep.test/%d/%s", run, name), name, content,
		).Scan(&id))
		return id
	}
	transcribedContent := fmt.Sprintf("transcribed text %d", run)
	summarizedContent := fmt.Sprintf("already summarized text %d", run)
	failingContent := fmt.Sprintf("failing text %d", run)

	transcribedID := insertArticle("transcribed", transcribedContent) // content 有り, summary 無し → 対象
	pendingID := insertArticle("pending", nil)                        // content NULL → 対象外
	summarizedID := insertArticle("summarized", summarizedContent)    // summary 済み → 対象外
	failingID := insertArticle("failing", failingContent)             // 要約失敗 → 次サイクルへ残る

	_, err = conn.ExecContext(ctx, `
INSERT INTO summaries (article_id, body, provider) VALUES ($1, 'existing body', 'groq')`,
		summarizedID)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = conn.Exec(`DELETE FROM summaries WHERE article_id IN (SELECT id FROM articles WHERE source_id = $1)`, sourceID)
		_, _ = conn.Exec(`DELETE FROM articles WHERE source_id = $1`, sourceID)
		_, _ = conn.Exec(`DELETE FROM sources WHERE id = $1`, sourceID)
	})

	summaryRepo := pgRepo.NewSummaryRepo(conn)
	newService := func(sum fetchUC.Summarizer) fetchUC.Service {
		svc := fetchUC.NewService(
			pgRepo.NewSourceRepo(conn),
			pgRepo.NewArticleRepo(conn),
			sum,
			&stubFeedFetcher{},
			nil,
			fetchUC.ContentFetchConfig{Parallelism: 1, Threshold: 1500},
		)
		svc.SummaryRepo = summaryRepo
		return svc
	}

	// ── Cycle 1: transcript is picked up, failure stays, others untouched ──
	sum1 := &recordingSummarizer{failOn: failingContent}
	svc1 := newService(sum1)
	stats, err := svc1.SweepUnsummarized(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, stats.Summarized, int64(1))
	assert.GreaterOrEqual(t, stats.Failed, int64(1))

	got, err := summaryRepo.GetByArticleID(ctx, transcribedID)
	require.NoError(t, err)
	require.NotNil(t, got, "content が後から埋まった記事は次サイクルで要約される")
	assert.Equal(t, "要約: "+transcribedContent, got.Body)
	assert.Equal(t, "gemini", got.Provider, "provider が永続化される")

	pendingSum, err := summaryRepo.GetByArticleID(ctx, pendingID)
	require.NoError(t, err)
	assert.Nil(t, pendingSum, "content NULL の記事は対象外")

	existing, err := summaryRepo.GetByArticleID(ctx, summarizedID)
	require.NoError(t, err)
	require.NotNil(t, existing)
	assert.Equal(t, "existing body", existing.Body, "summary 済みの記事は再要約されない")
	assert.False(t, sum1.sawContent(summarizedContent), "summarizer は summary 済み記事の content を受け取らない")

	failedSum, err := summaryRepo.GetByArticleID(ctx, failingID)
	require.NoError(t, err)
	assert.Nil(t, failedSum, "要約失敗した記事はそのまま残る(jobs テーブルは使わない)")

	// ── Cycle 2: the failed article is retried and recovered (縮退許容) ──
	sum2 := &recordingSummarizer{} // providers back up
	svc2 := newService(sum2)
	_, err = svc2.SweepUnsummarized(ctx)
	require.NoError(t, err)

	recovered, err := summaryRepo.GetByArticleID(ctx, failingID)
	require.NoError(t, err)
	require.NotNil(t, recovered, "前サイクルの失敗は次サイクルで自動回収される")
	assert.Equal(t, "要約: "+failingContent, recovered.Body)

	assert.False(t, sum2.sawContent(transcribedContent),
		"前サイクルで要約済みになった記事は再要約されない")
}
