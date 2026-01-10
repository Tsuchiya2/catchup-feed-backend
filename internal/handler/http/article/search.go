package article

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"catchup-feed/internal/handler/http/respond"
	"catchup-feed/internal/pkg/search"
	"catchup-feed/internal/pkg/validation"
	"catchup-feed/internal/repository"
	artUC "catchup-feed/internal/usecase/article"
)

type SearchHandler struct{ Svc artUC.Service }

// ServeHTTP 記事検索
// @Summary      記事検索
// @Description  マルチキーワードで記事を検索します（AND論理）
// @Tags         articles
// @Security     BearerAuth
// @Produce      json
// @Param        keyword query string true "検索キーワード（スペース区切り）"
// @Param        source_id query int false "ソースIDでフィルタ"
// @Param        from query string false "公開日時の開始（ISO 8601）"
// @Param        to query string false "公開日時の終了（ISO 8601）"
// @Success      200 {array} DTO "検索結果" headers(X-RateLimit-Limit=integer,X-RateLimit-Remaining=integer,X-RateLimit-Reset=integer)
// @Failure      400 {string} string "Bad request"
// @Failure      401 {string} string "Authentication required"
// @Failure      429 {string} string "Too many requests - rate limit exceeded" headers(X-RateLimit-Limit=integer,X-RateLimit-Remaining=integer,X-RateLimit-Reset=integer,Retry-After=integer)
// @Failure      500 {string} string "Server error"
// Note: This handler is deprecated. SearchPaginatedHandler is used in production.
// Router annotation removed to avoid Swagger duplicate route warning.
func (h SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse keyword parameter (required)
	kw := r.URL.Query().Get("keyword")
	if kw == "" {
		respond.SafeError(w, http.StatusBadRequest,
			errors.New("keyword query param required"))
		return
	}

	// Parse and validate keywords
	keywords, err := search.ParseKeywords(kw, search.DefaultMaxKeywordCount, search.DefaultMaxKeywordLength)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest,
			fmt.Errorf("invalid keyword: %w", err))
		return
	}

	// Build filters
	var filters repository.ArticleSearchFilters

	// Parse source_id if provided
	if sourceIDStr := r.URL.Query().Get("source_id"); sourceIDStr != "" {
		sourceID, err := strconv.ParseInt(sourceIDStr, 10, 64)
		if err != nil {
			respond.SafeError(w, http.StatusBadRequest,
				errors.New("invalid source_id: must be a valid integer"))
			return
		}
		if sourceID <= 0 {
			respond.SafeError(w, http.StatusBadRequest,
				errors.New("invalid source_id: must be positive"))
			return
		}
		filters.SourceID = &sourceID
	}

	// Parse from date if provided
	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		from, err := validation.ParseDateISO8601(fromStr)
		if err != nil {
			respond.SafeError(w, http.StatusBadRequest,
				fmt.Errorf("invalid from date: %w", err))
			return
		}
		filters.From = from
	}

	// Parse to date if provided
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		to, err := validation.ParseDateISO8601(toStr)
		if err != nil {
			respond.SafeError(w, http.StatusBadRequest,
				fmt.Errorf("invalid to date: %w", err))
			return
		}
		filters.To = to
	}

	// Validate date range: from <= to
	if filters.From != nil && filters.To != nil {
		if filters.From.After(*filters.To) {
			respond.SafeError(w, http.StatusBadRequest,
				errors.New("invalid date range: from date must be before or equal to to date"))
			return
		}
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
			ID: e.ID, SourceID: e.SourceID, Title: e.Title,
			URL: e.URL, Summary: e.Summary,
			PublishedAt: e.PublishedAt, CreatedAt: e.CreatedAt,
			UpdatedAt: e.CreatedAt, // Database schema doesn't have updated_at column
		})
	}
	respond.JSON(w, http.StatusOK, out)
}
