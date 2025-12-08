package article

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"catchup-feed/internal/common/pagination"
	"catchup-feed/internal/handler/http/respond"
	"catchup-feed/internal/pkg/search"
	"catchup-feed/internal/pkg/validation"
	"catchup-feed/internal/repository"
	artUC "catchup-feed/internal/usecase/article"
)

type SearchPaginatedHandler struct {
	Svc           artUC.Service
	PaginationCfg pagination.Config
}

// PaginatedResponse represents the response format for paginated search
type PaginatedResponse struct {
	Data       []DTO               `json:"data"`
	Pagination pagination.Metadata `json:"pagination"`
}

// ServeHTTP 記事検索（ページネーション付き）
// @Summary      記事検索（ページネーション付き）
// @Description  マルチキーワードで記事を検索します（AND論理）、ページネーション対応
// @Tags         articles
// @Security     BearerAuth
// @Produce      json
// @Param        keyword query string false "検索キーワード（スペース区切り）"
// @Param        source_id query int false "ソースIDでフィルタ"
// @Param        from query string false "公開日時の開始（ISO 8601）"
// @Param        to query string false "公開日時の終了（ISO 8601）"
// @Param        page query int false "ページ番号（1-indexed、デフォルト: 1）"
// @Param        limit query int false "1ページあたりの件数（デフォルト: 10、最大: 100）"
// @Success      200 {object} PaginatedResponse "検索結果（ページネーション付き）"
// @Failure      400 {string} string "Bad request"
// @Failure      401 {string} string "Authentication required"
// @Failure      500 {string} string "Server error"
// @Router       /articles/search [get]
func (h SearchPaginatedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse pagination parameters
	paginationParams, err := pagination.ParseQueryParams(r, h.PaginationCfg)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}

	// Parse keyword parameter (optional - allows browsing with filters only)
	kw := r.URL.Query().Get("keyword")
	var keywords []string
	if kw != "" {
		// Parse and validate keywords
		keywords, err = search.ParseKeywords(kw, search.DefaultMaxKeywordCount, search.DefaultMaxKeywordLength)
		if err != nil {
			respond.SafeError(w, http.StatusBadRequest,
				fmt.Errorf("invalid keyword: %w", err))
			return
		}
	} else {
		// Empty keyword - return all articles with pagination (filtered if filters provided)
		keywords = []string{}
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

	// Execute search with filters and pagination
	result, err := h.Svc.SearchWithFiltersPaginated(
		r.Context(),
		keywords,
		filters,
		paginationParams.Page,
		paginationParams.Limit,
	)
	if err != nil {
		respond.SafeError(w, http.StatusInternalServerError, err)
		return
	}

	// Convert to DTO
	out := make([]DTO, 0, len(result.Data))
	for _, item := range result.Data {
		out = append(out, DTO{
			ID:          item.Article.ID,
			SourceID:    item.Article.SourceID,
			SourceName:  item.SourceName,
			Title:       item.Article.Title,
			URL:         item.Article.URL,
			Summary:     item.Article.Summary,
			PublishedAt: item.Article.PublishedAt,
			CreatedAt:   item.Article.CreatedAt,
			UpdatedAt:   item.Article.CreatedAt, // Database schema doesn't have updated_at column
		})
	}

	// Return paginated response
	respond.JSON(w, http.StatusOK, PaginatedResponse{
		Data:       out,
		Pagination: result.Pagination,
	})
}
