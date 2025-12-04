package article

import (
	"errors"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	artUC "catchup-feed/internal/usecase/article"
)

type SearchHandler struct{ Svc artUC.Service }

// ServeHTTP 記事検索
// @Summary      記事検索
// @Description  キーワードで記事を検索します
// @Tags         articles
// @Security     BearerAuth
// @Produce      json
// @Param        keyword query string true "検索キーワード"
// @Success      200 {array} DTO "検索結果"
// @Failure      400 {string} string "Bad request - keyword query param required"
// @Failure      401 {string} string "Authentication required - missing or invalid JWT token"
// @Failure      403 {string} string "Forbidden - insufficient permissions"
// @Failure      500 {string} string "サーバーエラー"
// @Router       /articles/search [get]
func (h SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	kw := r.URL.Query().Get("keyword")
	if kw == "" {
		respond.SafeError(w, http.StatusBadRequest,
			errors.New("keyword query param required"))
		return
	}
	list, err := h.Svc.Search(r.Context(), kw)
	if err != nil {
		respond.SafeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]DTO, 0, len(list))
	for _, e := range list {
		out = append(out, DTO{
			ID: e.ID, SourceID: e.SourceID, Title: e.Title,
			URL: e.URL, Summary: e.Summary,
			PublishedAt: e.PublishedAt, CreatedAt: e.CreatedAt,
		})
	}
	respond.JSON(w, http.StatusOK, out)
}
