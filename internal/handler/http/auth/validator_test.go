package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/crypto/bcrypt"
)

func TestValidateAdminCredentials(t *testing.T) {
	costTooLow, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	require.NoError(t, err)
	costOK, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.DefaultCost)
	require.NoError(t, err)

	tests := []struct {
		name      string
		envUser   string
		envHash   string
		wantError string
	}{
		{
			name:    "valid configuration",
			envUser: testAdminUser,
			envHash: string(costOK),
		},
		{
			name:      "missing admin user",
			envUser:   "",
			envHash:   string(costOK),
			wantError: "ADMIN_USER must not be empty",
		},
		{
			name:      "missing hash",
			envUser:   testAdminUser,
			envHash:   "",
			wantError: "ADMIN_PASSWORD_HASH must not be empty",
		},
		{
			name:      "plaintext password instead of hash",
			envUser:   testAdminUser,
			envHash:   "not-a-bcrypt-hash",
			wantError: "not a valid bcrypt hash",
		},
		{
			name:      "cost below minimum",
			envUser:   testAdminUser,
			envHash:   string(costTooLow),
			wantError: "below the minimum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAdminEnv(t, tt.envUser, tt.envHash)

			err := ValidateAdminCredentials()

			if tt.wantError == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantError)
				// ハッシュ値そのものをログに漏らさない
				if tt.envHash != "" {
					assert.NotContains(t, err.Error(), tt.envHash)
				}
			}
		})
	}
}
