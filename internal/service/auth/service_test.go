package auth

import (
	"context"
	"fmt"
	"testing"
)

// mockAuthProvider is a mock implementation of AuthProvider for testing
type mockAuthProvider struct {
	name                   string
	validateCredentialsErr error
	requirements           CredentialRequirements
}

func (m *mockAuthProvider) ValidateCredentials(ctx context.Context, creds Credentials) error {
	return m.validateCredentialsErr
}

func (m *mockAuthProvider) GetRequirements() CredentialRequirements {
	return m.requirements
}

func (m *mockAuthProvider) Name() string {
	return m.name
}

func TestNewAuthService(t *testing.T) {
	provider := &mockAuthProvider{name: "mock"}
	publicEndpoints := []string{"/health", "/metrics"}

	service := NewAuthService(provider, publicEndpoints)

	if service == nil {
		t.Fatal("expected service to be non-nil")
	}

	if service.provider != provider {
		t.Error("expected provider to be set correctly")
	}

	if len(service.publicEndpoints) != 2 {
		t.Errorf("expected 2 public endpoints, got %d", len(service.publicEndpoints))
	}
}

func TestAuthService_ValidateCredentials(t *testing.T) {
	tests := []struct {
		name        string
		providerErr error
		expectError bool
	}{
		{
			name:        "successful validation",
			providerErr: nil,
			expectError: false,
		},
		{
			name:        "provider returns error",
			providerErr: fmt.Errorf("invalid credentials"),
			expectError: true,
		},
		{
			name:        "provider returns empty credentials error",
			providerErr: fmt.Errorf("credentials must not be empty"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &mockAuthProvider{
				name:                   "mock",
				validateCredentialsErr: tt.providerErr,
			}

			service := NewAuthService(provider, nil)
			ctx := context.Background()

			creds := Credentials{
				Username: "testuser",
				Password: "testpass",
			}

			err := service.ValidateCredentials(ctx, creds)

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestAuthService_IsPublicEndpoint(t *testing.T) {
	publicEndpoints := []string{
		"/health",
		"/ready",
		"/metrics",
		"/swagger/",
		"/auth/token",
	}

	provider := &mockAuthProvider{name: "mock"}
	service := NewAuthService(provider, publicEndpoints)

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "exact match - health",
			path:     "/health",
			expected: true,
		},
		{
			name:     "exact match - ready",
			path:     "/ready",
			expected: true,
		},
		{
			name:     "exact match - metrics",
			path:     "/metrics",
			expected: true,
		},
		{
			name:     "exact match - auth token",
			path:     "/auth/token",
			expected: true,
		},
		{
			name:     "prefix match - swagger",
			path:     "/swagger/index.html",
			expected: true,
		},
		{
			name:     "prefix match - swagger docs",
			path:     "/swagger/doc.json",
			expected: true,
		},
		{
			name:     "protected endpoint - articles",
			path:     "/articles",
			expected: false,
		},
		{
			name:     "protected endpoint - sources",
			path:     "/sources",
			expected: false,
		},
		{
			name:     "protected endpoint - articles with ID",
			path:     "/articles/123",
			expected: false,
		},
		{
			name:     "partial match with prefix",
			path:     "/healthcheck",
			expected: true, // matches "/health" prefix
		},
		{
			name:     "similar path should not match",
			path:     "/api/health",
			expected: false,
		},
		{
			name:     "empty path",
			path:     "",
			expected: false,
		},
		{
			name:     "root path",
			path:     "/",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.IsPublicEndpoint(tt.path)
			if result != tt.expected {
				t.Errorf("expected %v for path %s, got %v", tt.expected, tt.path, result)
			}
		})
	}
}

func TestAuthService_IsPublicEndpoint_EmptyEndpoints(t *testing.T) {
	provider := &mockAuthProvider{name: "mock"}
	service := NewAuthService(provider, []string{})

	// No public endpoints configured
	if service.IsPublicEndpoint("/health") {
		t.Error("expected /health to be protected when no public endpoints configured")
	}

	if service.IsPublicEndpoint("/anything") {
		t.Error("expected any path to be protected when no public endpoints configured")
	}
}

func TestAuthService_IsPublicEndpoint_NilEndpoints(t *testing.T) {
	provider := &mockAuthProvider{name: "mock"}
	service := NewAuthService(provider, nil)

	// Nil public endpoints should not panic
	if service.IsPublicEndpoint("/health") {
		t.Error("expected /health to be protected when public endpoints is nil")
	}
}

func TestAuthService_GetProvider(t *testing.T) {
	provider := &mockAuthProvider{
		name: "test-provider",
		requirements: CredentialRequirements{
			MinPasswordLength: 10,
			WeakPasswords:     []string{"weak"},
		},
	}

	service := NewAuthService(provider, nil)

	retrievedProvider := service.GetProvider()

	if retrievedProvider == nil {
		t.Fatal("expected provider to be non-nil")
	}

	if retrievedProvider.Name() != "test-provider" {
		t.Errorf("expected provider name to be 'test-provider', got '%s'", retrievedProvider.Name())
	}

	reqs := retrievedProvider.GetRequirements()
	if reqs.MinPasswordLength != 10 {
		t.Errorf("expected min password length to be 10, got %d", reqs.MinPasswordLength)
	}
}

// mockAuthProviderWithContext is a mock that captures context
type mockAuthProviderWithContext struct {
	name        string
	receivedCtx context.Context
}

func (m *mockAuthProviderWithContext) ValidateCredentials(ctx context.Context, creds Credentials) error {
	m.receivedCtx = ctx
	return nil
}

func (m *mockAuthProviderWithContext) GetRequirements() CredentialRequirements {
	return CredentialRequirements{}
}

func (m *mockAuthProviderWithContext) Name() string {
	return m.name
}

func TestAuthService_ContextPropagation(t *testing.T) {
	// Test that context is properly passed to provider
	provider := &mockAuthProviderWithContext{
		name: "mock",
	}

	service := NewAuthService(provider, nil)

	type contextKey string
	key := contextKey("test-key")
	value := "test-value"

	ctx := context.WithValue(context.Background(), key, value)

	creds := Credentials{
		Username: "test",
		Password: "test",
	}

	_ = service.ValidateCredentials(ctx, creds)

	if provider.receivedCtx == nil {
		t.Fatal("context was not passed to provider")
	}

	receivedValue := provider.receivedCtx.Value(key)
	if receivedValue != value {
		t.Errorf("expected context value '%s', got '%v'", value, receivedValue)
	}
}

func TestAuthService_MultipleProviders(t *testing.T) {
	// Test that service can be created with different providers
	providers := []*mockAuthProvider{
		{name: "basic"},
		{name: "oauth"},
		{name: "saml"},
	}

	for _, provider := range providers {
		service := NewAuthService(provider, nil)

		if service.GetProvider().Name() != provider.name {
			t.Errorf("expected provider name '%s', got '%s'", provider.name, service.GetProvider().Name())
		}
	}
}

func TestAuthService_ConcurrentAccess(t *testing.T) {
	// Test that service is safe for concurrent access
	provider := &mockAuthProvider{name: "mock"}
	service := NewAuthService(provider, []string{"/health"})

	done := make(chan bool)

	// Concurrent IsPublicEndpoint calls
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			paths := []string{"/health", "/articles", "/metrics", "/sources"}
			for j := 0; j < 100; j++ {
				_ = service.IsPublicEndpoint(paths[j%len(paths)])
			}
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestAuthService_ValidateCredentials_WithContextCancellation(t *testing.T) {
	provider := &mockAuthProvider{
		name:                   "mock",
		validateCredentialsErr: nil,
	}

	service := NewAuthService(provider, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	creds := Credentials{
		Username: "test",
		Password: "test",
	}

	// Note: Current implementation doesn't check ctx.Done() in service layer
	// This test documents the current behavior
	_ = service.ValidateCredentials(ctx, creds)
}
