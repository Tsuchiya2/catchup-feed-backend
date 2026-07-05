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

type ctxKey string

const ctxUser ctxKey = "user"

// Authz is an authorization middleware that requires JWT authentication for
// all HTTP methods on protected endpoints.
//
// Authorization Logic:
//  1. Public endpoints (see PublicEndpoints) pass through without a token.
//  2. Everything else requires a valid HS256 JWT for ALL methods.
//  3. The token's sub claim must equal the configured administrator
//     (ADMIN_USER). pulse is a single-admin system (C-20): there are no
//     roles, and any validly-signed token whose subject is not the
//     administrator — e.g. a leftover "viewer" token from the old system or
//     a token with tampered claims — is rejected with 403.
//
// Security Note:
// This middleware fixes CVE-CATCHUP-2024-002 (Authorization Bypass for GET
// Requests): GET requests to protected endpoints require authentication.
//
// JWT_SECRET and ADMIN_USER are read when the middleware is constructed, so
// Authz must be called after startup validation (ValidateAdminCredentials
// for ADMIN_USER; JWT_SECRET is validated by cmd/server's validateJWTSecret).
func Authz(next http.Handler) http.Handler {
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
		sub, err := validateJWT(r.Header.Get("Authorization"), secret)
		if err != nil {
			respond.SafeError(w, http.StatusUnauthorized, fmt.Errorf("unauthorized: %w", err))
			return
		}

		// Step 3: Single-admin check. A validly-signed token whose subject
		// is not the administrator must not reach the admin API.
		if subtle.ConstantTimeCompare([]byte(sub), []byte(adminUser)) != 1 {
			logger.Warn("authorization denied",
				slog.String("user_email", sub),
				slog.String("reason", "subject_is_not_admin"))
			respond.SafeError(w, http.StatusForbidden, errors.New("forbidden"))
			return
		}

		logger.Debug("authorization granted", slog.String("user_email", sub))

		ctx := context.WithValue(r.Context(), ctxUser, sub)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// validateJWT parses and validates a Bearer token and returns its subject.
// It enforces HS256, a valid signature, the presence of exp (not yet
// expired) and a non-empty sub claim.
func validateJWT(authz string, secret []byte) (string, error) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authz, prefix) {
		return "", errors.New("missing bearer token")
	}
	tokenString := strings.TrimPrefix(authz, prefix)
	tok, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil || !tok.Valid {
		return "", errors.New("invalid token")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return "", errors.New("invalid claims")
	}
	if exp, ok := claims["exp"].(float64); !ok || int64(exp) < time.Now().Unix() {
		return "", errors.New("token expired")
	}
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		return "", errors.New("invalid sub claim")
	}
	return sub, nil
}
