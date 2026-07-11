package book

import (
	"net/http"

	"catchup-feed/internal/handler/http/auth"
	bookUC "catchup-feed/internal/usecase/book"
)

// Register registers the book management routes (D-25, C-21 フラット構成).
// Every route is wrapped in auth.Authz: book management requires the
// administrator's JWT, and the explicit wrap keeps that true even if the
// mux is ever mounted without the outer Authz.
func Register(mux *http.ServeMux, svc *bookUC.Service) {
	mux.Handle("GET /books", auth.Authz(ListHandler{Svc: svc}))
	mux.Handle("POST /books", auth.Authz(UploadHandler{Svc: svc}))
	mux.Handle("DELETE /books/{filename}", auth.Authz(DeleteHandler{Svc: svc}))
}
