package auth

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

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
// @Failure      400 {string} string "リクエストが不正"
// @Failure      401 {string} string "認証失敗"
// @Failure      500 {string} string "トークン生成失敗"
// @Router       /auth/token [post]
func TokenHandler(authService *authservice.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		// Validate credentials using AuthService
		creds := authservice.Credentials{
			Username: req.Email, // Use Email field, map to Username internally
			Password: req.Password,
		}

		if err := authService.ValidateCredentials(r.Context(), creds); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Generate JWT token
		secret := []byte(os.Getenv("JWT_SECRET"))

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":  req.Email, // Use email in subject claim
			"role": "admin",
			"exp":  time.Now().Add(1 * time.Hour).Unix(),
		})

		signed, err := token.SignedString(secret)
		if err != nil {
			http.Error(w, "token generation failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(tokenResponse{Token: signed}); err != nil {
			log.Printf("auth: failed to encode token response: %v", err)
		}
	}
}
