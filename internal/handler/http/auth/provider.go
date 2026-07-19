package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"

	authservice "catchup-feed/internal/service/auth"

	"golang.org/x/crypto/bcrypt"
)

// Environment variable names for the administrator credentials (C-7:
// 単一管理者、users テーブルなし。環境変数+bcrypt ハッシュ)。
const (
	// EnvAdminUser holds the administrator's login name.
	EnvAdminUser = "ADMIN_USER"
	// EnvAdminPasswordHash holds the bcrypt hash of the administrator's
	// password. Generate it with `make admin-hash`.
	EnvAdminPasswordHash = "ADMIN_PASSWORD_HASH"
)

// dummyBcryptHash is compared when ADMIN_PASSWORD_HASH is missing so that a
// misconfigured server still spends the same bcrypt work per login attempt
// and the response time does not reveal configuration state. The plaintext
// is not used anywhere; validation against this hash always fails together
// with the username check.
//
// The constant is kept at bcrypt.DefaultCost, aligned with the viewer
// dummy (usecase/viewer): TestDummyBcryptHash_CostMatchesDefaultCost pins
// the cost so future bcrypt.DefaultCost bumps do not silently reintroduce
// a timing skew.
const dummyBcryptHash = "$2a$10$2liJaVtwjkEHDTCuT02M2.Fk2DMXjYqQhpWzlKwPwD.B5SfFQ0fpm"

// AdminAuthProvider validates the single administrator's credentials against
// environment variables. The password is verified with bcrypt (C-20); the
// plaintext password is never stored on the server.
type AdminAuthProvider struct{}

// NewAdminAuthProvider creates a new administrator credential provider.
func NewAdminAuthProvider() *AdminAuthProvider {
	return &AdminAuthProvider{}
}

// ValidateCredentials validates credentials against ADMIN_USER and
// ADMIN_PASSWORD_HASH.
//
// Security notes:
//   - The username comparison is constant time.
//   - bcrypt comparison runs regardless of the username result, so timing
//     does not reveal whether the username was correct.
//   - The returned error is generic; it does not reveal which part failed.
func (p *AdminAuthProvider) ValidateCredentials(_ context.Context, creds authservice.Credentials) error {
	if creds.Username == "" || creds.Password == "" {
		return fmt.Errorf("credentials must not be empty")
	}

	adminUser := os.Getenv(EnvAdminUser)
	hash := os.Getenv(EnvAdminPasswordHash)

	userMatch := adminUser != "" &&
		subtle.ConstantTimeCompare([]byte(creds.Username), []byte(adminUser)) == 1

	// Keep the bcrypt work uniform even when the hash is not configured.
	configured := hash != ""
	if !configured {
		hash = dummyBcryptHash
	}
	passErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(creds.Password))

	if !userMatch || passErr != nil || !configured {
		return fmt.Errorf("invalid credentials")
	}
	return nil
}

// Name returns the provider name.
func (p *AdminAuthProvider) Name() string {
	return "env-bcrypt"
}
