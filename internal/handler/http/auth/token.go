package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	authservice "catchup-feed/internal/service/auth"
	"catchup-feed/internal/handler/http/requestid"

	"github.com/golang-jwt/jwt/v5"
)

type loginRequest struct {
	Email    string `json:"email" example:"admin@example.com"`
	Password string `json:"password" example:"your_password"`
}

type tokenResponse struct {
	Token string `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
}

// TokenHandler creates an HTTP handler that authenticates users and issues JWT tokens.
// It uses the provided AuthService for credential validation.
//
// @Summary      JWT トークン取得
// @Description  ユーザー名とパスワードで認証し、JWT トークンを発行します
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request body loginRequest true "ログイン情報"
// @Success      200 {object} tokenResponse "JWT トークン"
// @Header       200 {integer} X-RateLimit-Limit "Maximum number of requests allowed in the current window"
// @Header       200 {integer} X-RateLimit-Remaining "Number of requests remaining in the current window"
// @Header       200 {integer} X-RateLimit-Reset "Unix timestamp when the rate limit window resets"
// @Failure      400 {string} string "リクエストが不正"
// @Failure      401 {string} string "認証失敗"
// @Failure      429 {string} string "Too many requests - rate limit exceeded"
// @Header       429 {integer} X-RateLimit-Limit "Maximum number of requests allowed in the current window"
// @Header       429 {integer} X-RateLimit-Remaining "Number of requests remaining (should be 0)"
// @Header       429 {integer} X-RateLimit-Reset "Unix timestamp when the rate limit window resets"
// @Header       429 {integer} Retry-After "Seconds until the client should retry"
// @Failure      500 {string} string "トークン生成失敗"
// @Router       /auth/token [post]
func TokenHandler(authService *authservice.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Get request ID from context
		requestID := requestid.FromContext(r.Context())
		logger := slog.With(slog.String("request_id", requestID))

		logger.Info("authentication attempt started")

		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Warn("authentication failed",
				slog.String("reason", "invalid_request"),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()))
			// Record metrics
			RecordAuthRequest("unknown", "failure")
			RecordAuthDuration("unknown", time.Since(start).Seconds())
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		// Validate credentials using AuthService
		creds := authservice.Credentials{
			Username: req.Email, // Use Email field, map to Username internally
			Password: req.Password,
		}

		if err := authService.ValidateCredentials(r.Context(), creds); err != nil {
			logger.Warn("authentication failed",
				slog.String("reason", "invalid_credentials"),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()))
			// Record metrics
			RecordAuthRequest("unknown", "failure")
			RecordAuthDuration("unknown", time.Since(start).Seconds())
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Get user role
		role, err := authService.GetProvider().IdentifyUser(r.Context(), req.Email)
		if err != nil {
			logger.Warn("authentication failed",
				slog.String("reason", "role_identification_failed"),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()))
			// Record metrics
			RecordAuthRequest("unknown", "failure")
			RecordAuthDuration("unknown", time.Since(start).Seconds())
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Generate JWT token
		secret := []byte(os.Getenv("JWT_SECRET"))

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":  req.Email, // Use email in subject claim
			"role": role,      // Use role from IdentifyUser instead of hardcoded "admin"
			"exp":  time.Now().Add(1 * time.Hour).Unix(),
		})

		signed, err := token.SignedString(secret)
		if err != nil {
			logger.Error("token generation failed",
				slog.String("error", err.Error()),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()))
			// Record metrics
			RecordAuthRequest(role, "failure")
			RecordAuthDuration(role, time.Since(start).Seconds())
			http.Error(w, "token generation failed", http.StatusInternalServerError)
			return
		}

		logger.Info("authentication successful",
			slog.String("user_email", req.Email),
			slog.String("role", role),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()))

		// Record metrics
		RecordAuthRequest(role, "success")
		RecordAuthDuration(role, time.Since(start).Seconds())

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(tokenResponse{Token: signed}); err != nil {
			logger.Error("failed to encode token response",
				slog.String("error", err.Error()))
		}
	}
}
