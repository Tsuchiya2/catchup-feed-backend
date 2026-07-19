package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"catchup-feed/internal/handler/http/pathutil"
	"catchup-feed/internal/handler/http/requestid"
	"catchup-feed/internal/handler/http/respond"

	"github.com/golang-jwt/jwt/v5"
)

// Roles carried by the JWT role claim (D-27). Exactly two values exist —
// pulse deliberately has no permission framework beyond this (単一ユーザー
// 右サイズ).
const (
	// RoleAdmin is the single administrator (C-7: env + bcrypt, no DB row).
	RoleAdmin = "admin"
	// RoleViewer is a read-only friend account (viewers table, D-27).
	RoleViewer = "viewer"
)

type ctxKey string

const (
	ctxUser ctxKey = "user"
	ctxRole ctxKey = "role"
)

// WithIdentity returns a context carrying the authenticated subject and
// role. Exposed for handler tests; production code only sets it from the
// middleware in this package.
func WithIdentity(ctx context.Context, sub, role string) context.Context {
	ctx = context.WithValue(ctx, ctxUser, sub)
	return context.WithValue(ctx, ctxRole, role)
}

// SubjectFromContext returns the authenticated subject (admin user name or
// viewer email), or "" when the request did not pass the auth middleware.
func SubjectFromContext(ctx context.Context) string {
	sub, _ := ctx.Value(ctxUser).(string)
	return sub
}

// RoleFromContext returns RoleAdmin / RoleViewer, or "" when the request
// did not pass the auth middleware.
func RoleFromContext(ctx context.Context) string {
	role, _ := ctx.Value(ctxRole).(string)
	return role
}

// ViewerVerifier re-validates a viewer on every request (D-27 (4)): the
// viewer must still exist and be active (deactivated_at IS NULL) so
// deactivation takes effect immediately instead of waiting for JWT expiry.
// Implemented by usecase/viewer.Service.
type ViewerVerifier interface {
	IsActiveViewer(ctx context.Context, email string) (bool, error)
}

// viewerAllowedRoutes is the closed allowlist of "METHOD path" routes a
// viewer may reach (D-27 (3)). Everything else is admin-only by default —
// a newly added endpoint is never reachable by viewers unless it is
// explicitly listed here. POST /auth/logout is not listed because it is a
// public endpoint (D-22) and never reaches the middleware.
var viewerAllowedRoutes = map[string]struct{}{
	"GET /sources": {},
	"GET /auth/me": {},
}

// viewerAllowed reports whether a viewer may reach method+path. A single
// trailing slash is tolerated (mirrors IsPublicEndpoint's normalization).
func viewerAllowed(method, path string) bool {
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	_, ok := viewerAllowedRoutes[method+" "+path]
	return ok
}

// Authz is the admin-only authorization middleware used to wrap individual
// admin routes. It authenticates the JWT and requires role=admin with the
// administrator's subject; viewer tokens are rejected with 403 here
// regardless of path. Use AuthzWithViewer for the outer mux wrapper that
// additionally admits viewers to their allowlisted read-only routes.
//
// Authorization Logic:
//  1. Public endpoints (see PublicEndpoints) pass through without a token.
//  2. Everything else requires a valid HS256 JWT for ALL methods.
//  3. The token must carry role=admin (D-27; tokens without a role claim —
//     pre-D-27 tokens — and unknown roles are rejected with 403: the C-20
//     regression rule re-read for the two-role world) and its sub claim
//     must equal the configured administrator (ADMIN_USER, constant-time).
//
// Security Note:
// This middleware fixes CVE-CATCHUP-2024-002 (Authorization Bypass for GET
// Requests): GET requests to protected endpoints require authentication.
//
// JWT_SECRET and ADMIN_USER are read when the middleware is constructed, so
// Authz must be called after startup validation (ValidateAdminCredentials
// for ADMIN_USER; JWT_SECRET is validated by cmd/server's validateJWTSecret).
func Authz(next http.Handler) http.Handler {
	return newAuthz(nil, next)
}

// AuthzWithViewer builds the role-aware authorization middleware that wraps
// the whole protected mux in cmd/server. Admins pass to every route (the
// same checks as Authz); viewers are re-validated against the DB on every
// request and then confined to the viewerAllowedRoutes allowlist (D-27).
func AuthzWithViewer(viewers ViewerVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return newAuthz(viewers, next)
	}
}

