package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"catchup-feed/internal/handler/http/requestid"
	authservice "catchup-feed/internal/service/auth"

	"github.com/golang-jwt/jwt/v5"
)

type loginRequest struct {
	Email    string `json:"email" example:"admin@example.com"`
	Password string `json:"password" example:"your_password"`
}

type tokenResponse struct {
	Token string `json:"token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
}

// tokenTTL is the lifetime of an issued JWT.
const tokenTTL = 1 * time.Hour

// TokenHandler creates an HTTP handler that authenticates the administrator
// and issues a JWT. pulse is a single-admin system (C-7/C-20): the token
// carries only sub/iat/exp claims and no role.
//
// Unlike the admin API handlers (respond.SafeError -> JSON
// {"error": "..."}), this endpoint replies to failures with http.Error
// (text/plain) - the 429 likewise comes from the rate-limit middleware as
// text/plain. The @Failure annotations below stay {string} on purpose so
// the spec matches the wire format the frontend already handles.
//
// @Summary      JWT トークン取得
// @Description  管理者のユーザー名とパスワードで認証し、JWT トークンを発行します。
// @Description  JSON body の token(dev の Bearer フォールバック用に後方互換で維持)に加え、
// @Description  同じ JWT を HttpOnly / Secure / SameSite=Strict の cookie
// @Description  (catchup_feed_auth_token)で Set-Cookie します(D-22)。
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request body loginRequest true "ログイン情報"
// @Success      200 {object} tokenResponse "JWT トークン(併せて Set-Cookie: catchup_feed_auth_token を返す)"
// @Header       200 {string} Set-Cookie "catchup_feed_auth_token=<jwt>; HttpOnly; Secure; SameSite=Strict; Path=/"
// @Failure      400 {string} string "リクエストが不正"
// @Failure      401 {string} string "認証失敗"
// @Failure      429 {string} string "Too many requests - rate limit exceeded"
// @Failure      500 {string} string "トークン生成失敗"
// @Router       /auth/token [post]
func TokenHandler(authService *authservice.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		requestID := requestid.FromContext(r.Context())
		logger := slog.With(slog.String("request_id", requestID))

		logger.Info("authentication attempt started")

		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			logger.Warn("authentication failed",
				slog.String("reason", "invalid_request"),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()))
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		creds := authservice.Credentials{
			Username: req.Email, // Use Email field, map to Username internally
			Password: req.Password,
		}

		if err := authService.ValidateCredentials(r.Context(), creds); err != nil {
			logger.Warn("authentication failed",
				slog.String("reason", "invalid_credentials"),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		secret := []byte(os.Getenv("JWT_SECRET"))

		now := time.Now()
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub": req.Email,
			"iat": now.Unix(),
			"exp": now.Add(tokenTTL).Unix(),
		})

		signed, err := token.SignedString(secret)
		if err != nil {
			logger.Error("token generation failed",
				slog.String("error", err.Error()),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()))
			http.Error(w, "token generation failed", http.StatusInternalServerError)
			return
		}

		logger.Info("authentication successful",
			slog.String("user_email", req.Email),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()))

		// Issue the JWT as an HttpOnly cookie so the browser never exposes it
		// to JavaScript (mitigates XSS token theft, D-22). SetCookie must run
		// before WriteHeader (JSON encode below writes the header).
		http.SetCookie(w, newAuthCookie(signed, tokenTTL))

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(tokenResponse{Token: signed}); err != nil {
			logger.Error("failed to encode token response",
				slog.String("error", err.Error()))
		}
	}
}

// LogoutHandler clears the auth cookie. Because the cookie is HttpOnly it
// cannot be removed from JavaScript, so logout is a server round-trip that
// emits an expiring Set-Cookie (D-22). It is idempotent and does not require
// authentication: clearing a cookie is always safe, and requiring a valid JWT
// to log out would strand a user whose token has already expired.
//
// @Summary      ログアウト(cookie 失効)
// @Description  HttpOnly の認証 cookie(catchup_feed_auth_token)を Max-Age=0 で失効させます。
// @Description  HttpOnly cookie は JS から削除できないため backend で失効させます(D-22)。
// @Description  認証不要・冪等。
// @Tags         auth
// @Produce      json
// @Success      204 "ログアウト成功(cookie 失効)"
// @Header       204 {string} Set-Cookie "catchup_feed_auth_token=; Max-Age=0; HttpOnly; Secure; SameSite=Strict; Path=/"
// @Router       /auth/logout [post]
func LogoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, expiredAuthCookie())
		w.WriteHeader(http.StatusNoContent)
	}
}
