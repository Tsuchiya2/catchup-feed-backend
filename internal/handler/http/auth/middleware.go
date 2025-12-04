package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"catchup-feed/internal/handler/http/requestid"
	"catchup-feed/internal/handler/http/respond"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey string

const ctxUser ctxKey = "user"

// Authz is an authorization middleware that requires JWT authentication
// for all HTTP methods on protected endpoints.
//
// Authorization Logic:
// 1. Check if the endpoint is public (health checks, metrics, swagger, auth)
//   - If public: Allow access without JWT validation
//
// 2. If protected: Require valid JWT token for ALL methods (GET, POST, PUT, DELETE, etc.)
//   - Extract and validate JWT from Authorization header
//   - Verify role-based permissions using checkRolePermission
//   - Add user to request context
//
// Role-Based Authorization:
// - Admin: Full access to all endpoints and methods (GET, POST, PUT, DELETE, etc.)
// - Viewer: Read-only access (GET) to articles, sources, and swagger endpoints
// - Permission checks use role + method + path combination
//
// Security Note:
// This middleware fixes CVE-CATCHUP-2024-002 (Authorization Bypass for GET Requests).
// Previous implementation allowed GET requests to bypass JWT validation, making
// list/search APIs publicly accessible.
//
// Breaking Change:
// GET requests to protected endpoints (/articles, /sources) now require authentication.
// Clients must provide valid JWT tokens for all requests to protected resources.
func Authz(next http.Handler) http.Handler {
	secret := []byte(os.Getenv("JWT_SECRET"))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Step 1: Check if endpoint is public
		// Public endpoints are accessible without authentication
		if IsPublicEndpoint(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		// Step 2: Protected endpoint - require JWT for ALL methods
		user, role, err := validateJWT(r.Header.Get("Authorization"), secret)
		if err != nil {
			respond.SafeError(w, http.StatusUnauthorized, fmt.Errorf("unauthorized: %w", err))
			return
		}

		// Get request ID for logging
		requestID := requestid.FromContext(r.Context())
		logger := slog.With(
			slog.String("request_id", requestID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)

		// Log authorization check started
		logger.Debug("authorization check started", slog.String("role", role))

		// Step 3: Check if user has permission for this request
		// Track authorization check duration
		authzStart := time.Now()
		hasPermission := checkRolePermission(role, r.Method, r.URL.Path)
		RecordAuthzCheckDuration(time.Since(authzStart).Seconds())

		if !hasPermission {
			// Record forbidden access attempt
			RecordForbiddenAttempt(role, r.Method)

			logger.Warn("authorization denied",
				slog.String("user_email", user),
				slog.String("role", role),
				slog.String("reason", "insufficient_permissions"))
			respond.SafeError(w, http.StatusForbidden, fmt.Errorf("forbidden: %s role cannot perform %s operations", role, r.Method))
			return
		}

		// Log successful authorization
		logger.Info("authorization granted",
			slog.String("user_email", user),
			slog.String("role", role))

		ctx := context.WithValue(r.Context(), ctxUser, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validateJWT(authz string, secret []byte) (string, string, error) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authz, prefix) {
		return "", "", errors.New("missing bearer token")
	}
	tokenString := strings.TrimPrefix(authz, prefix)
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
	sub, ok := claims["sub"].(string)
	if !ok {
		return "", "", errors.New("invalid sub claim")
	}
	role, ok := claims["role"].(string)
	if !ok {
		return "", "", errors.New("invalid role claim")
	}
	return sub, role, nil
}
