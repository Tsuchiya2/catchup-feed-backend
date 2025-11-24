package repository

import (
	"context"
	"time"

	"catchup-feed/internal/domain/entity"
)

type SourceRepository interface {
	Get(ctx context.Context, id int64) (*entity.Source, error)
	List(ctx context.Context) ([]*entity.Source, error)
	ListActive(ctx context.Context) ([]*entity.Source, error)
	Search(ctx context.Context, keyword string) ([]*entity.Source, error)
	Create(ctx context.Context, source *entity.Source) error
	Update(ctx context.Context, source *entity.Source) error
	Delete(ctx context.Context, id int64) error
	TouchCrawledAt(ctx context.Context, id int64, t time.Time) error
}
