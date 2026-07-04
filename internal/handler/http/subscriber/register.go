package subscriber

import (
	"net/http"

	"catchup-feed/internal/handler/http/auth"
	subUC "catchup-feed/internal/usecase/subscriber"
)

// Register registers the friend-management routes (§5.1). Every route —
// reads included — is wrapped in auth.Authz: subscriber and token
// management is admin-only (the viewer role's path allowlist excludes
// these paths), and the explicit wrap keeps that true even if the mux is
// ever mounted without the outer Authz. publicBaseURL is
// feed.Config.PublicBaseURL, used to build the one-time subscription URL
// at token issue (D-5).
func Register(mux *http.ServeMux, svc subUC.Service, publicBaseURL string) {
	mux.Handle("GET /subscribers", auth.Authz(ListHandler{svc}))
	mux.Handle("POST /subscribers", auth.Authz(CreateHandler{svc}))
	mux.Handle("GET /subscribers/{id}", auth.Authz(GetHandler{svc}))
	mux.Handle("PUT /subscribers/{id}", auth.Authz(UpdateHandler{svc}))
	mux.Handle("DELETE /subscribers/{id}", auth.Authz(DeleteHandler{svc}))

	mux.Handle("POST /subscribers/{id}/tokens", auth.Authz(IssueTokenHandler{Svc: svc, PublicBaseURL: publicBaseURL}))
	mux.Handle("GET /subscribers/{id}/tokens", auth.Authz(ListTokensHandler{svc}))
	mux.Handle("DELETE /tokens/{id}", auth.Authz(RevokeTokenHandler{svc}))
}
