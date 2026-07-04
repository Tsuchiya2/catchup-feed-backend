package repository

import (
	"context"

	"catchup-feed/internal/domain/entity"
)

// SourceSearchFilters contains optional filters for source search
type SourceSearchFilters struct {
	Category *string // Optional: Filter by category (台本のコーナー分け単位)
	Active   *bool   // Optional: Filter by active status
}

type SourceRepository interface {
	Get(ctx context.Context, id int64) (*entity.Source, error)
	List(ctx context.Context) ([]*entity.Source, error)
	ListActive(ctx context.Context) ([]*entity.Source, error)
	Search(ctx context.Context, keyword string) ([]*entity.Source, error)
	SearchWithFilters(ctx context.Context, keywords []string, filters SourceSearchFilters) ([]*entity.Source, error)
	Create(ctx context.Context, source *entity.Source) error
	Update(ctx context.Context, source *entity.Source) error
	Delete(ctx context.Context, id int64) error
}
