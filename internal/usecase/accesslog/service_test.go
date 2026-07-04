package accesslog_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	alUC "catchup-feed/internal/usecase/accesslog"
)

type stubLogRepo struct {
	// captured arguments
	subscriberID *int64
	limit        int
	since7d      time.Time
	since30d     time.Time

	records   []*entity.FeedAccessRecord
	summaries []*entity.SubscriberAccessSummary
}

func (s *stubLogRepo) Insert(context.Context, *entity.FeedAccessLog) error { return nil }

func (s *stubLogRepo) ListRecords(_ context.Context, subscriberID *int64, limit int) ([]*entity.FeedAccessRecord, error) {
	s.subscriberID = subscriberID
	s.limit = limit
	return s.records, nil
}

func (s *stubLogRepo) SummarizeBySubscriber(_ context.Context, since7d, since30d time.Time) ([]*entity.SubscriberAccessSummary, error) {
	s.since7d = since7d
	s.since30d = since30d
	return s.summaries, nil
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
}

func TestService_List_LimitClamping(t *testing.T) {
	subscriberID := int64(3)

	tests := []struct {
		name         string
		subscriberID *int64
		limit        int
		wantLimit    int
	}{
		{name: "zero falls back to default", limit: 0, wantLimit: alUC.DefaultLimit},
		{name: "negative falls back to default", limit: -5, wantLimit: alUC.DefaultLimit},
		{name: "oversized is capped", limit: 100000, wantLimit: alUC.MaxLimit},
		{name: "explicit limit and filter pass through", subscriberID: &subscriberID, limit: 25, wantLimit: 25},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubLogRepo{}
			svc := alUC.Service{Logs: repo, Now: fixedNow}

			_, err := svc.List(context.Background(), tt.subscriberID, tt.limit)
			require.NoError(t, err)
			assert.Equal(t, tt.wantLimit, repo.limit)
			assert.Equal(t, tt.subscriberID, repo.subscriberID)
		})
	}
}

func TestService_Summarize(t *testing.T) {
	now := fixedNow()
	threeDaysAgo := now.Add(-72 * time.Hour)
	future := now.Add(time.Hour)

	repo := &stubLogRepo{summaries: []*entity.SubscriberAccessSummary{
		{SubscriberID: 1, SubscriberName: "友人A", Active: true, LastAccessedAt: &threeDaysAgo, Count7d: 4, Count30d: 12},
		{SubscriberID: 2, SubscriberName: "友人B", Active: true},                          // 一度もアクセスなし
		{SubscriberID: 3, SubscriberName: "友人C", Active: true, LastAccessedAt: &future}, // clock skew
	}}
	svc := alUC.Service{Logs: repo, Now: fixedNow}

	got, err := svc.Summarize(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 3)

	// aggregation windows are now-7d / now-30d
	assert.Equal(t, now.AddDate(0, 0, -7), repo.since7d)
	assert.Equal(t, now.AddDate(0, 0, -30), repo.since30d)

	require.NotNil(t, got[0].DaysSinceLastAccess)
	assert.Equal(t, 3, *got[0].DaysSinceLastAccess)
	assert.Equal(t, int64(4), got[0].Count7d)

	assert.Nil(t, got[1].DaysSinceLastAccess, "never accessed → nil, not 0")

	require.NotNil(t, got[2].DaysSinceLastAccess)
	assert.Equal(t, 0, *got[2].DaysSinceLastAccess, "future access clamps to 0")
}
