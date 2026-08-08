package source

import (
	"errors"
	"net/http"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/pathutil"
	"catchup-feed/internal/handler/http/respond"
	srcUC "catchup-feed/internal/usecase/source"
)

type DeleteHandler struct{ Svc srcUC.Service }

// ServeHTTP ソース削除
// @Summary      ソース削除
// @Description  ソースを削除します(論理削除。記事は保持されます)
// @Tags         sources
// @Security     BearerAuth
// @Param        id path int true "ソースID"
// @Success      204 "No Content"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid ID"
// @Failure      401 {object} respond.ErrorResponse "Authentication required - missing or invalid JWT token"
// @Failure      404 {object} respond.ErrorResponse "Not found - source not found"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /sources/{id} [delete]
func (h DeleteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathutil.ExtractID(r.URL.Path, "/sources/")
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.Svc.Delete(r.Context(), id); err != nil {
		code := http.StatusInternalServerError
		var vErr *entity.ValidationError
		switch {
		case errors.Is(err, srcUC.ErrSourceNotFound):
			code = http.StatusNotFound
		case errors.As(err, &vErr):
			code = http.StatusBadRequest
		}
		respond.SafeError(w, code, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
