package article

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"catchup-feed/internal/handler/http/respond"
	artUC "catchup-feed/internal/usecase/article"
)

type CreateHandler struct{ Svc artUC.Service }

// ServeHTTP 記事作成
// @Summary      記事作成
// @Description  新しい記事を作成します
// @Tags         articles
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        article body object true "記事情報"
// @Success      201 "Created"
// @Header       201 {integer} X-RateLimit-Limit "Maximum number of requests allowed in the current window"
// @Header       201 {integer} X-RateLimit-Remaining "Number of requests remaining in the current window"
// @Header       201 {integer} X-RateLimit-Reset "Unix timestamp when the rate limit window resets"
// @Failure      400 {string} string "Bad request - invalid input"
// @Failure      401 {string} string "Authentication required - missing or invalid JWT token"
// @Failure      403 {string} string "Forbidden - admin role required"
// @Failure      429 {string} string "Too many requests - rate limit exceeded"
// @Header       429 {integer} X-RateLimit-Limit "Maximum number of requests allowed in the current window"
// @Header       429 {integer} X-RateLimit-Remaining "Number of requests remaining (should be 0)"
// @Header       429 {integer} X-RateLimit-Reset "Unix timestamp when the rate limit window resets"
// @Header       429 {integer} Retry-After "Seconds until the client should retry"
// @Router       /articles [post]
func (h CreateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SourceID    int64  `json:"source_id"`
		Title       string `json:"title"`
		URL         string `json:"url"`
		Summary     string `json:"summary"`
		PublishedAt string `json:"published_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	if req.SourceID == 0 || req.Title == "" || req.URL == "" {
		respond.SafeError(w, http.StatusBadRequest,
			errors.New("source_id, title, url are required"))
		return
	}

	var pAt time.Time
	if req.PublishedAt != "" {
		var err error
		pAt, err = time.Parse(time.RFC3339, req.PublishedAt)
		if err != nil {
			respond.SafeError(w, http.StatusBadRequest,
				errors.New("published_at must be in RFC3339 format"))
			return
		}
	}

	if err := h.Svc.Create(r.Context(), artUC.CreateInput{
		SourceID:    req.SourceID,
		Title:       req.Title,
		URL:         req.URL,
		Summary:     req.Summary,
		PublishedAt: pAt,
	}); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
