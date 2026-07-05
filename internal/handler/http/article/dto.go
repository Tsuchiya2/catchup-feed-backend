// Package article provides HTTP handlers for article-related endpoints.
// It includes handlers for creating, listing, searching, updating, and deleting articles.
package article

import "time"

// DTO represents the JSON structure for article data transfer.
// Summary comes from the summaries table (empty until the crawl pipeline
// has summarized the article); crawled_at replaces the old created_at (§4).
//
// Content (go-readability の抽出全文、記事あたり数十KB) は意図的に応答へ
// 含めない: ダッシュボードはタイトル+要約しか表示せず、一覧系エンドポイント
// で全文を返すとペイロードが桁で膨らむため。Create/Update のリクエストが
// content を受けるのはパイプライン外から記事を投入する管理経路のためで、
// 応答との非対称は仕様。全文が必要になったら専用エンドポイントを検討する。
type DTO struct {
	ID          int64     `json:"id" example:"1"`
	SourceID    int64     `json:"source_id" example:"1"`
	SourceName  string    `json:"source_name,omitempty" example:"Go Blog"`
	Title       string    `json:"title" example:"Go 1.23 リリース"`
	URL         string    `json:"url" example:"https://example.com/article/1"`
	Summary     string    `json:"summary" example:"Go 1.23 がリリースされました。新機能には..."`
	PublishedAt time.Time `json:"published_at" example:"2025-10-26T10:00:00Z"`
	CrawledAt   time.Time `json:"crawled_at" example:"2025-10-26T12:00:00Z"`
}

// CreateRequest is the POST /articles body (パイプライン外から記事を投入する
// 管理経路). source_id / title / url are required.
type CreateRequest struct {
	SourceID int64  `json:"source_id" example:"1"`
	Title    string `json:"title" example:"Go 1.23 リリース"`
	URL      string `json:"url" example:"https://example.com/article/1"`
	Content  string `json:"content,omitempty" example:"記事全文..."`
	// PublishedAt is an RFC 3339 timestamp; empty means unknown.
	PublishedAt string `json:"published_at,omitempty" example:"2025-10-26T10:00:00Z"`
}

// UpdateRequest is the PUT /articles/{id} body. Every field is optional;
// omitted (null) fields keep their current value.
type UpdateRequest struct {
	SourceID *int64  `json:"source_id,omitempty" example:"1"`
	Title    *string `json:"title,omitempty" example:"Go 1.23 リリース"`
	URL      *string `json:"url,omitempty" example:"https://example.com/article/1"`
	Content  *string `json:"content,omitempty" example:"記事全文..."`
	// PublishedAt is an RFC 3339 timestamp.
	PublishedAt *string `json:"published_at,omitempty" example:"2025-10-26T10:00:00Z"`
}
