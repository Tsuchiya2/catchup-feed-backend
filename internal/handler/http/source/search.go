package source

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"catchup-feed/internal/handler/http/respond"
	"catchup-feed/internal/pkg/search"
	"catchup-feed/internal/pkg/validation"
	"catchup-feed/internal/repository"
	srcUC "catchup-feed/internal/usecase/source"
)

type SearchHandler struct{ Svc srcUC.Service }

// ServeHTTP ソース検索
// @Summary      ソース検索
// @Description  マルチキーワードでソースを検索します（AND論理）
// @Tags         sources
// @Security     BearerAuth
// @Produce      json
// @Param        keyword query string true "検索キーワード（スペース区切り）"
// @Param        source_type query string false "ソースタイプでフィルタ（RSS, Webflow, NextJS, Remix）"
// @Param        active query bool false "アクティブ状態でフィルタ"
// @Success      200 {array} DTO "検索結果"
// @Failure      400 {string} string "Bad request"
// @Failure      401 {string} string "Authentication required"
// @Failure      500 {string} string "Server error"
// @Router       /sources/search [get]
func (h SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse and validate keyword parameter
	keywordParam := parseKeyword(r.URL)
	if keywordParam == "" {
		respond.SafeError(w, http.StatusBadRequest,
			errors.New("keyword query param required"))
		return
	}

	// Parse space-separated keywords
	keywords, err := search.ParseKeywords(keywordParam, search.DefaultMaxKeywordCount, search.DefaultMaxKeywordLength)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}

	// Parse optional filters
	filters := repository.SourceSearchFilters{}

	// Parse source_type filter
	sourceTypeParam := r.URL.Query().Get("source_type")
	if sourceTypeParam != "" {
		allowedSourceTypes := []string{"RSS", "Webflow", "NextJS", "Remix"}
		if err := validation.ValidateEnum(sourceTypeParam, allowedSourceTypes, "source_type"); err != nil {
			respond.SafeError(w, http.StatusBadRequest, err)
			return
		}
		filters.SourceType = &sourceTypeParam
	}

	// Parse active filter
	activeParam := r.URL.Query().Get("active")
	if activeParam != "" {
		active, err := validation.ParseBool(activeParam)
		if err != nil {
			respond.SafeError(w, http.StatusBadRequest, err)
			return
		}
		filters.Active = active
	}

	// Execute search with filters
	list, err := h.Svc.SearchWithFilters(r.Context(), keywords, filters)
	if err != nil {
		respond.SafeError(w, http.StatusInternalServerError, err)
		return
	}

	// Convert to DTO
	out := make([]DTO, 0, len(list))
	for _, e := range list {
		out = append(out, DTO{
			ID:            e.ID,
			Name:          e.Name,
			FeedURL:       e.FeedURL,
			URL:           e.FeedURL, // Map FeedURL to URL for frontend compatibility
			SourceType:    e.SourceType,
			LastCrawledAt: e.LastCrawledAt,
			Active:        e.Active,
			CreatedAt:     time.Time{}, // Database schema doesn't have created_at column for sources
			UpdatedAt:     time.Time{}, // Database schema doesn't have updated_at column for sources
		})
	}
	respond.JSON(w, http.StatusOK, out)
}

func parseKeyword(u *url.URL) string {
	return u.Query().Get("keyword")
}
