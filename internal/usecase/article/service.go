package article

import (
	"context"
	"fmt"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// CreateInput represents the input parameters for creating a new article.
type CreateInput struct {
	SourceID    int64
	Title       string
	URL         string
	Summary     string
	PublishedAt time.Time
}

// UpdateInput represents the input parameters for updating an existing article.
// Fields with nil values will not be updated.
type UpdateInput struct {
	ID          int64
	SourceID    *int64
	Title       *string
	URL         *string
	Summary     *string
	PublishedAt *time.Time
}

// Service provides article management use cases.
// It handles business logic for article operations and delegates persistence to the repository.
type Service struct {
	Repo repository.ArticleRepository
}

// List retrieves all articles from the repository.
// Returns an error if the repository operation fails.
func (s *Service) List(ctx context.Context) ([]*entity.Article, error) {
	articles, err := s.Repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list articles: %w", err)
	}
	return articles, nil
}

// Get retrieves a single article by its ID.
// Returns ErrInvalidArticleID if the ID is not positive.
// Returns ErrArticleNotFound if the article does not exist.
func (s *Service) Get(ctx context.Context, id int64) (*entity.Article, error) {
	if id <= 0 {
		return nil, ErrInvalidArticleID
	}

	article, err := s.Repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get article: %w", err)
	}
	if article == nil {
		return nil, ErrArticleNotFound
	}
	return article, nil
}

// Search finds articles matching the given keyword.
// The search is performed against article titles and summaries.
// Returns an error if the repository operation fails.
func (s *Service) Search(ctx context.Context, kw string) ([]*entity.Article, error) {
	articles, err := s.Repo.Search(ctx, kw)
	if err != nil {
		return nil, fmt.Errorf("search articles: %w", err)
	}
	return articles, nil
}

// Create creates a new article with the provided input.
// It validates the input data including URL format before creating the article.
// Returns a ValidationError if any input field is invalid.
func (s *Service) Create(ctx context.Context, in CreateInput) error {
	if in.SourceID <= 0 {
		return &entity.ValidationError{Field: "sourceID", Message: "must be positive"}
	}
	if in.Title == "" {
		return &entity.ValidationError{Field: "title", Message: "is required"}
	}
	if in.URL == "" {
		return &entity.ValidationError{Field: "url", Message: "is required"}
	}

	// URL形式検証
	if err := entity.ValidateURL(in.URL); err != nil {
		return fmt.Errorf("validate URL: %w", err)
	}

	art := &entity.Article{
		SourceID:    in.SourceID,
		Title:       in.Title,
		URL:         in.URL,
		Summary:     in.Summary,
		PublishedAt: in.PublishedAt,
		CreatedAt:   time.Now(),
	}

	if err := s.Repo.Create(ctx, art); err != nil {
		return fmt.Errorf("create article: %w", err)
	}
	return nil
}

// Update modifies an existing article with the provided input.
// Only non-nil fields in the input will be updated.
// Returns ErrInvalidArticleID if the ID is not positive.
// Returns ErrArticleNotFound if the article does not exist.
// Returns a ValidationError if any updated field is invalid.
func (s *Service) Update(ctx context.Context, in UpdateInput) error {
	if in.ID <= 0 {
		return ErrInvalidArticleID
	}

	art, err := s.Repo.Get(ctx, in.ID)
	if err != nil {
		return fmt.Errorf("get article: %w", err)
	}
	if art == nil {
		return ErrArticleNotFound
	}

	if in.SourceID != nil {
		if *in.SourceID <= 0 {
			return &entity.ValidationError{Field: "sourceID", Message: "must be positive"}
		}
		art.SourceID = *in.SourceID
	}
	if in.Title != nil {
		if *in.Title == "" {
			return &entity.ValidationError{Field: "title", Message: "cannot be empty"}
		}
		art.Title = *in.Title
	}
	if in.URL != nil {
		// URL形式検証
		if err := entity.ValidateURL(*in.URL); err != nil {
			return fmt.Errorf("validate URL: %w", err)
		}
		art.URL = *in.URL
	}
	if in.Summary != nil {
		art.Summary = *in.Summary
	}
	if in.PublishedAt != nil {
		art.PublishedAt = *in.PublishedAt
	}

	if err := s.Repo.Update(ctx, art); err != nil {
		return fmt.Errorf("update article: %w", err)
	}
	return nil
}

// Delete removes an article by its ID.
// Returns ErrInvalidArticleID if the ID is not positive.
// Returns an error if the repository operation fails.
func (s *Service) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrInvalidArticleID
	}

	if err := s.Repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete article: %w", err)
	}
	return nil
}
