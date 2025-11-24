package source

import (
	"errors"
	"net/http"
	"net/url"

	"catchup-feed/internal/handler/http/respond"
	srcUC "catchup-feed/internal/usecase/source"
)

type SearchHandler struct{ Svc srcUC.Service }

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
