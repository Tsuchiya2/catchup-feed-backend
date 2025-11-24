package source

import (
	"encoding/json"
	"errors"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	srcUC "catchup-feed/internal/usecase/source"
)

type CreateHandler struct{ Svc srcUC.Service }

func (h CreateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		FeedURL string `json:"feedURL"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || req.FeedURL == "" {
		respond.SafeError(w, http.StatusBadRequest,
			errors.New("name and feedURL required"))
		return
	}
	err := h.Svc.Create(r.Context(), srcUC.CreateInput{
		Name: req.Name, FeedURL: req.FeedURL,
	})
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
