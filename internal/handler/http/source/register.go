package source

import (
	"net/http"

	"catchup-feed/internal/handler/http/auth"
	"catchup-feed/internal/handler/http/middleware"
	srcUC "catchup-feed/internal/usecase/source"
)

// Register registers all source-related HTTP handlers with the given mux.
// It sets up routes for listing, searching, creating, updating, and deleting sources.
// Protected routes (create, update, delete) require authentication via the auth middleware.
// Search endpoints are protected by rate limiting to prevent DoS attacks.
func Register(mux *http.ServeMux, svc srcUC.Service, searchRateLimiter *middleware.RateLimiter) {
	mux.Handle("GET    /sources", ListHandler{svc})
	// Search endpoint with rate limiting (100 req/min per IP)
	mux.Handle("GET    /sources/search", searchRateLimiter.Middleware(SearchHandler{svc}))

	mux.Handle("POST   /sources", auth.Authz(CreateHandler{svc}))
	mux.Handle("PUT    /sources/", auth.Authz(UpdateHandler{svc}))
	mux.Handle("DELETE /sources/", auth.Authz(DeleteHandler{svc}))
}
