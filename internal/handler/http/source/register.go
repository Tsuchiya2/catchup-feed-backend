package source

import (
	"net/http"

	"catchup-feed/internal/handler/http/auth"
	srcUC "catchup-feed/internal/usecase/source"
)

func Register(mux *http.ServeMux, svc srcUC.Service) {
	mux.Handle("GET    /sources", ListHandler{svc})
	mux.Handle("GET    /sources/search", SearchHandler{svc})

	mux.Handle("POST   /sources", auth.Authz(CreateHandler{svc}))
	mux.Handle("PUT    /sources/", auth.Authz(UpdateHandler{svc}))
	mux.Handle("DELETE /sources/", auth.Authz(DeleteHandler{svc}))
}
