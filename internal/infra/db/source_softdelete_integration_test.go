package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	pgRepo "catchup-feed/internal/infra/adapter/persistence/postgres"
	"catchup-feed/internal/repository"
)

// TestSourceSoftDelete_RealPostgres proves the two production bugs fixed by
// the sources soft-delete against a real PostgreSQL:
//
//  1. 削除 500 バグ: a source with crawled articles could not be deleted
//     (DELETE FROM sources → articles_source_id_fkey violation → 500).
//     Soft delete keeps the row, so deletion succeeds and the articles stay.
//  2. UNIQUE(feed_url) エッジケース: re-registering a deleted URL must
//     resurrect the soft-deleted row (same id) instead of violating the
//     UNIQUE constraint; a conflict with a live row stays an error.
//
// Skipped unless TEST_DATABASE_URL is set (migrate_integration_test.go の
// 流儀).
func TestSourceSoftDelete_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))

	ctx := context.Background()
	sources := pgRepo.NewSourceRepo(conn)

	nano := time.Now().UnixNano()
	feedURL := fmt.Sprintf("https://softdelete.example.com/%d.rss", nano)

	src := &entity.Source{
		Name: "softdelete-test", FeedURL: feedURL,
		Category: "dev", Lang: "en", Kind: "rss", Active: true,
	}
	require.NoError(t, sources.Create(ctx, src))
	t.Cleanup(func() {
		// 物理削除で後片付け(記事 → ソースの順で FK を守る)。
		_, _ = conn.Exec(`DELETE FROM articles WHERE source_id = $1`, src.ID)
		_, _ = conn.Exec(`DELETE FROM sources WHERE id = $1`, src.ID)
	})

	// クロール済み記事を持たせる — 旧実装で 500 になっていた前提条件。
	var articleID int64
	require.NoError(t, conn.QueryRow(
		`INSERT INTO articles (source_id, url, title) VALUES ($1, $2, 'ep') RETURNING id`,
		src.ID, fmt.Sprintf("https://softdelete.example.com/%d/a1", nano)).Scan(&articleID))

	// (1) 記事を持つソースの削除が FK 違反にならず成功する。
	require.NoError(t, sources.Delete(ctx, src.ID),
		"deleting a source with articles must not hit articles_source_id_fkey")

	// 記事は残る(振り返り資産)。
	var articleCount int
	require.NoError(t, conn.QueryRow(
		`SELECT count(*) FROM articles WHERE source_id = $1`, src.ID).Scan(&articleCount))
	assert.Equal(t, 1, articleCount, "articles must survive source deletion")

	// 行は残るが deleted_at が立ち、active も落ちる。
	var deletedAtSet, active bool
	require.NoError(t, conn.QueryRow(
		`SELECT deleted_at IS NOT NULL, active FROM sources WHERE id = $1`, src.ID).
		Scan(&deletedAtSet, &active))
	assert.True(t, deletedAtSet, "row is kept with deleted_at set")
	assert.False(t, active, "soft delete also drops active")

	// 全読み取り経路から消える。
	got, err := sources.Get(ctx, src.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "Get must not return a soft-deleted source")

	containsID := func(list []*entity.Source) bool {
		for _, s := range list {
			if s.ID == src.ID {
				return true
			}
		}
		return false
	}
	list, err := sources.List(ctx)
	require.NoError(t, err)
	assert.False(t, containsID(list), "List must hide soft-deleted sources")

	activeList, err := sources.ListActive(ctx)
	require.NoError(t, err)
	assert.False(t, containsID(activeList), "ListActive (crawler) must hide soft-deleted sources")

	found, err := sources.Search(ctx, "softdelete-test")
	require.NoError(t, err)
	assert.False(t, containsID(found), "Search must hide soft-deleted sources")

	filtered, err := sources.SearchWithFilters(ctx, []string{"softdelete"}, repository.SourceSearchFilters{})
	require.NoError(t, err)
	assert.False(t, containsID(filtered), "SearchWithFilters must hide soft-deleted sources")

	// 二重削除は not found 扱い(RowsAffected 0)。
	assert.Error(t, sources.Delete(ctx, src.ID), "double delete must not succeed silently")

	// (2) 削除済み feed_url の再登録 → 同じ行が復活し、値は上書きされる。
	resurrected := &entity.Source{
		Name: "softdelete-test (再登録)", FeedURL: feedURL,
		Category: "ai", Lang: "ja", Kind: "rss", Active: true,
	}
	require.NoError(t, sources.Create(ctx, resurrected),
		"re-registering a deleted feed_url must not violate sources_feed_url_key")
	assert.Equal(t, src.ID, resurrected.ID, "resurrect reuses the original row (articles stay attached)")

	revived, err := sources.Get(ctx, src.ID)
	require.NoError(t, err)
	require.NotNil(t, revived, "resurrected source is visible again")
	assert.Equal(t, "softdelete-test (再登録)", revived.Name)
	assert.Equal(t, "ai", revived.Category)
	assert.Equal(t, "ja", revived.Lang)
	assert.True(t, revived.Active)

	// live 行との重複は復活ではなくエラー。
	err = sources.Create(ctx, &entity.Source{
		Name: "dup", FeedURL: feedURL, Category: "dev",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, repository.ErrDuplicateFeedURL)
}

// TestSeedOnlyWhenEmpty_RealPostgres proves the restart behaviour that
// caused the resurrection bug (Pi 実機で確認: 再起動のたびに削除済み
// ソースが36行復活): once the sources table has any row, MigrateUp must
// not re-run the seed — a deleted row stays deleted across restarts.
func TestSeedOnlyWhenEmpty_RealPostgres(t *testing.T) {
	conn := openTestDB(t)
	require.NoError(t, MigrateUp(conn))

	ctx := context.Background()
	sources := pgRepo.NewSourceRepo(conn)

	// 前提: 初回 MigrateUp(空テーブル)でシードが入っている。
	var before int
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM sources`).Scan(&before))
	require.Greater(t, before, 0, "fresh database must have been seeded by the first MigrateUp")

	// 再起動相当の MigrateUp — シードは再投入されない。
	require.NoError(t, MigrateUp(conn))
	var after int
	require.NoError(t, conn.QueryRow(`SELECT count(*) FROM sources`).Scan(&after))
	assert.Equal(t, before, after, "restart MigrateUp must not re-insert seed rows")

	// 論理削除したソースも再起動(MigrateUp)で復活しない。
	nano := time.Now().UnixNano()
	src := &entity.Source{
		Name: "seed-guard-test", FeedURL: fmt.Sprintf("https://seedguard.example.com/%d.rss", nano),
		Category: "dev", Active: true,
	}
	require.NoError(t, sources.Create(ctx, src))
	t.Cleanup(func() { _, _ = conn.Exec(`DELETE FROM sources WHERE id = $1`, src.ID) })
	require.NoError(t, sources.Delete(ctx, src.ID))

	require.NoError(t, MigrateUp(conn), "restart after a dashboard delete")

	got, err := sources.Get(ctx, src.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "a deleted source must stay deleted across restarts")

	var deletedAtSet bool
	require.NoError(t, conn.QueryRow(
		`SELECT deleted_at IS NOT NULL FROM sources WHERE id = $1`, src.ID).Scan(&deletedAtSet))
	assert.True(t, deletedAtSet, "MigrateUp must not clear deleted_at")
}
