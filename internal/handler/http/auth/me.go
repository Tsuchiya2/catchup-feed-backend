package auth

import (
	"net/http"

	"catchup-feed/internal/handler/http/respond"
)

// MeResponse is the GET /auth/me body: the authenticated identity as the
// middleware resolved it. The frontend cannot read the HttpOnly cookie
// (D-22), so this endpoint is its only way to learn the current role
// (D-27 (5)).
type MeResponse struct {
	Sub  string `json:"sub" example:"friend@example.com"`
	Role string `json:"role" example:"viewer" enums:"admin,viewer"`
}

// MeHandler returns the authenticated user's subject and role. It must be
// mounted behind the auth middleware (it reads the identity from the
// request context); it is on the viewer allowlist, so both roles can call
// it.
//
// @Summary      認証情報取得
// @Description  認証済みユーザーの識別子(sub)とロール(admin / viewer)を返します。
// @Description  JWT は HttpOnly cookie のため JS から読めず、frontend が自分のロールを
// @Description  知る唯一の手段です(D-27 (5)、D-22)。admin / viewer の両ロールが呼べます。
// @Tags         auth
// @Security     BearerAuth
// @Produce      json
// @Success      200 {object} MeResponse "認証済みユーザーの sub と role"
// @Failure      401 {object} respond.ErrorResponse "Authentication required"
// @Failure      403 {object} respond.ErrorResponse "Forbidden - role クレームなし・未知 role・無効化済み viewer"
// @Router       /auth/me [get]
func MeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		respond.JSON(w, http.StatusOK, MeResponse{
			Sub:  SubjectFromContext(r.Context()),
			Role: RoleFromContext(r.Context()),
		})
	}
}
