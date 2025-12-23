package source

import (
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	srcUC "catchup-feed/internal/usecase/source"
)

type ListHandler struct{ Svc srcUC.Service }

// ServeHTTP ソース一覧取得
// @Summary      ソース一覧取得
// @Description  登録されているすべてのソースを取得します
// @Tags         sources
// @Security     BearerAuth
// @Produce      json
// @Success      200 {array} DTO "ソース一覧" headers(X-RateLimit-Limit=integer,X-RateLimit-Remaining=integer,X-RateLimit-Reset=integer)
// @Failure      401 {string} string "Authentication required - missing or invalid JWT token"
// @Failure      403 {string} string "Forbidden - insufficient permissions"
// @Failure      429 {string} string "Too many requests - rate limit exceeded" headers(X-RateLimit-Limit=integer,X-RateLimit-Remaining=integer,X-RateLimit-Reset=integer,Retry-After=integer)
// @Failure      500 {string} string "サーバーエラー"
// @Router       /sources [get]
func (h ListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	list, err := h.Svc.List(r.Context())
	if err != nil {
		respond.SafeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]DTO, 0, len(list))
	for _, e := range list {
		out = append(out, DTO{
			ID: e.ID, Name: e.Name, FeedURL: e.FeedURL,
			LastCrawledAt: e.LastCrawledAt,
			Active:        e.Active,
		})
	}
	respond.JSON(w, http.StatusOK, out)
}
