package article

import (
	"net/http"

	"catchup-feed/internal/handler/http/pathutil"
	"catchup-feed/internal/handler/http/respond"
	artUC "catchup-feed/internal/usecase/article"
)

type DeleteHandler struct{ Svc artUC.Service }

// ServeHTTP 記事削除
// @Summary      記事削除
// @Description  記事を削除します
// @Tags         articles
// @Security     BearerAuth
// @Param        id path int true "記事ID"
// @Success      204 "No Content"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid ID"
// @Failure      401 {object} respond.ErrorResponse "Authentication required - missing or invalid JWT token"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /articles/{id} [delete]
func (h DeleteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathutil.ExtractID(r.URL.Path, "/articles/")
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
