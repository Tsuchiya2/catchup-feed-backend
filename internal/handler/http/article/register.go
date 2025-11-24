package article

import (
	"net/http"

	"catchup-feed/internal/handler/http/auth"
	artUC "catchup-feed/internal/usecase/article"
)

// Register registers all article-related HTTP handlers with the given mux.
// It sets up routes for listing, searching, creating, updating, and deleting articles.
// Protected routes (create, update, delete) require authentication via the auth middleware.
func Register(mux *http.ServeMux, svc artUC.Service) {
	mux.Handle("GET    /articles", ListHandler{svc})
	mux.Handle("GET    /articles/search", SearchHandler{svc})

	mux.Handle("POST   /articles", auth.Authz(CreateHandler{svc}))
	mux.Handle("PUT    /articles/", auth.Authz(UpdateHandler{svc}))
	mux.Handle("DELETE /articles/", auth.Authz(DeleteHandler{svc}))
}
