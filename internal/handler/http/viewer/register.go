package viewer

import (
	"net/http"

	"catchup-feed/internal/handler/http/auth"
	viewerUC "catchup-feed/internal/usecase/viewer"
)

// Register registers the viewer-management routes (D-27, C-21 flat paths).
// Every route is wrapped in auth.Authz: viewer management is admin-only —
// the explicit admin-only wrap keeps that true even if the mux is ever
// mounted without the outer role-aware middleware, and the outer
// middleware's allowlist independently keeps viewers out (belt and
// braces).
func Register(mux *http.ServeMux, svc *viewerUC.Service) {
	mux.Handle("GET /viewers", auth.Authz(ListHandler{svc}))
	mux.Handle("POST /viewers", auth.Authz(CreateHandler{svc}))
	mux.Handle("PUT /viewers/{id}", auth.Authz(UpdateHandler{svc}))
	mux.Handle("PUT /viewers/{id}/active", auth.Authz(SetActiveHandler{svc}))
	mux.Handle("DELETE /viewers/{id}", auth.Authz(DeleteHandler{svc}))
}
