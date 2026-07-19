package viewer

import (
	"encoding/json"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	viewerUC "catchup-feed/internal/usecase/viewer"
)

type ListHandler struct{ Svc *viewerUC.Service }

// ServeHTTP viewer 一覧取得
// @Summary      viewer 一覧取得
// @Description  閲覧専用アカウント(viewer, D-27)をアクティブ・非アクティブ含めてすべて取得します。
// @Description  active / deactivated_at で有効・無効を判別できます。admin 専用
// @Tags         viewers
// @Security     BearerAuth
// @Produce      json
// @Success      200 {array} DTO "viewer 一覧"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      403 {object} respond.ErrorResponse "Forbidden - admin 専用"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /viewers [get]
func (h ListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	list, err := h.Svc.List(r.Context())
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	out := make([]DTO, 0, len(list))
	for _, v := range list {
		out = append(out, toDTO(v))
	}
	respond.JSON(w, http.StatusOK, out)
}

type CreateHandler struct{ Svc *viewerUC.Service }

// ServeHTTP viewer 登録
// @Summary      viewer 登録
// @Description  閲覧専用アカウントを作成します。パスワードは admin が設定し(D-27 (2))、
// @Description  サーバー側で bcrypt ハッシュのみ保存されます。admin 専用
// @Tags         viewers
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        viewer body CreateRequest true "viewer 情報(name / email / password すべて必須)"
// @Success      201 {object} DTO "作成された viewer"
// @Failure      400 {object} respond.ErrorResponse "Bad request - name/email/password が不正"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      403 {object} respond.ErrorResponse "Forbidden - admin 専用"
// @Failure      409 {object} respond.ErrorResponse "Conflict - email が既に登録済み"
// @Router       /viewers [post]
func (h CreateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	created, err := h.Svc.Create(r.Context(), viewerUC.CreateInput{
		Name:     req.Name,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusCreated, toDTO(created))
}

type UpdateHandler struct{ Svc *viewerUC.Service }

// ServeHTTP viewer 更新
// @Summary      viewer 更新
// @Description  viewer の name / email を更新します。password は任意で、指定した場合のみ
// @Description  再設定されます(省略時は現在のパスワードを維持)。admin 専用
// @Tags         viewers
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id path int true "viewer ID"
// @Param        viewer body UpdateRequest true "更新する viewer 情報"
// @Success      200 {object} DTO "更新後の viewer"
// @Failure      400 {object} respond.ErrorResponse "Bad request - 入力が不正"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      403 {object} respond.ErrorResponse "Forbidden - admin 専用"
// @Failure      404 {object} respond.ErrorResponse "Not found - viewer が存在しない"
// @Failure      409 {object} respond.ErrorResponse "Conflict - email が既に登録済み"
// @Router       /viewers/{id} [put]
func (h UpdateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	var req UpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	updated, err := h.Svc.Update(r.Context(), id, viewerUC.UpdateInput{
		Name:     req.Name,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, toDTO(updated))
}

type SetActiveHandler struct{ Svc *viewerUC.Service }

// ServeHTTP viewer 有効/無効切替
// @Summary      viewer 有効/無効切替
// @Description  viewer の有効/無効を切り替えます(論理無効化 = deactivated_at の set/clear)。
// @Description  無効化はリクエスト時の DB 再検証により即時反映され、発行済み JWT の失効を
// @Description  待ちません(D-27 (4))。冪等。admin 専用
// @Tags         viewers
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id path int true "viewer ID"
// @Param        active body ActiveRequest true "{active: true|false}"
// @Success      200 {object} DTO "切替後の viewer"
// @Failure      400 {object} respond.ErrorResponse "Bad request - 入力が不正"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      403 {object} respond.ErrorResponse "Forbidden - admin 専用"
// @Failure      404 {object} respond.ErrorResponse "Not found - viewer が存在しない"
// @Router       /viewers/{id}/active [put]
func (h SetActiveHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	var req ActiveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	updated, err := h.Svc.SetActive(r.Context(), id, req.Active)
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, toDTO(updated))
}

type DeleteHandler struct{ Svc *viewerUC.Service }

// ServeHTTP viewer 削除(物理削除)
// @Summary      viewer 削除(物理削除)
// @Description  viewer を物理削除します(subscribers と異なり集計のために行を残す理由が
// @Description  ないため、D-27 (4))。削除後、その viewer の既存 JWT はリクエスト時の
// @Description  DB 再検証で即座に 403 になります。admin 専用
// @Tags         viewers
// @Security     BearerAuth
// @Param        id path int true "viewer ID"
// @Success      204 "No Content"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid ID"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      403 {object} respond.ErrorResponse "Forbidden - admin 専用"
// @Failure      404 {object} respond.ErrorResponse "Not found - viewer が存在しない"
// @Router       /viewers/{id} [delete]
func (h DeleteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.Svc.Delete(r.Context(), id); err != nil {
		respondUsecaseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
