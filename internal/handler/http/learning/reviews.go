package learning

import (
	"encoding/json"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	learncore "catchup-feed/internal/learning"
	learnUC "catchup-feed/internal/usecase/learning"
)

type PendingReviewsHandler struct{ Svc learnUC.Service }

// ServeHTTP 未採点の出題一覧取得
// @Summary      未採点の出題一覧取得
// @Description  未採点の出題(review log)を古い出題日から順に返します。採点画面(§8.2)用に項目の concept/question/answer を含みます。空配列は正常系です(「今日は採点するものがありません」)— 件数バッジ・期日超過集計は設計で禁止されています(§2 Out)
// @Tags         learning
// @Security     BearerAuth
// @Produce      json
// @Success      200 {array} PendingReviewDTO "未採点の出題一覧(古い順)"
// @Failure      401 {object} respond.ErrorResponse "Authentication required - missing or invalid JWT token"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /learning/reviews/pending [get]
func (h PendingReviewsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pending, err := h.Svc.PendingReviews(r.Context())
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	out := make([]PendingReviewDTO, 0, len(pending))
	for _, p := range pending {
		out = append(out, toPendingDTO(p))
	}
	respond.JSON(w, http.StatusOK, out)
}

type GradeHandler struct{ Svc learnUC.Service }

// ServeHTTP 採点
// @Summary      採点(一発確定)
// @Description  出題(review log)に自己採点 ○△×(good/fuzzy/forgot)を記録し、項目のステージ遷移(§6.1、due 起点は採点日 JST)を適用します。**採点は一発確定**: 採点済み(手動・48h 自動解決 result=auto とも)への再採点は 409 を返し、上書きはできません
// @Tags         learning
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id path int true "出題ログID(pending の log_id)"
// @Param        grade body GradeRequest true "採点結果"
// @Success      200 {object} GradeResponse "遷移後の項目状態"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid ID or result"
// @Failure      401 {object} respond.ErrorResponse "Authentication required - missing or invalid JWT token"
// @Failure      404 {object} respond.ErrorResponse "Not found - review log not found"
// @Failure      409 {object} respond.ErrorResponse "Conflict - already graded (manual, auto-resolved or concurrent)"
// @Router       /learning/reviews/{id}/grade [post]
func (h GradeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	var req GradeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	outcome, err := h.Svc.Grade(r.Context(), id, req.Result)
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, GradeResponse{
		LogID:   id,
		ItemID:  outcome.ItemID,
		Result:  req.Result,
		Stage:   outcome.Stage,
		DueOn:   learncore.FormatDay(outcome.DueOn),
		Retired: outcome.Retired,
	})
}
