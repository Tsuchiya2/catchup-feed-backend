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
