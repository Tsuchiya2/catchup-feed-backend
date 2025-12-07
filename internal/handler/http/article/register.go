package article

import (
	"log/slog"
	"net/http"

	"catchup-feed/internal/common/pagination"
	"catchup-feed/internal/handler/http/auth"
	artUC "catchup-feed/internal/usecase/article"
)

// Register registers all article-related HTTP handlers with the given mux.
// It sets up routes for listing, searching, creating, updating, and deleting articles.
// Protected routes (create, update, delete) require authentication via the auth middleware.
func Register(mux *http.ServeMux, svc artUC.Service, paginationCfg pagination.Config, logger *slog.Logger) {
	mux.Handle("GET    /articles", ListHandler{
		Svc:           svc,
		PaginationCfg: paginationCfg,
		Logger:        logger,
	})
	mux.Handle("GET    /articles/search", SearchHandler{svc})
	mux.Handle("GET    /articles/", auth.Authz(GetHandler{svc}))

	mux.Handle("POST   /articles", auth.Authz(CreateHandler{svc}))
	mux.Handle("PUT    /articles/", auth.Authz(UpdateHandler{svc}))
	mux.Handle("DELETE /articles/", auth.Authz(DeleteHandler{svc}))
}
