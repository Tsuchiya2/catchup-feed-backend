package article

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"catchup-feed/internal/handler/http/pathutil"
	"catchup-feed/internal/handler/http/respond"
	artUC "catchup-feed/internal/usecase/article"
)

type UpdateHandler struct{ Svc artUC.Service }

// ServeHTTP 記事更新
// @Summary      記事更新
// @Description  既存の記事を更新します
// @Tags         articles
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id path int true "記事ID"
// @Param        article body object true "更新する記事情報"
// @Success      204 "No Content"
// @Failure      400 {string} string "Bad request - invalid input"
// @Failure      401 {string} string "Authentication required - missing or invalid JWT token"
// @Failure      403 {string} string "Forbidden - admin role required"
// @Failure      404 {string} string "Not found - article not found"
// @Router       /articles/{id} [put]
func (h UpdateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathutil.ExtractID(r.URL.Path, "/articles/")
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}

	var req struct {
		SourceID    *int64  `json:"source_id"`
		Title       *string `json:"title"`
		URL         *string `json:"url"`
		Summary     *string `json:"summary"`
		PublishedAt *string `json:"published_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}

	var pAtPtr *time.Time
	if req.PublishedAt != nil {
		t, err := time.Parse(time.RFC3339, *req.PublishedAt)
		if err != nil {
			respond.SafeError(w, http.StatusBadRequest,
				errors.New("published_at must be in RFC3339 format"))
			return
		}
		pAtPtr = &t
	}

	err = h.Svc.Update(r.Context(), artUC.UpdateInput{
		ID:          id,
		SourceID:    req.SourceID,
		Title:       req.Title,
		URL:         req.URL,
		Summary:     req.Summary,
		PublishedAt: pAtPtr,
	})
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, artUC.ErrArticleNotFound) {
			code = http.StatusNotFound
		}
		respond.SafeError(w, code, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
