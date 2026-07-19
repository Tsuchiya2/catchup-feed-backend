package auth

import (
	"context"
	"testing"

	authservice "catchup-feed/internal/service/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/crypto/bcrypt"
)

// testAdminUser / testPassword are the credentials used across the auth
// package tests. Hashes are generated with bcrypt.MinCost to keep the test
// suite fast; the production cost floor is enforced by
// ValidateAdminCredentials and cmd/hash-password, not by the provider.
const (
	testAdminUser = "admin@example.com"
	testPassword  = "correct-horse-battery"
)

// testHash generates a bcrypt hash for password at MinCost.
func testHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return string(hash)
}

// setAdminEnv configures the admin credential environment for a test.
func setAdminEnv(t *testing.T, user, passwordHash string) {
	t.Helper()
	t.Setenv(EnvAdminUser, user)
	t.Setenv(EnvAdminPasswordHash, passwordHash)
}

func TestAdminAuthProvider_Name(t *testing.T) {
	assert.Equal(t, "env-bcrypt", NewAdminAuthProvider().Name())
}

// TestDummyBcryptHash_CostMatchesDefaultCost pins the dummy hash to
// bcrypt.DefaultCost, aligned with the viewer-side dummy
// (usecase/viewer): a cost mismatch between the dummy and real hashes
// makes the equal-work timing defence observable. If bcrypt.DefaultCost is
// ever bumped, regenerate dummyBcryptHash.
func TestDummyBcryptHash_CostMatchesDefaultCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyBcryptHash))
	require.NoError(t, err)
	assert.Equal(t, bcrypt.DefaultCost, cost)
}

func TestAdminAuthProvider_ValidateCredentials(t *testing.T) {
	validHash := testHash(t, testPassword)

	tests := []struct {
		name      string
		envUser   string
		envHash   string
		creds     authservice.Credentials
		wantError bool
	}{
		{
			name:    "valid credentials",
			envUser: testAdminUser,
			envHash: validHash,
			creds:   authservice.Credentials{Username: testAdminUser, Password: testPassword},
		},
		{
			name:      "wrong password",
			envUser:   testAdminUser,
			envHash:   validHash,
			creds:     authservice.Credentials{Username: testAdminUser, Password: "wrong-password-123"},
			wantError: true,
		},
		{
			name:      "wrong username",
			envUser:   testAdminUser,
			envHash:   validHash,
			creds:     authservice.Credentials{Username: "someone@example.com", Password: testPassword},
			wantError: true,
		},
		{
			name:      "empty username",
			envUser:   testAdminUser,
			envHash:   validHash,
			creds:     authservice.Credentials{Username: "", Password: testPassword},
			wantError: true,
		},
		{
			name:      "empty password",
			envUser:   testAdminUser,
			envHash:   validHash,
			creds:     authservice.Credentials{Username: testAdminUser, Password: ""},
			wantError: true,
		},
		{
			name:      "plaintext password stored instead of hash is rejected",
			envUser:   testAdminUser,
			envHash:   testPassword, // C-20: 平文比較は廃止。平文が入っていても絶対に通らない
			creds:     authservice.Credentials{Username: testAdminUser, Password: testPassword},
			wantError: true,
		},
		{
			name:      "hash not configured",
			envUser:   testAdminUser,
			envHash:   "",
			creds:     authservice.Credentials{Username: testAdminUser, Password: testPassword},
			wantError: true,
		},
		{
			name:      "admin user not configured rejects empty username",
			envUser:   "",
			envHash:   validHash,
			creds:     authservice.Credentials{Username: "", Password: testPassword},
			wantError: true,
		},
		{
			name:      "hash value supplied as password is rejected",
			envUser:   testAdminUser,
			envHash:   validHash,
			creds:     authservice.Credentials{Username: testAdminUser, Password: validHash},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setAdminEnv(t, tt.envUser, tt.envHash)

			err := NewAdminAuthProvider().ValidateCredentials(context.Background(), tt.creds)

			if tt.wantError {
				require.Error(t, err)
				// 失敗理由(ユーザー名かパスワードのどちらが誤りか)を漏らさない
				assert.Contains(t, []string{"invalid credentials", "credentials must not be empty"}, err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
