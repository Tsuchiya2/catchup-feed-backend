// Package accesslog provides the feed access log HTTP handlers (§5.1):
// the per-friend access timeline and the neglect detection summary (C-8).
// All routes are admin-only JWT routes.
package accesslog

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/auth"
	"catchup-feed/internal/handler/http/respond"
	alUC "catchup-feed/internal/usecase/accesslog"
)

// DTO is one access row joined with its subscriber (C-8), so the timeline
// is readable per friend without extra lookups.
type DTO struct {
	ID             int64     `json:"id"`
	TokenID        int64     `json:"token_id"`
	SubscriberID   int64     `json:"subscriber_id"`
	SubscriberName string    `json:"subscriber_name"`
	EpisodeID      *int64    `json:"episode_id"` // null = feed.xml 取得
	UserAgent      *string   `json:"user_agent"`
	AccessedAt     time.Time `json:"accessed_at"`
}

func toDTO(rec *entity.FeedAccessRecord) DTO {
	return DTO{
		ID:             rec.ID,
		TokenID:        rec.TokenID,
		SubscriberID:   rec.SubscriberID,
		SubscriberName: rec.SubscriberName,
		EpisodeID:      rec.EpisodeID,
		UserAgent:      rec.UserAgent,
		AccessedAt:     rec.AccessedAt,
	}
}

// SummaryDTO is one friend's aggregate: last access, recent counts and
// days_since_last_access (null = 一度もアクセスなし) for neglect detection.
type SummaryDTO struct {
	SubscriberID        int64      `json:"subscriber_id"`
	SubscriberName      string     `json:"subscriber_name"`
	Active              bool       `json:"active"`
	LastAccessedAt      *time.Time `json:"last_accessed_at"`
	DaysSinceLastAccess *int       `json:"days_since_last_access"`
	Count7d             int64      `json:"count_7d"`
	Count30d            int64      `json:"count_30d"`
}

func toSummaryDTO(s *alUC.Summary) SummaryDTO {
	return SummaryDTO{
		SubscriberID:        s.SubscriberID,
		SubscriberName:      s.SubscriberName,
		Active:              s.Active,
		LastAccessedAt:      s.LastAccessedAt,
		DaysSinceLastAccess: s.DaysSinceLastAccess,
		Count7d:             s.Count7d,
		Count30d:            s.Count30d,
	}
}

type ListHandler struct{ Svc alUC.Service }

// ServeHTTP アクセスログ一覧取得
// @Summary      アクセスログ一覧取得
// @Description  公開フィードへのアクセスログを新しい順に取得します。各行には友人(subscriber)が結合されます。subscriber_id で友人単位に絞り込めます
// @Tags         access-logs
// @Security     BearerAuth
// @Produce      json
// @Param        subscriber_id query int false "友人IDで絞り込み"
// @Param        limit query int false "取得件数(デフォルト100、最大1000)"
// @Success      200 {array} DTO "アクセスログ(新しい順)"
// @Failure      400 {string} string "Bad request - invalid query parameter"
// @Failure      401 {string} string "Authentication required"
// @Failure      500 {string} string "サーバーエラー"
// @Router       /access-logs [get]
func (h ListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var subscriberID *int64
	if raw := q.Get("subscriber_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			respond.SafeError(w, http.StatusBadRequest, errors.New("invalid subscriber_id"))
			return
		}
		subscriberID = &id
	}

	limit := 0 // 0 → usecase default
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			respond.SafeError(w, http.StatusBadRequest, errors.New("invalid limit"))
			return
		}
		limit = n
	}

	records, err := h.Svc.List(r.Context(), subscriberID, limit)
	if err != nil {
		respond.SafeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]DTO, 0, len(records))
	for _, rec := range records {
		out = append(out, toDTO(rec))
	}
	respond.JSON(w, http.StatusOK, out)
}

type SummaryHandler struct{ Svc alUC.Service }

// ServeHTTP アクセスログ集計取得
// @Summary      アクセスログ集計取得(友人単位)
// @Description  友人ごとの最終アクセス日時・経過日数・直近7日/30日のアクセス数を返します。days_since_last_access が大きい(または null = 一度もアクセスなし)友人が放置検知の対象です。トークン未発行の友人も含まれます
// @Tags         access-logs
// @Security     BearerAuth
// @Produce      json
// @Success      200 {array} SummaryDTO "友人単位の集計"
// @Failure      401 {string} string "Authentication required"
// @Failure      500 {string} string "サーバーエラー"
// @Router       /access-logs/summary [get]
func (h SummaryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.Svc.Summarize(r.Context())
	if err != nil {
		respond.SafeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]SummaryDTO, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, toSummaryDTO(s))
	}
	respond.JSON(w, http.StatusOK, out)
}

// Register registers the access log routes (§5.1). Both routes are
// admin-only: the viewer role's path allowlist excludes /access-logs, and
// the explicit auth.Authz wrap keeps them protected even if the mux is
// ever mounted without the outer Authz.
func Register(mux *http.ServeMux, svc alUC.Service) {
	mux.Handle("GET /access-logs", auth.Authz(ListHandler{svc}))
	mux.Handle("GET /access-logs/summary", auth.Authz(SummaryHandler{svc}))
}
