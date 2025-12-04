package source

import (
	"encoding/json"
	"errors"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	srcUC "catchup-feed/internal/usecase/source"
)

type CreateHandler struct{ Svc srcUC.Service }

// ServeHTTP ソース作成
// @Summary      ソース作成
// @Description  新しいソースを作成します
// @Tags         sources
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        source body object true "ソース情報"
// @Success      201 "Created"
// @Failure      400 {string} string "Bad request - invalid input"
// @Failure      401 {string} string "Authentication required - missing or invalid JWT token"
// @Failure      403 {string} string "Forbidden - admin role required"
// @Router       /sources [post]
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
