// Package auth provides framework-agnostic authentication business logic.
//
// pulse は単一管理者システム(C-7・C-20)であり、ここにはロールや複数
// ユーザーの概念を持ち込まない。資格情報の検証方法(環境変数+bcrypt)は
// AuthProvider の実装側に委ねる。
package auth

import (
	"context"
)

// Credentials represents authentication credentials.
type Credentials struct {
	Username string
	Password string
}

// AuthProvider defines the interface for credential validation.
type AuthProvider interface {
	// ValidateCredentials validates user credentials.
	// It returns an error when the credentials do not belong to the
	// administrator.
	ValidateCredentials(ctx context.Context, creds Credentials) error

	// Name returns the name of this provider.
	Name() string
}

// AuthService handles authentication business logic.
type AuthService struct {
	provider AuthProvider
}

// NewAuthService creates a new authentication service.
func NewAuthService(provider AuthProvider) *AuthService {
	return &AuthService{provider: provider}
}

// ValidateCredentials validates user credentials via the configured provider.
func (s *AuthService) ValidateCredentials(ctx context.Context, creds Credentials) error {
	return s.provider.ValidateCredentials(ctx, creds)
}
