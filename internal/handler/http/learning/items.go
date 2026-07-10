package learning

import (
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	learnUC "catchup-feed/internal/usecase/learning"
)

type ListItemsHandler struct{ Svc learnUC.Service }

// ServeHTTP 学習項目一覧取得
// @Summary      学習項目一覧取得
// @Description  学習項目(トラッカー)を取得します。status=active(デフォルト)は現役項目を期日順(due_on ASC)、status=retired は卒業・手動アーカイブ済み項目を新しい順(retired_at DESC)で返します。履歴サマリは出題回数と直近結果のみです(§8.1 — 過剰な集計はしない)
// @Tags         learning
// @Security     BearerAuth
// @Produce      json
// @Param        status query string false "フィルタ(デフォルト active)" Enums(active,retired)
// @Success      200 {array} ItemDTO "学習項目一覧"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid status"
// @Failure      401 {object} respond.ErrorResponse "Authentication required - missing or invalid JWT token"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /learning/items [get]
func (h ListItemsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	items, err := h.Svc.ListItems(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	out := make([]ItemDTO, 0, len(items))
	for _, item := range items {
		out = append(out, toItemDTO(item))
	}
	respond.JSON(w, http.StatusOK, out)
}

type RetireItemHandler struct{ Svc learnUC.Service }

// ServeHTTP 学習項目の手動アーカイブ
// @Summary      学習項目の手動アーカイブ
// @Description  項目を手動でアーカイブします(「もう追わなくていい」)。retired_at をセットするのみで、以後の出題選定から外れます。冪等: アーカイブ済み項目への再実行は元の retired_at をそのまま返します(200)
// @Tags         learning
// @Security     BearerAuth
// @Produce      json
// @Param        id path int true "学習項目ID"
// @Success      200 {object} RetireResponse "アーカイブ後の項目(冪等)"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid ID"
// @Failure      401 {object} respond.ErrorResponse "Authentication required - missing or invalid JWT token"
// @Failure      404 {object} respond.ErrorResponse "Not found - learning item not found"
// @Router       /learning/items/{id}/retire [post]
func (h RetireItemHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	retiredAt, err := h.Svc.RetireItem(r.Context(), id)
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, RetireResponse{ID: id, RetiredAt: retiredAt})
}
