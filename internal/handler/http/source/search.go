package source

import (
	"net/http"
	"net/url"

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
// @Param        keyword query string false "検索キーワード（スペース区切り）"
// @Param        category query string false "カテゴリでフィルタ（台本のコーナー分け単位）"
// @Param        active query bool false "アクティブ状態でフィルタ"
// @Success      200 {array} DTO "検索結果"
// @Failure      400 {object} respond.ErrorResponse "Bad request"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      429 {object} respond.ErrorResponse "Too many requests - rate limit exceeded"
// @Failure      500 {object} respond.ErrorResponse "Server error"
// @Router       /sources/search [get]
func (h SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse keyword parameter (optional - allows filter-only searches)
	keywordParam := parseKeyword(r.URL)
	var keywords []string
	var err error
	if keywordParam != "" {
		// Parse space-separated keywords
		keywords, err = search.ParseKeywords(keywordParam, search.DefaultMaxKeywordCount, search.DefaultMaxKeywordLength)
		if err != nil {
			respond.SafeError(w, http.StatusBadRequest, err)
			return
		}
	} else {
		// Empty keyword - filter-only search mode
		keywords = []string{}
	}

	// Parse optional filters
	filters := repository.SourceSearchFilters{}

	// Parse category filter (free-form: categories are user-defined
	// script corners, §4)
	categoryParam := r.URL.Query().Get("category")
	if categoryParam != "" {
		filters.Category = &categoryParam
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
		out = append(out, toDTO(e.ID, e.Name, e.FeedURL, e.Category, e.Lang, e.Kind, e.Active, e.CreatedAt))
	}
	respond.JSON(w, http.StatusOK, out)
}

func parseKeyword(u *url.URL) string {
	return u.Query().Get("keyword")
}
