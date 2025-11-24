package repository

import (
	"context"

	"catchup-feed/internal/domain/entity"
)

type ArticleRepository interface {
	List(ctx context.Context) ([]*entity.Article, error)
	Get(ctx context.Context, id int64) (*entity.Article, error)
	Search(ctx context.Context, keyword string) ([]*entity.Article, error)
	Create(ctx context.Context, article *entity.Article) error
	Update(ctx context.Context, article *entity.Article) error
	Delete(ctx context.Context, id int64) error
	ExistsByURL(ctx context.Context, url string) (bool, error)
	// ExistsByURLBatch はバッチでURL存在チェックを行い、N+1問題を解消する
	ExistsByURLBatch(ctx context.Context, urls []string) (map[string]bool, error)
}
