package article

import (
	"log/slog"
	"net/http"
	"time"

	"catchup-feed/internal/common/pagination"
	"catchup-feed/internal/handler/http/requestid"
	"catchup-feed/internal/handler/http/respond"
	"catchup-feed/internal/observability/logging"
	artUC "catchup-feed/internal/usecase/article"
)

type ListHandler struct {
	Svc           artUC.Service
	PaginationCfg pagination.Config
	Logger        *slog.Logger
}

// ServeHTTP 記事一覧取得
// @Summary      記事一覧取得（ページネーション対応）
// @Description  登録されている記事を取得します。ページネーションパラメータを指定して、ページ単位で記事を取得できます。
// @Tags         articles
// @Security     BearerAuth
// @Produce      json
// @Param        page   query    int  false  "ページ番号 (1-based)" default(1) minimum(1)
// @Param        limit  query    int  false  "1ページあたりの件数" default(20) minimum(1) maximum(100)
// @Success      200 {object} pagination.Response[DTO] "ページネーション付き記事一覧"
// @Failure      400 {string} string "Invalid query parameters"
// @Failure      401 {string} string "Authentication required - missing or invalid JWT token"
// @Failure      403 {string} string "Forbidden - insufficient permissions"
// @Failure      500 {string} string "サーバーエラー"
// @Router       /articles [get]
func (h ListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	startTime := time.Now()

	// Get request ID for logging
	reqID := requestid.FromContext(ctx)
	logger := logging.WithRequestID(ctx, h.Logger)

	// Parse pagination parameters
	params, err := pagination.ParseQueryParams(r, h.PaginationCfg)
	if err != nil {
		logger.Warn("Invalid pagination parameters",
			"error", err.Error(),
			"request_id", reqID)
		pagination.RecordError("validation")
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}

	// Log request
	logger.Info("Paginated article list request",
		"page", params.Page,
		"limit", params.Limit,
		"request_id", reqID)

	// Get paginated data from service
	result, err := h.Svc.ListWithSourcePaginated(ctx, params)
	if err != nil {
		logger.Error("Failed to list articles",
			"error", err.Error(),
			"page", params.Page,
			"limit", params.Limit,
			"request_id", reqID)
		pagination.RecordError("database")
		respond.SafeError(w, http.StatusInternalServerError, err)
		return
	}

	// Convert to DTOs
	dtos := make([]DTO, 0, len(result.Data))
	for _, item := range result.Data {
		dtos = append(dtos, DTO{
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

	// Build paginated response
	response := pagination.NewResponse(dtos, result.Pagination)

	// Record metrics
	duration := time.Since(startTime)
	pagination.RecordRequest(http.StatusOK, params.Page)
	pagination.RecordDuration("handler", duration.Seconds())
	pagination.UpdateTotalCount(result.Pagination.Total)

	// Log response
	logger.Info("Paginated response",
		"page", params.Page,
		"limit", params.Limit,
		"returned_count", len(dtos),
		"duration_ms", duration.Milliseconds(),
		"status", http.StatusOK,
		"request_id", reqID)

	respond.JSON(w, http.StatusOK, response)
}
