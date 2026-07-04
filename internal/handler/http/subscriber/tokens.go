package subscriber

import (
	"fmt"
	"net/http"

	"catchup-feed/internal/handler/http/respond"
	subUC "catchup-feed/internal/usecase/subscriber"
)

type IssueTokenHandler struct {
	Svc subUC.Service
	// PublicBaseURL is feed.Config.PublicBaseURL (D-6), used to assemble
	// the one-time subscription URL.
	PublicBaseURL string
}

// ServeHTTP トークン発行
// @Summary      トークン発行
// @Description  友人に新しいフィードトークンを発行します。DB にはハッシュのみ保存され(D-5)、レスポンスの token(平文)と feed_url(購読 URL)は**この発行時の一度だけ**返ります。以後いかなる API でも再取得できません。紛失時は失効させて再発行してください(§5.2)
// @Tags         tokens
// @Security     BearerAuth
// @Produce      json
// @Param        id path int true "友人ID"
// @Success      201 {object} IssuedTokenDTO "発行されたトークン(平文と購読 URL はこのレスポンス限り)"
// @Failure      400 {string} string "Bad request - invalid ID"
// @Failure      401 {string} string "Authentication required"
// @Failure      404 {string} string "Not found - subscriber not found"
// @Failure      409 {string} string "Conflict - subscriber is deactivated"
// @Router       /subscribers/{id}/tokens [post]
func (h IssueTokenHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	token, plaintext, err := h.Svc.IssueToken(r.Context(), id)
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	// D-5: this response is the only place the plaintext token and the
	// subscription URL ever appear. Only the hash is persisted, so neither
	// can be reconstructed afterwards — the dashboard must show them now
	// or lose them.
	respond.JSON(w, http.StatusCreated, IssuedTokenDTO{
		TokenDTO: toTokenDTO(token),
		Token:    plaintext,
		FeedURL:  fmt.Sprintf("%s/feeds/%s/feed.xml", h.PublicBaseURL, plaintext),
	})
}

type ListTokensHandler struct{ Svc subUC.Service }

// ServeHTTP トークン一覧取得
// @Summary      トークン一覧取得
// @Description  友人のフィードトークンを新しい順にすべて取得します。返るのは ID・発行日時・失効日時・状態のみで、平文はもちろんハッシュも含まれません(D-5)
// @Tags         tokens
// @Security     BearerAuth
// @Produce      json
// @Param        id path int true "友人ID"
// @Success      200 {array} TokenDTO "トークン一覧(平文・ハッシュは含まない)"
// @Failure      400 {string} string "Bad request - invalid ID"
// @Failure      401 {string} string "Authentication required"
// @Failure      404 {string} string "Not found - subscriber not found"
// @Router       /subscribers/{id}/tokens [get]
func (h ListTokensHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	tokens, err := h.Svc.ListTokens(r.Context(), id)
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	out := make([]TokenDTO, 0, len(tokens))
	for _, t := range tokens {
		out = append(out, toTokenDTO(t))
	}
	respond.JSON(w, http.StatusOK, out)
}

type RevokeTokenHandler struct{ Svc subUC.Service }

// ServeHTTP トークン失効
// @Summary      トークン失効
// @Description  トークンを失効させます(revoked_at 更新のみ、§5.2)。**失効は不可逆**で、復活はできません。アクセスを回復するには新しいトークンを発行してください。冪等: 失効済みトークンへの再実行は元の失効日時をそのまま返します
// @Tags         tokens
// @Security     BearerAuth
// @Produce      json
// @Param        id path int true "トークンID"
// @Success      200 {object} RevokedTokenDTO "失効後のトークン(note で不可逆であることを明示)"
// @Failure      400 {string} string "Bad request - invalid ID"
// @Failure      401 {string} string "Authentication required"
// @Failure      404 {string} string "Not found - token not found"
// @Router       /tokens/{id} [delete]
func (h RevokeTokenHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		respond.SafeError(w, http.StatusBadRequest, err)
		return
	}
	token, err := h.Svc.RevokeToken(r.Context(), id)
	if err != nil {
		respondUsecaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, RevokedTokenDTO{
		TokenDTO: toTokenDTO(token),
		Note:     revokeNote,
	})
}
