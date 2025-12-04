package source

import (
	"errors"
	"net/http"
	"net/url"

	"catchup-feed/internal/handler/http/respond"
	srcUC "catchup-feed/internal/usecase/source"
)

type SearchHandler struct{ Svc srcUC.Service }

// ServeHTTP ソース検索
// @Summary      ソース検索
// @Description  キーワードでソースを検索します
// @Tags         sources
// @Security     BearerAuth
// @Produce      json
// @Param        keyword query string true "検索キーワード"
// @Success      200 {array} DTO "検索結果"
// @Failure      400 {string} string "Bad request - keyword query param required"
// @Failure      401 {string} string "Authentication required - missing or invalid JWT token"
// @Failure      403 {string} string "Forbidden - insufficient permissions"
// @Failure      500 {string} string "サーバーエラー"
// @Router       /sources/search [get]
func (h SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	keyword := parseKeyword(r.URL)
	if keyword == "" {
		respond.SafeError(w, http.StatusBadRequest,
			errors.New("keyword query param required"))
		return
	}
	list, err := h.Svc.Search(r.Context(), keyword)
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

func parseKeyword(u *url.URL) string {
	return u.Query().Get("keyword")
}
