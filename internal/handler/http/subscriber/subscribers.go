package subscriber

import (
	"encoding/json"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	subUC "catchup-feed/internal/usecase/subscriber"
)

// Request is the create/update body. Name is required; note / email are
// optional and cleared when omitted on update (full replacement, §5.1).
type Request struct {
	Name  string  `json:"name"`
	Note  *string `json:"note"`
	Email *string `json:"email"`
}

type ListHandler struct{ Svc subUC.Service }

// ServeHTTP 友人一覧取得
// @Summary      友人一覧取得
// @Description  登録されている友人(購読者)をアクティブ・非アクティブ含めてすべて取得します(削除は論理削除のため両方返る、C-8)
// @Tags         subscribers
// @Security     BearerAuth
// @Produce      json
// @Success      200 {array} DTO "友人一覧"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      500 {object} respond.ErrorResponse "サーバーエラー"
// @Router       /subscribers [get]
func (h ListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	list, err := h.Svc.List(r.Context())
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	out := make([]DTO, 0, len(list))
	for _, s := range list {
		out = append(out, toDTO(s))
	}
	respond.JSON(w, http.StatusOK, out)
}

type CreateHandler struct{ Svc subUC.Service }

// ServeHTTP 友人登録
// @Summary      友人登録
// @Description  新しい友人(購読者)を登録します。name は必須、note / email は任意です
// @Tags         subscribers
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        subscriber body Request true "友人情報"
// @Success      201 {object} DTO "作成された友人"
// @Failure      400 {object} respond.ErrorResponse "Bad request - name is required"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Router       /subscribers [post]
func (h CreateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	created, err := h.Svc.Create(r.Context(), subUC.Input{Name: req.Name, Note: req.Note, Email: req.Email})
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusCreated, toDTO(created))
}

type GetHandler struct{ Svc subUC.Service }

// ServeHTTP 友人取得
// @Summary      友人取得
// @Description  友人(購読者)を1件取得します
// @Tags         subscribers
// @Security     BearerAuth
// @Produce      json
// @Param        id path int true "友人ID"
// @Success      200 {object} DTO "友人"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid ID"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      404 {object} respond.ErrorResponse "Not found - subscriber not found"
// @Router       /subscribers/{id} [get]
func (h GetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	subscriber, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, toDTO(subscriber))
}

type UpdateHandler struct{ Svc subUC.Service }

// ServeHTTP 友人更新
// @Summary      友人更新
// @Description  友人(購読者)の name / note / email を更新します(全置換。省略したフィールドはクリアされます)
// @Tags         subscribers
// @Security     BearerAuth
// @Accept       json
// @Produce      json
// @Param        id path int true "友人ID"
// @Param        subscriber body Request true "更新する友人情報"
// @Success      200 {object} DTO "更新後の友人"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid input"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      404 {object} respond.ErrorResponse "Not found - subscriber not found"
// @Router       /subscribers/{id} [put]
func (h UpdateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	updated, err := h.Svc.Update(r.Context(), id, subUC.Input{Name: req.Name, Note: req.Note, Email: req.Email})
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, toDTO(updated))
}

type DeleteHandler struct{ Svc subUC.Service }

// ServeHTTP 友人削除(論理削除)
// @Summary      友人削除(論理削除)
// @Description  友人(購読者)を非アクティブ化します。行は削除されず(C-8: トークンとアクセスログは集計のため残す)、その友人のトークンは即座に検証を通らなくなります(§5.2)。冪等です
// @Tags         subscribers
// @Security     BearerAuth
// @Param        id path int true "友人ID"
// @Success      204 "No Content"
// @Failure      400 {object} respond.ErrorResponse "Bad request - invalid ID"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      404 {object} respond.ErrorResponse "Not found - subscriber not found"
// @Router       /subscribers/{id} [delete]
func (h DeleteHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	if err := h.Svc.Deactivate(r.Context(), id); err != nil {
		respondUsecaseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
