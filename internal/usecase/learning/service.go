package learning

import (
	"context"
	"errors"
	"fmt"
	"time"

	learncore "catchup-feed/internal/learning"
	"catchup-feed/internal/repository"
)

// Item list status filter values (?status=, §8.1). The zero value defaults
// to active.
const (
	StatusActive  = "active"
	StatusRetired = "retired"
)

// Service provides the learning admin use cases (§8.1). Ladder is the D-18
// interval ladder (learning.Config.Ladder — LoadConfig guarantees it
// non-empty); Now is injectable for tests and defaults to time.Now.
type Service struct {
	Repo   repository.LearningAdminRepository
	Ladder []int
	Now    func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// PendingReviews returns the grading queue, oldest asking first. An empty
// slice is the happy path (§8.2: 「今日は採点するものがありません」).
func (s *Service) PendingReviews(ctx context.Context) ([]repository.PendingReview, error) {
	pending, err := s.Repo.ListPendingReviews(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending reviews: %w", err)
	}
	return pending, nil
}

// Grade applies one manual grade (§8.1 一発確定). The due 起点 is the JST
// grading day (§6.1: 採点日), derived here — the single BroadcastDay
// choke point (§12-10) — and the transition itself runs inside the
// repository transaction via learning.Transition.
func (s *Service) Grade(ctx context.Context, logID int64, result string) (repository.GradeOutcome, error) {
	switch result {
	case learncore.ResultGood, learncore.ResultFuzzy, learncore.ResultForgot:
	default:
		return repository.GradeOutcome{}, ErrInvalidResult
	}
	gradedOn := learncore.BroadcastDay(s.now())
	outcome, err := s.Repo.GradeReview(ctx, logID, result, gradedOn, s.Ladder)
	switch {
	case errors.Is(err, repository.ErrReviewLogNotFound):
		return repository.GradeOutcome{}, ErrReviewNotFound
	case errors.Is(err, repository.ErrReviewLogGraded):
		return repository.GradeOutcome{}, ErrReviewAlreadyGraded
	case err != nil:
		return repository.GradeOutcome{}, fmt.Errorf("grade review %d: %w", logID, err)
	}
	return outcome, nil
}

// ListItems returns the tracker rows for ?status=active|retired (§8.1).
// An empty status defaults to active.
func (s *Service) ListItems(ctx context.Context, status string) ([]repository.LearningItemSummary, error) {
	var retired bool
	switch status {
	case StatusActive, "":
		retired = false
	case StatusRetired:
		retired = true
	default:
		return nil, ErrInvalidStatus
	}
	items, err := s.Repo.ListItems(ctx, retired)
	if err != nil {
		return nil, fmt.Errorf("list learning items: %w", err)
	}
	return items, nil
}

// RetireItem archives an item manually (§8.1, 冪等).
func (s *Service) RetireItem(ctx context.Context, itemID int64) (time.Time, error) {
	retiredAt, err := s.Repo.RetireItem(ctx, itemID)
	switch {
	case errors.Is(err, repository.ErrLearningItemNotFound):
		return time.Time{}, ErrItemNotFound
	case err != nil:
		return time.Time{}, fmt.Errorf("retire item %d: %w", itemID, err)
	}
	return retiredAt, nil
}

// ListBooks returns the D-20 book management list.
func (s *Service) ListBooks(ctx context.Context) ([]repository.ReviewBook, error) {
	books, err := s.Repo.ListBooks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	return books, nil
}

// ActivateBook makes the book the (single) active review target,
// demoting any previous active book in the same transaction (D-20 入れ替え).
func (s *Service) ActivateBook(ctx context.Context, bookID int64) (repository.ReviewBook, error) {
	book, err := s.Repo.ActivateBook(ctx, bookID)
	switch {
	case errors.Is(err, repository.ErrBookNotFound):
		return repository.ReviewBook{}, ErrBookNotFound
	case err != nil:
		return repository.ReviewBook{}, fmt.Errorf("activate book %d: %w", bookID, err)
	}
	return book, nil
}

// DeactivateBook pauses the book (D-20: status→idle、カーソル保持, 冪等).
func (s *Service) DeactivateBook(ctx context.Context, bookID int64) (repository.ReviewBook, error) {
	book, err := s.Repo.DeactivateBook(ctx, bookID)
	switch {
	case errors.Is(err, repository.ErrBookNotFound):
		return repository.ReviewBook{}, ErrBookNotFound
	case err != nil:
		return repository.ReviewBook{}, fmt.Errorf("deactivate book %d: %w", bookID, err)
	}
	return book, nil
}
