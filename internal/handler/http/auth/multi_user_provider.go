package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"os"
	"strings"

	authservice "catchup-feed/internal/service/auth"
)

// MultiUserAuthProvider implements environment-based authentication for multiple users.
// It supports both admin and viewer (demo) roles.
type MultiUserAuthProvider struct {
	minPasswordLength int
	weakPasswords     []string
}

// NewMultiUserAuthProvider creates a new multi-user auth provider.
func NewMultiUserAuthProvider(minPasswordLength int, weakPasswords []string) *MultiUserAuthProvider {
	return &MultiUserAuthProvider{
		minPasswordLength: minPasswordLength,
		weakPasswords:     weakPasswords,
	}
}

// ValidateCredentials validates credentials against environment variables.
// It checks both admin and viewer credentials.
func (p *MultiUserAuthProvider) ValidateCredentials(ctx context.Context, creds authservice.Credentials) error {
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

	// Get admin credentials
	adminUser := os.Getenv("ADMIN_USER")
	adminPass := os.Getenv("ADMIN_USER_PASSWORD")

	// Check admin credentials using constant-time comparison
	adminUserMatch := subtle.ConstantTimeCompare([]byte(creds.Username), []byte(adminUser)) == 1
	adminPassMatch := subtle.ConstantTimeCompare([]byte(creds.Password), []byte(adminPass)) == 1

	if adminUserMatch && adminPassMatch {
		return nil
	}

	// Get viewer credentials (optional)
	demoUser := os.Getenv("DEMO_USER")
	demoPass := os.Getenv("DEMO_USER_PASSWORD")

	// Only check viewer if configured
	if demoUser != "" {
		demoUserMatch := subtle.ConstantTimeCompare([]byte(creds.Username), []byte(demoUser)) == 1
		demoPassMatch := subtle.ConstantTimeCompare([]byte(creds.Password), []byte(demoPass)) == 1

		if demoUserMatch && demoPassMatch {
			return nil
		}
	}

	return fmt.Errorf("invalid credentials")
}

// IdentifyUser returns the role for a given email address.
// Returns "admin", "viewer", or error if email not recognized.
func (p *MultiUserAuthProvider) IdentifyUser(ctx context.Context, email string) (string, error) {
	if email == "" {
		return "", fmt.Errorf("email must not be empty")
	}

	adminUser := os.Getenv("ADMIN_USER")
	demoUser := os.Getenv("DEMO_USER")

	// Check admin using constant-time comparison
	if subtle.ConstantTimeCompare([]byte(email), []byte(adminUser)) == 1 {
		return RoleAdmin, nil
	}

	// Check viewer if configured
	if demoUser != "" && subtle.ConstantTimeCompare([]byte(email), []byte(demoUser)) == 1 {
		return RoleViewer, nil
	}

	return "", fmt.Errorf("user not found")
}

// GetRequirements returns the password requirements.
func (p *MultiUserAuthProvider) GetRequirements() authservice.CredentialRequirements {
	return authservice.CredentialRequirements{
		MinPasswordLength: p.minPasswordLength,
		WeakPasswords:     p.weakPasswords,
	}
}

// Name returns the provider name.
func (p *MultiUserAuthProvider) Name() string {
	return "multi-user"
}
