package source

import (
	"net/http"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/handler/http/auth"
	"catchup-feed/internal/handler/http/respond"
	srcUC "catchup-feed/internal/usecase/source"
)

type ListHandler struct{ Svc srcUC.Service }

// ServeHTTP ソース一覧取得
// @Summary      ソース一覧取得
// @Description  登録されているソースを取得します。admin はアクティブ・非アクティブ含む全件、
// @Description  viewer はアクティブなソースのみ返ります(サーバー側で強制フィルタ、D-27)
// @Tags         sources
// @Security     BearerAuth
// @Produce      json
// @Success      200 {array} DTO "ソース一覧"
// @Failure      401 {object} respond.ErrorResponse "Authentication required - missing or invalid JWT token"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /sources [get]
func (h ListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// D-27 (3): viewer には active=TRUE をサーバー側で強制する(クエリ
	// パラメータでの opt-in ではない)。admin は従来どおり全件。
	var (
		list []*entity.Source
		err  error
	)
	if auth.RoleFromContext(r.Context()) == auth.RoleViewer {
		list, err = h.Svc.ListActive(r.Context())
	} else {
		list, err = h.Svc.List(r.Context())
	}
	if err != nil {
		respond.SafeError(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]DTO, 0, len(list))
	for _, e := range list {
		out = append(out, toDTO(e.ID, e.Name, e.FeedURL, e.Category, e.Lang, e.Kind, e.Active, e.CreatedAt))
	}
	respond.JSON(w, http.StatusOK, out)
}
