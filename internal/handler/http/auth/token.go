package auth

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"catchup-feed/internal/handler/http/requestid"
	authservice "catchup-feed/internal/service/auth"
	viewerUC "catchup-feed/internal/usecase/viewer"

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

// ViewerAuthenticator validates a viewer login (email + bcrypt password
// against the viewers table, D-27 (2)). It must reject deactivated viewers.
// Credential mismatches (unknown email / wrong password / deactivated) must
// be reported as usecase/viewer.ErrInvalidCredentials; any other error is
// treated as an infrastructure failure (DB down) and logged as such — the
// HTTP response is 401 either way so failures don't enumerate accounts.
// Implemented by usecase/viewer.Service.
type ViewerAuthenticator interface {
	Authenticate(ctx context.Context, email, password string) error
}

// TokenHandler creates an HTTP handler that authenticates a user and issues
// a JWT. Credentials are checked against the administrator first (C-7: env
// + bcrypt); on mismatch they fall through to the viewers table (D-27 (2),
// email + bcrypt; deactivated viewers are rejected). The issued token
// carries sub/iat/exp plus the role claim (admin / viewer). viewers may be
// nil to disable viewer login entirely (admin-only issuance).
//
// Unlike the admin API handlers (respond.SafeError -> JSON
// {"error": "..."}), this endpoint replies to failures with http.Error
// (text/plain) - the 429 likewise comes from the rate-limit middleware as
// text/plain. The @Failure annotations below stay {string} on purpose so
// the spec matches the wire format the frontend already handles.
//
// @Summary      JWT トークン取得
// @Description  メールアドレスとパスワードで認証し、JWT トークンを発行します。
// @Description  まず管理者(環境変数+bcrypt)と照合し、不一致なら viewers テーブルの
// @Description  アクティブな閲覧専用アカウントと照合します(D-27。無効化済み viewer は拒否)。
// @Description  発行する JWT には role クレーム(admin / viewer)が入ります。
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
func TokenHandler(authService *authservice.AuthService, viewers ViewerAuthenticator) http.HandlerFunc {
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

		// 管理者を先に照合し、不一致なら viewer にフォールバック(D-27 (2))。
		// 失敗レスポンスはどちらの照合で落ちたかを区別しない(401 固定)。
		// ログのみ、資格情報不一致とインフラ障害(DB エラー等)を区別する。
		role := RoleAdmin
		if err := authService.ValidateCredentials(r.Context(), creds); err != nil {
			viewerErr := err
			if viewers != nil {
				viewerErr = viewers.Authenticate(r.Context(), req.Email, req.Password)
			}
			if viewerErr != nil {
				if viewers != nil && !errors.Is(viewerErr, viewerUC.ErrInvalidCredentials) {
					logger.Error("authentication failed",
						slog.String("reason", "viewer_lookup_failed"),
						slog.String("error", viewerErr.Error()),
						slog.Int64("duration_ms", time.Since(start).Milliseconds()))
				} else {
					logger.Warn("authentication failed",
						slog.String("reason", "invalid_credentials"),
						slog.Int64("duration_ms", time.Since(start).Milliseconds()))
				}
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			role = RoleViewer
		}

		secret := []byte(os.Getenv("JWT_SECRET"))

		now := time.Now()
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":  req.Email,
			"role": role,
			"iat":  now.Unix(),
			"exp":  now.Add(tokenTTL).Unix(),
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
			slog.String("role", role),
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
