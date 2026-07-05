package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a test double for AuthProvider.
type mockProvider struct {
	err       error
	gotCreds  Credentials
	callCount int
}

func (m *mockProvider) ValidateCredentials(_ context.Context, creds Credentials) error {
	m.callCount++
	m.gotCreds = creds
	return m.err
}

func (m *mockProvider) Name() string { return "mock" }

func TestAuthService_ValidateCredentials(t *testing.T) {
	tests := []struct {
		name        string
		providerErr error
	}{
		{name: "provider accepts"},
		{name: "provider rejects", providerErr: errors.New("invalid credentials")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := &mockProvider{err: tt.providerErr}
			svc := NewAuthService(provider)
			creds := Credentials{Username: "admin@example.com", Password: "pw"}

			err := svc.ValidateCredentials(context.Background(), creds)

			if tt.providerErr != nil {
				require.Error(t, err)
				assert.Equal(t, tt.providerErr, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, 1, provider.callCount)
			assert.Equal(t, creds, provider.gotCreds, "credentials must be passed through unchanged")
		})
	}
}
