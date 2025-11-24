package article

import (
	"errors"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	artUC "catchup-feed/internal/usecase/article"
)

type SearchHandler struct{ Svc artUC.Service }

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
