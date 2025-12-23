package article

import (
	"errors"
	"net/http"

	"catchup-feed/internal/handler/http/pathutil"
	"catchup-feed/internal/handler/http/respond"
	artUC "catchup-feed/internal/usecase/article"
)

type GetHandler struct{ Svc artUC.Service }

// ServeHTTP 記事詳細取得
// @Summary      記事詳細取得
// @Description  指定されたIDの記事を取得します（ソース名を含む）
// @Tags         articles
// @Security     BearerAuth
// @Produce      json
// @Param        id path int true "記事ID"
// @Success      200 {object} DTO "記事詳細" headers(X-RateLimit-Limit=integer,X-RateLimit-Remaining=integer,X-RateLimit-Reset=integer)
// @Failure      400 {string} string "Bad request - invalid article ID"
// @Failure      401 {string} string "Authentication required - missing or invalid JWT token"
// @Failure      403 {string} string "Forbidden - insufficient permissions"
// @Failure      404 {string} string "Not found - article not found"
// @Failure      429 {string} string "Too many requests - rate limit exceeded" headers(X-RateLimit-Limit=integer,X-RateLimit-Remaining=integer,X-RateLimit-Reset=integer,Retry-After=integer)
// @Failure      500 {string} string "サーバーエラー"
// @Router       /articles/{id} [get]
func (h GetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathutil.ExtractID(r.URL.Path, "/articles/")
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}

	article, sourceName, err := h.Svc.GetWithSource(r.Context(), id)
	if err != nil {
		code := http.StatusInternalServerError
		if errors.Is(err, artUC.ErrInvalidArticleID) {
			code = http.StatusBadRequest
		} else if errors.Is(err, artUC.ErrArticleNotFound) {
			code = http.StatusNotFound
		}
		respond.SafeError(w, code, err)
		return
	}

	out := DTO{
		ID:          article.ID,
		SourceID:    article.SourceID,
		SourceName:  sourceName,
		Title:       article.Title,
		URL:         article.URL,
		Summary:     article.Summary,
		PublishedAt: article.PublishedAt,
		CreatedAt:   article.CreatedAt,
		UpdatedAt:   article.CreatedAt, // Database schema doesn't have updated_at column
	}

	respond.JSON(w, http.StatusOK, out)
}
