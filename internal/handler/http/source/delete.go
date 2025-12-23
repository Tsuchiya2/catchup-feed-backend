package source

import (
	"net/http"

	"catchup-feed/internal/handler/http/pathutil"
	"catchup-feed/internal/handler/http/respond"
	srcUC "catchup-feed/internal/usecase/source"
)

type DeleteHandler struct{ Svc srcUC.Service }

// ServeHTTP ソース削除
// @Summary      ソース削除
// @Description  ソースを削除します
// @Tags         sources
// @Security     BearerAuth
// @Param        id path int true "ソースID"
// @Success      204 "No Content" headers(X-RateLimit-Limit=integer,X-RateLimit-Remaining=integer,X-RateLimit-Reset=integer)
// @Failure      400 {string} string "Bad request - invalid ID"
// @Failure      401 {string} string "Authentication required - missing or invalid JWT token"
// @Failure      403 {string} string "Forbidden - admin role required"
// @Failure      429 {string} string "Too many requests - rate limit exceeded" headers(X-RateLimit-Limit=integer,X-RateLimit-Remaining=integer,X-RateLimit-Reset=integer,Retry-After=integer)
// @Failure      500 {string} string "サーバーエラー"
// @Router       /sources/{id} [delete]
func (h DeleteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathutil.ExtractID(r.URL.Path, "/sources/")
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.Svc.Delete(r.Context(), id); err != nil {
		respond.SafeError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
