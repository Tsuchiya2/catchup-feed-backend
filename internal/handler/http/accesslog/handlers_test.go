package accesslog_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"catchup-feed/internal/domain/entity"
	haccesslog "catchup-feed/internal/handler/http/accesslog"
	alUC "catchup-feed/internal/usecase/accesslog"
)

type stubLogRepo struct {
	subscriberID *int64
	limit        int
	records      []*entity.FeedAccessRecord
	summaries    []*entity.SubscriberAccessSummary
}

func (s *stubLogRepo) Insert(context.Context, *entity.FeedAccessLog) error { return nil }

func (s *stubLogRepo) ListRecords(_ context.Context, subscriberID *int64, limit int) ([]*entity.FeedAccessRecord, error) {
	s.subscriberID = subscriberID
	s.limit = limit
	return s.records, nil
}

func (s *stubLogRepo) SummarizeBySubscriber(context.Context, time.Time, time.Time) ([]*entity.SubscriberAccessSummary, error) {
	return s.summaries, nil
}

func fixedNow() time.Time {
	return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
}

func TestListHandler(t *testing.T) {
	episodeID := int64(12)
	record := &entity.FeedAccessRecord{
		FeedAccessLog: entity.FeedAccessLog{
			ID: 2, TokenID: 1, EpisodeID: &episodeID,
			AccessedAt: time.Date(2026, 7, 3, 8, 0, 0, 0, time.UTC),
		},
		SubscriberID:   7,
		SubscriberName: "友人A",
	}

	tests := []struct {
		name             string
		target           string
		wantCode         int
		wantSubscriberID *int64
		wantLimit        int
	}{
		{name: "default limit, no filter", target: "/access-logs", wantCode: http.StatusOK, wantLimit: 0},
		{name: "subscriber filter and limit", target: "/access-logs?subscriber_id=7&limit=10", wantCode: http.StatusOK, wantSubscriberID: ptr(int64(7)), wantLimit: 10},
		{name: "invalid subscriber_id", target: "/access-logs?subscriber_id=abc", wantCode: http.StatusBadRequest},
		{name: "non-positive subscriber_id", target: "/access-logs?subscriber_id=0", wantCode: http.StatusBadRequest},
		{name: "invalid limit", target: "/access-logs?limit=abc", wantCode: http.StatusBadRequest},
		{name: "non-positive limit", target: "/access-logs?limit=-1", wantCode: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubLogRepo{records: []*entity.FeedAccessRecord{record}, limit: -999}
			handler := haccesslog.ListHandler{Svc: alUC.Service{Logs: repo, Now: fixedNow}}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tt.target, nil))
			require.Equal(t, tt.wantCode, rr.Code)

			if tt.wantCode != http.StatusOK {
				assert.Equal(t, -999, repo.limit, "repository must not be hit on validation errors")
				return
			}

			assert.Equal(t, tt.wantSubscriberID, repo.subscriberID)
			if tt.wantLimit == 0 {
				assert.Equal(t, alUC.DefaultLimit, repo.limit)
			} else {
				assert.Equal(t, tt.wantLimit, repo.limit)
			}

			var got []haccesslog.DTO
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
			require.Len(t, got, 1)
			assert.Equal(t, int64(7), got[0].SubscriberID)
			assert.Equal(t, "友人A", got[0].SubscriberName)
			require.NotNil(t, got[0].EpisodeID)
			assert.Equal(t, int64(12), *got[0].EpisodeID)
		})
	}
}

func TestSummaryHandler(t *testing.T) {
	threeDaysAgo := fixedNow().Add(-72 * time.Hour)
	repo := &stubLogRepo{summaries: []*entity.SubscriberAccessSummary{
		{SubscriberID: 1, SubscriberName: "友人A", Active: true, LastAccessedAt: &threeDaysAgo, Count7d: 4, Count30d: 12},
		{SubscriberID: 2, SubscriberName: "友人B", Active: true}, // 一度もアクセスなし
	}}
	handler := haccesslog.SummaryHandler{Svc: alUC.Service{Logs: repo, Now: fixedNow}}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/access-logs/summary", nil))
	require.Equal(t, http.StatusOK, rr.Code)

	var got []haccesslog.SummaryDTO
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.Len(t, got, 2)

	require.NotNil(t, got[0].DaysSinceLastAccess, "neglect indicator is precomputed for the dashboard")
	assert.Equal(t, 3, *got[0].DaysSinceLastAccess)
	assert.Equal(t, int64(4), got[0].Count7d)
	assert.Equal(t, int64(12), got[0].Count30d)

	assert.Nil(t, got[1].LastAccessedAt)
	assert.Nil(t, got[1].DaysSinceLastAccess, "never-accessed friend stays null, not 0 days")
}

// TestRegister_RequiresJWT pins that the access log routes reject anonymous
// requests: both are wrapped in auth.Authz at registration.
func TestRegister_RequiresJWT(t *testing.T) {
	mux := http.NewServeMux()
	haccesslog.Register(mux, alUC.Service{Logs: &stubLogRepo{}, Now: fixedNow})

	for _, path := range []string{"/access-logs", "/access-logs/summary"} {
		t.Run(path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
			assert.Equal(t, http.StatusUnauthorized, rr.Code, "missing JWT must be rejected")
		})
	}
}

func ptr[T any](v T) *T { return &v }
