package source

import (
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	srcUC "catchup-feed/internal/usecase/source"
)

type ListHandler struct{ Svc srcUC.Service }

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
