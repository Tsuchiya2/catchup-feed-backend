package auth

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

// minBcryptCost is the minimum accepted bcrypt cost for the administrator
// password hash. bcrypt.DefaultCost (10) is the floor; `make admin-hash`
// generates hashes with cost 12.
const minBcryptCost = bcrypt.DefaultCost

// ValidateAdminCredentials validates the administrator credential
// configuration at application startup. It must be called before the server
// starts so that a misconfigured server fails fast instead of rejecting
// every login at runtime.
//
// Requirements:
//   - ADMIN_USER must not be empty
//   - ADMIN_PASSWORD_HASH must not be empty
//   - ADMIN_PASSWORD_HASH must be a parseable bcrypt hash
//   - the bcrypt cost must be at least minBcryptCost
//
// The returned error is safe to log; it never contains the hash itself.
func ValidateAdminCredentials() error {
	if os.Getenv(EnvAdminUser) == "" {
		return fmt.Errorf("admin credentials validation failed: %s must not be empty", EnvAdminUser)
	}

	hash := os.Getenv(EnvAdminPasswordHash)
	if hash == "" {
		return fmt.Errorf("admin credentials validation failed: %s must not be empty (generate one with `make admin-hash`)", EnvAdminPasswordHash)
	}

	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		return fmt.Errorf("admin credentials validation failed: %s is not a valid bcrypt hash (generate one with `make admin-hash`)", EnvAdminPasswordHash)
	}
	if cost < minBcryptCost {
		return fmt.Errorf("admin credentials validation failed: %s bcrypt cost %d is below the minimum %d (regenerate with `make admin-hash`)", EnvAdminPasswordHash, cost, minBcryptCost)
	}

	return nil
}
