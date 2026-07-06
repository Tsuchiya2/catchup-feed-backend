package source

import (
	"encoding/json"
	"errors"
	"net/http"

	"catchup-feed/internal/handler/http/pathutil"
	"catchup-feed/internal/handler/http/respond"
	srcUC "catchup-feed/internal/usecase/source"
)

type UpdateHandler struct{ Svc srcUC.Service }

// ServeHTTP ソース更新
// @Summary      ソース更新
// @Description  既存のソースを更新します
// @Tags         sources
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id path int true "ソースID"
// @Param        source body UpdateRequest true "更新するソース情報"
// @Success      204 "No Content"
// @Failure      400 {string} string "Bad request - invalid input"
// @Failure      401 {string} string "Authentication required - missing or invalid JWT token"
// @Failure      404 {string} string "Not found - source not found"
// @Router       /sources/{id} [put]
func (h UpdateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathutil.ExtractID(r.URL.Path, "/sources/")
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}

	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}

	err = h.Svc.Update(r.Context(), srcUC.UpdateInput{
		ID: id, Name: req.Name, FeedURL: req.FeedURL,
		Category: req.Category, Lang: req.Lang, Kind: req.Kind,
		Active: req.Active,
	})
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, srcUC.ErrSourceNotFound) {
			code = http.StatusNotFound
		}
		respond.SafeError(w, code, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
