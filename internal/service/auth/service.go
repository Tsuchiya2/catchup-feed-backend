package auth

import (
	"context"
	"strings"
)

// Credentials represents authentication credentials.
type Credentials struct {
	Username string
	Password string
}

// CredentialRequirements defines password policy requirements.
type CredentialRequirements struct {
	MinPasswordLength int
	WeakPasswords     []string
}

// AuthProvider defines the interface for authentication providers.
// This interface is framework-agnostic and can be implemented by various authentication mechanisms.
type AuthProvider interface {
	// ValidateCredentials validates user credentials.
	ValidateCredentials(ctx context.Context, creds Credentials) error

	// GetRequirements returns the credential requirements for this provider.
	GetRequirements() CredentialRequirements

	// Name returns the name of this provider.
	Name() string
}

// AuthService handles authentication business logic.
// This service is framework-agnostic and can be used with any HTTP framework or CLI.
type AuthService struct {
	provider        AuthProvider
	publicEndpoints []string
}

// NewAuthService creates a new authentication service.
func NewAuthService(provider AuthProvider, publicEndpoints []string) *AuthService {
	return &AuthService{
		provider:        provider,
		publicEndpoints: publicEndpoints,
	}
}

// ValidateCredentials validates user credentials via the configured provider.
func (s *AuthService) ValidateCredentials(ctx context.Context, creds Credentials) error {
	return s.provider.ValidateCredentials(ctx, creds)
}

// IsPublicEndpoint checks if a path is publicly accessible.
// Returns true if the path matches any configured public endpoint prefix.
func (s *AuthService) IsPublicEndpoint(path string) bool {
	for _, endpoint := range s.publicEndpoints {
		if strings.HasPrefix(path, endpoint) {
			return true
		}
	}
	return false
}

// GetProvider returns the current authentication provider.
func (s *AuthService) GetProvider() AuthProvider {
	return s.provider
}
