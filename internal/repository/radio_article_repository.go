package repository

import (
	"context"
	"time"
)

// RadioArticle is the radio-batch view of a summarized article (§6-1).
// It deliberately has no Content field: the script generator receives the
// summary body only, never the extracted article text (C-12 — 台本生成に
// 渡すのは公開記事の要約のみ). PublishedAt falls back to crawled_at when
// the feed did not carry a publication date.
type RadioArticle struct {
	ID          int64
	Title       string
	URL         string
	Category    string // sources.category — 台本のコーナー分け(§4)
	SourceName  string
	Summary     string // summaries.body(日本語要約)
	PublishedAt time.Time
}

// RadioArticleRepository selects the articles that feed an episode.
// It is separate from ArticleRepository so that the radio batch depends on
// exactly one query and the dashboard-facing interface stays untouched.
type RadioArticleRepository interface {
	// ListSummarizedSince returns articles whose summary was created after
	// `since` (the previous public episode's published_at — §6-1: 前回
	// public エピソード以降の要約済み記事), oldest first, up to limit.
	// The cursor is summaries.created_at, not articles.published_at, so an
	// old article that was summarized late still gets on air once.
	ListSummarizedSince(ctx context.Context, since time.Time, limit int) ([]RadioArticle, error)
}
