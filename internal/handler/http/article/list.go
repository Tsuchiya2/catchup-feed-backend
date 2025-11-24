package article

import (
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	artUC "catchup-feed/internal/usecase/article"
)

type ListHandler struct{ Svc artUC.Service }

// ServeHTTP 記事一覧取得
// @Summary      記事一覧取得
// @Description  登録されているすべての記事を取得します
// @Tags         articles
// @Produce      json
// @Success      200 {array} DTO "記事一覧"
// @Failure      500 {string} string "サーバーエラー"
// @Router       /articles [get]
func (h ListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	list, err := h.Svc.List(r.Context())
	if err != nil {
		respond.SafeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]DTO, 0, len(list))
	for _, e := range list {
		out = append(out, DTO{
			ID:          e.ID,
			SourceID:    e.SourceID,
			Title:       e.Title,
			URL:         e.URL,
			Summary:     e.Summary,
			PublishedAt: e.PublishedAt,
			CreatedAt:   e.CreatedAt,
		})
	}
	respond.JSON(w, http.StatusOK, out)
}
