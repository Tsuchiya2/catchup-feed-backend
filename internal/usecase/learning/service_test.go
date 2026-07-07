package learning_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	learncore "catchup-feed/internal/learning"
	"catchup-feed/internal/repository"
	learnUC "catchup-feed/internal/usecase/learning"
)

// stubAdminRepo records the arguments of the last call and returns the
// configured values. Methods not under test fail loudly if reached.
type stubAdminRepo struct {
	gradeLogID    int64
	gradeResult   string
	gradeGradedOn time.Time
	gradeLadder   []int
	gradeOutcome  repository.GradeOutcome
	gradeErr      error
	gradeCalls    int

	listRetired *bool
	listErr     error

	retireErr error

	activateErr   error
	deactivateErr error
	book          repository.ReviewBook
}

func (s *stubAdminRepo) ListPendingReviews(context.Context) ([]repository.PendingReview, error) {
	return nil, nil
}

func (s *stubAdminRepo) GradeReview(_ context.Context, logID int64, result string, gradedOn time.Time, ladder []int) (repository.GradeOutcome, error) {
	s.gradeCalls++
	s.gradeLogID, s.gradeResult, s.gradeGradedOn, s.gradeLadder = logID, result, gradedOn, ladder
	return s.gradeOutcome, s.gradeErr
}

func (s *stubAdminRepo) ListItems(_ context.Context, retired bool) ([]repository.LearningItemSummary, error) {
	s.listRetired = &retired
	return nil, s.listErr
}

func (s *stubAdminRepo) RetireItem(context.Context, int64) (time.Time, error) {
	return time.Time{}, s.retireErr
}

func (s *stubAdminRepo) ListBooks(context.Context) ([]repository.ReviewBook, error) {
	return nil, nil
}

func (s *stubAdminRepo) ActivateBook(context.Context, int64) (repository.ReviewBook, error) {
	return s.book, s.activateErr
}

func (s *stubAdminRepo) DeactivateBook(context.Context, int64) (repository.ReviewBook, error) {
	return s.book, s.deactivateErr
}

func TestService_Grade_ResultValidation(t *testing.T) {
	tests := []struct {
		name      string
		result    string
		wantErr   error
		wantCalls int
	}{
		{name: "good passes", result: "good", wantCalls: 1},
		{name: "fuzzy passes", result: "fuzzy", wantCalls: 1},
		{name: "forgot passes", result: "forgot", wantCalls: 1},
		// 'auto' は radio バッチ専用語彙(D-17)— 手動採点 API からは
		// 注入できない。
		{name: "auto rejected", result: "auto", wantErr: learnUC.ErrInvalidResult},
		{name: "empty rejected", result: "", wantErr: learnUC.ErrInvalidResult},
		{name: "garbage rejected", result: "great", wantErr: learnUC.ErrInvalidResult},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubAdminRepo{}
			svc := learnUC.Service{Repo: repo, Ladder: []int{1, 7, 30}}
			_, err := svc.Grade(context.Background(), 5, tt.result)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.result, repo.gradeResult)
			}
			assert.Equal(t, tt.wantCalls, repo.gradeCalls,
				"invalid results must be rejected before any repository call")
		})
	}
}

// TestService_Grade_BroadcastDayAndLadder pins §6.1/§12-10: the due 起点
// passed to the repository is the JST grading day derived via
// learning.BroadcastDay — 23:30 JST(= 14:30 UTC)はその JST 日、UTC 日付
// 比較なら前日に化ける境界 — and the ladder is the service's D-18 config.
func TestService_Grade_BroadcastDayAndLadder(t *testing.T) {
	repo := &stubAdminRepo{}
	ladder := []int{2, 5}
	// 2026-07-07 23:30 JST = 14:30 UTC → 採点日は JST の 7/07。
	now := time.Date(2026, 7, 7, 14, 30, 0, 0, time.UTC)
	svc := learnUC.Service{Repo: repo, Ladder: ladder, Now: func() time.Time { return now }}

	_, err := svc.Grade(context.Background(), 42, "good")
	require.NoError(t, err)
	assert.EqualValues(t, 42, repo.gradeLogID)
	assert.Equal(t, learncore.BroadcastDay(now), repo.gradeGradedOn)
	assert.Equal(t, "2026-07-07", learncore.FormatDay(repo.gradeGradedOn))
	assert.Equal(t, ladder, repo.gradeLadder)
}

func TestService_Grade_ErrorMapping(t *testing.T) {
	tests := []struct {
		name    string
		repoErr error
		wantErr error
	}{
		{"absent log maps to 404 material", repository.ErrReviewLogNotFound, learnUC.ErrReviewNotFound},
		{"resolved log maps to 409 material", repository.ErrReviewLogGraded, learnUC.ErrReviewAlreadyGraded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubAdminRepo{gradeErr: tt.repoErr}
			svc := learnUC.Service{Repo: repo, Ladder: []int{1, 7, 30}}
			_, err := svc.Grade(context.Background(), 5, "good")
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}

	t.Run("unexpected errors pass through wrapped", func(t *testing.T) {
		boom := errors.New("db down")
		repo := &stubAdminRepo{gradeErr: boom}
		svc := learnUC.Service{Repo: repo, Ladder: []int{1, 7, 30}}
		_, err := svc.Grade(context.Background(), 5, "good")
		assert.ErrorIs(t, err, boom)
		assert.NotErrorIs(t, err, learnUC.ErrReviewAlreadyGraded)
	})
}

func TestService_ListItems_StatusFilter(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		wantRetired *bool
		wantErr     error
	}{
		{name: "default is active", status: "", wantRetired: ptr(false)},
		{name: "active", status: "active", wantRetired: ptr(false)},
		{name: "retired", status: "retired", wantRetired: ptr(true)},
		{name: "unknown rejected", status: "archived", wantErr: learnUC.ErrInvalidStatus},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubAdminRepo{}
			svc := learnUC.Service{Repo: repo, Ladder: []int{1, 7, 30}}
			_, err := svc.ListItems(context.Background(), tt.status)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, repo.listRetired, "invalid status must not reach the repository")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, repo.listRetired)
			assert.Equal(t, *tt.wantRetired, *repo.listRetired)
		})
	}
}

func TestService_NotFoundMapping(t *testing.T) {
	ctx := context.Background()

	t.Run("retire", func(t *testing.T) {
		svc := learnUC.Service{Repo: &stubAdminRepo{retireErr: repository.ErrLearningItemNotFound}}
		_, err := svc.RetireItem(ctx, 9)
		assert.ErrorIs(t, err, learnUC.ErrItemNotFound)
	})
	t.Run("activate", func(t *testing.T) {
		svc := learnUC.Service{Repo: &stubAdminRepo{activateErr: repository.ErrBookNotFound}}
		_, err := svc.ActivateBook(ctx, 9)
		assert.ErrorIs(t, err, learnUC.ErrBookNotFound)
	})
	t.Run("deactivate", func(t *testing.T) {
		svc := learnUC.Service{Repo: &stubAdminRepo{deactivateErr: repository.ErrBookNotFound}}
		_, err := svc.DeactivateBook(ctx, 9)
		assert.ErrorIs(t, err, learnUC.ErrBookNotFound)
	})
}

func ptr[T any](v T) *T { return &v }