// newAuthz is the shared implementation. viewers == nil means admin-only:
// any viewer token is rejected with 403.
func newAuthz(viewers ViewerVerifier, next http.Handler) http.Handler {
	secret := []byte(os.Getenv("JWT_SECRET"))
	adminUser := os.Getenv(EnvAdminUser)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Public endpoints are accessible without authentication.
		if IsPublicEndpoint(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		requestID := requestid.FromContext(r.Context())
		logger := slog.With(
			slog.String("request_id", requestID),
			slog.String("method", r.Method),
			slog.String("path", pathutil.RedactPath(r.URL.Path)),
		)

		// Fail closed when the administrator or the signing key is not
		// configured. An empty HS256 key would let anyone forge a validly
		// signed token. Startup validation makes both branches unreachable
		// in a correctly booted server.
		if adminUser == "" {
			logger.Error("authorization denied", slog.String("reason", "admin_user_not_configured"))
			respond.SafeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
			return
		}
		if len(secret) == 0 {
			logger.Error("authorization denied", slog.String("reason", "jwt_secret_not_configured"))
			respond.SafeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
			return
		}

		// Step 2: Protected endpoint - require a valid JWT for ALL methods.
		// The token is read from the HttpOnly cookie first (D-22) and falls
		// back to the Authorization: Bearer header (dev / API clients). Both
		// paths feed the identical HS256/exp/sub verification below.
		tokenString, err := extractToken(r)
		if err != nil {
			respond.SafeError(w, http.StatusUnauthorized, fmt.Errorf("unauthorized: %w", err))
			return
		}
		sub, role, err := validateJWT(tokenString, secret)
		if err != nil {
			respond.SafeError(w, http.StatusUnauthorized, fmt.Errorf("unauthorized: %w", err))
			return
		}

		// Step 3: Role-based authorization (D-27). A missing role claim
		// (pre-D-27 token) or an unknown role is rejected with 403 — the
		// C-20 regression rule re-read for the two-role world.
		switch role {
		case RoleAdmin:
			// Single-admin check (C-7): a validly-signed admin token whose
			// subject is not the administrator must not reach the admin API.
			if subtle.ConstantTimeCompare([]byte(sub), []byte(adminUser)) != 1 {
				logger.Warn("authorization denied",
					slog.String("user_email", sub),
					slog.String("reason", "subject_is_not_admin"))
				respond.SafeError(w, http.StatusForbidden, errors.New("forbidden"))
				return
			}

		case RoleViewer:
			if viewers == nil {
				// Admin-only wrapper: viewers never pass, whatever the path.
				logger.Warn("authorization denied",
					slog.String("user_email", sub),
					slog.String("reason", "viewer_on_admin_route"))
				respond.SafeError(w, http.StatusForbidden, errors.New("forbidden"))
				return
			}
			// D-27 (4): re-validate against the DB on every request so
			// deactivation (or deletion) cuts off existing JWTs immediately.
			active, err := viewers.IsActiveViewer(r.Context(), sub)
			if err != nil {
				logger.Error("viewer re-validation failed", slog.Any("error", err))
				respond.SafeError(w, http.StatusInternalServerError, errors.New("internal error"))
				return
			}
			if !active {
				logger.Warn("authorization denied",
					slog.String("user_email", sub),
					slog.String("reason", "viewer_deactivated_or_deleted"))
				respond.SafeError(w, http.StatusForbidden, errors.New("forbidden"))
				return
			}
			// D-27 (3): viewers only reach the closed read-only allowlist.
			if !viewerAllowed(r.Method, r.URL.Path) {
				logger.Warn("authorization denied",
					slog.String("user_email", sub),
					slog.String("reason", "viewer_route_not_allowed"))
				respond.SafeError(w, http.StatusForbidden, errors.New("forbidden"))
				return
			}

		default:
			logger.Warn("authorization denied",
				slog.String("user_email", sub),
				slog.String("role", role),
				slog.String("reason", "missing_or_unknown_role"))
			respond.SafeError(w, http.StatusForbidden, errors.New("forbidden"))
			return
		}

		logger.Debug("authorization granted",
			slog.String("user_email", sub), slog.String("role", role))

		next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), sub, role)))
	})
}

// extractToken pulls the raw JWT string from the request. Precedence (D-22):
//  1. The HttpOnly cookie catchup_feed_auth_token (browser dashboard).
//  2. The Authorization: Bearer header (dev / non-browser API clients).
//
// The cookie is only used when present and non-empty; otherwise the Bearer
// header is used as a complete fallback, so the pre-existing Bearer behaviour
// is preserved unchanged.
func extractToken(r *http.Request) (string, error) {
	if c, err := r.Cookie(authCookieName); err == nil && c.Value != "" {
		return c.Value, nil
	}

	const prefix = "Bearer "
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, prefix) {
		return "", errors.New("missing bearer token")
	}
	return strings.TrimPrefix(authz, prefix), nil
}

// validateJWT parses and validates a raw JWT string and returns its subject
// and role. It enforces HS256, a valid signature, the presence of exp (not
// yet expired) and a non-empty sub claim. The role claim is returned as-is
// ("" when absent); role-based rejection is the caller's job so 401
// (broken token) and 403 (valid token, wrong role) stay distinct.
func validateJWT(tokenString string, secret []byte) (sub, role string, err error) {
	tok, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil || !tok.Valid {
		return "", "", errors.New("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", errors.New("invalid claims")
	}
	if exp, ok := claims["exp"].(float64); !ok || int64(exp) < time.Now().Unix() {
		return "", "", errors.New("token expired")
	}
	sub, ok = claims["sub"].(string)
	if !ok || sub == "" {
		return "", "", errors.New("invalid sub claim")
	}
	role, _ = claims["role"].(string)
	return sub, role, nil
}
