package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"
	"strings"

	authservice "catchup-feed/internal/service/auth"
)

// BasicAuthProvider implements environment-based authentication.
type BasicAuthProvider struct {
	minPasswordLength int
	weakPasswords     []string
}

// NewBasicAuthProvider creates a new basic auth provider.
func NewBasicAuthProvider(minPasswordLength int, weakPasswords []string) *BasicAuthProvider {
	return &BasicAuthProvider{
		minPasswordLength: minPasswordLength,
		weakPasswords:     weakPasswords,
	}
}

// ValidateCredentials validates credentials against environment variables.
func (p *BasicAuthProvider) ValidateCredentials(ctx context.Context, creds authservice.Credentials) error {
	// Check if credentials are empty
	if creds.Username == "" || creds.Password == "" {
		return fmt.Errorf("credentials must not be empty")
	}

	// Check password length
	if len(creds.Password) < p.minPasswordLength {
		return fmt.Errorf("password must be at least %d characters", p.minPasswordLength)
	}

	// Check for weak passwords
	for _, weak := range p.weakPasswords {
		if creds.Password == weak || strings.HasPrefix(creds.Password, weak) {
			return fmt.Errorf("weak password detected")
		}
	}

	adminUser := os.Getenv("ADMIN_USER")
	adminPass := os.Getenv("ADMIN_USER_PASSWORD")

	// Use constant-time comparison to prevent timing attacks
	userMatch := subtle.ConstantTimeCompare([]byte(creds.Username), []byte(adminUser)) == 1
	passMatch := subtle.ConstantTimeCompare([]byte(creds.Password), []byte(adminPass)) == 1

	if !userMatch || !passMatch {
		return fmt.Errorf("invalid credentials")
	}

	return nil
}

// GetRequirements returns the password requirements.
func (p *BasicAuthProvider) GetRequirements() authservice.CredentialRequirements {
	return authservice.CredentialRequirements{
		MinPasswordLength: p.minPasswordLength,
		WeakPasswords:     p.weakPasswords,
	}
}

// Name returns the provider name.
func (p *BasicAuthProvider) Name() string {
	return "basic"
}
