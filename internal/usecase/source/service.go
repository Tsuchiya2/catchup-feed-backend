package source

import (
	"context"
	"fmt"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

// CreateInput represents the input parameters for creating a new source.
type CreateInput struct {
	Name    string
	FeedURL string
}

// UpdateInput represents the input parameters for updating an existing source.
// Empty string fields and nil Active field will not be updated.
type UpdateInput struct {
	ID      int64
	Name    string
	FeedURL string
	Active  *bool
}

// Service provides source management use cases.
// It handles business logic for source operations and delegates persistence to the repository.
type Service struct {
	Repo repository.SourceRepository
}

// List retrieves all sources from the repository.
// Returns an error if the repository operation fails.
func (s *Service) List(ctx context.Context) ([]*entity.Source, error) {
	sources, err := s.Repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	return sources, nil
}

// Search finds sources matching the given keyword.
// The search is performed against source names.
// Returns an error if the repository operation fails.
func (s *Service) Search(ctx context.Context, keyword string) ([]*entity.Source, error) {
	sources, err := s.Repo.Search(ctx, keyword)
	if err != nil {
		return nil, fmt.Errorf("search sources: %w", err)
	}
	return sources, nil
}

// Create creates a new source with the provided input.
// It validates the input data including feed URL format before creating the source.
// Returns a ValidationError if any input field is invalid.
func (s *Service) Create(ctx context.Context, in CreateInput) error {
	if in.Name == "" {
		return &entity.ValidationError{Field: "name", Message: "is required"}
	}
	if in.FeedURL == "" {
		return &entity.ValidationError{Field: "feedURL", Message: "is required"}
	}

	// URL形式検証
	if err := entity.ValidateURL(in.FeedURL); err != nil {
		return fmt.Errorf("validate feed URL: %w", err)
	}

	src := &entity.Source{
		Name:          in.Name,
		FeedURL:       in.FeedURL,
		LastCrawledAt: nil,
		Active:        true,
	}

	if err := s.Repo.Create(ctx, src); err != nil {
		return fmt.Errorf("create source: %w", err)
	}
	return nil
}

// Update modifies an existing source with the provided input.
// Empty string fields and nil Active field will not be updated.
// Returns ErrSourceNotFound if the source does not exist.
// Returns a ValidationError if any updated field is invalid.
func (s *Service) Update(ctx context.Context, in UpdateInput) error {
	if in.ID <= 0 {
		return &entity.ValidationError{Field: "id", Message: "must be positive"}
	}

	src, err := s.Repo.Get(ctx, in.ID)
	if err != nil {
		return fmt.Errorf("get source: %w", err)
	}
	if src == nil {
		return ErrSourceNotFound
	}

	if in.Name != "" {
		src.Name = in.Name
	}
	if in.FeedURL != "" {
		// URL形式検証
		if err := entity.ValidateURL(in.FeedURL); err != nil {
			return fmt.Errorf("validate feed URL: %w", err)
		}
		src.FeedURL = in.FeedURL
	}
	if in.Active != nil {
		src.Active = *in.Active
	}

	if err := s.Repo.Update(ctx, src); err != nil {
		return fmt.Errorf("update source: %w", err)
	}
	return nil
}

// Delete removes a source by its ID.
// Returns a ValidationError if the ID is not positive.
// Returns an error if the repository operation fails.
func (s *Service) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return &entity.ValidationError{Field: "id", Message: "must be positive"}
	}

	if err := s.Repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete source: %w", err)
	}
	return nil
}
