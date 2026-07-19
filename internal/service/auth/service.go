// Package auth provides framework-agnostic authentication business logic
// for the administrator.
//
// 管理者は環境変数+bcrypt のまま(C-7。users テーブルなしの原則は admin
// について維持)。閲覧専用アカウント(viewer, D-27)の照合はここではなく
// usecase/viewer が担い、HTTP 層(TokenHandler)が admin → viewer の順で
// フォールバックする。
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
