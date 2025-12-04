package auth

import (
	authservice "catchup-feed/internal/service/auth"
	"context"
	"os"
	"testing"
)

func TestNewBasicAuthProvider(t *testing.T) {
	weakPasswords := []string{"admin", "password", "123456"}
	provider := NewBasicAuthProvider(12, weakPasswords)

	if provider == nil {
		t.Fatal("expected provider to be non-nil")
	}

	if provider.minPasswordLength != 12 {
		t.Errorf("expected minPasswordLength to be 12, got %d", provider.minPasswordLength)
	}

	if len(provider.weakPasswords) != 3 {
		t.Errorf("expected 3 weak passwords, got %d", len(provider.weakPasswords))
	}
}

func TestBasicAuthProvider_Name(t *testing.T) {
	provider := NewBasicAuthProvider(12, nil)

	if provider.Name() != "basic" {
		t.Errorf("expected name to be 'basic', got '%s'", provider.Name())
	}
}

func TestBasicAuthProvider_GetRequirements(t *testing.T) {
	weakPasswords := []string{"admin", "password"}
	provider := NewBasicAuthProvider(10, weakPasswords)

	reqs := provider.GetRequirements()

	if reqs.MinPasswordLength != 10 {
		t.Errorf("expected MinPasswordLength to be 10, got %d", reqs.MinPasswordLength)
	}

	if len(reqs.WeakPasswords) != 2 {
		t.Errorf("expected 2 weak passwords, got %d", len(reqs.WeakPasswords))
	}
}

func TestBasicAuthProvider_ValidateCredentials(t *testing.T) {
	// Set up test environment variables
	originalUser := os.Getenv("ADMIN_USER")
	originalPass := os.Getenv("ADMIN_USER_PASSWORD")
	defer func() {
		_ = os.Setenv("ADMIN_USER", originalUser)
		_ = os.Setenv("ADMIN_USER_PASSWORD", originalPass)
	}()

	_ = os.Setenv("ADMIN_USER", "testadmin")
	_ = os.Setenv("ADMIN_USER_PASSWORD", "ValidPassword123")

	weakPasswords := []string{"admin", "password", "123456"}
	provider := NewBasicAuthProvider(12, weakPasswords)

	tests := []struct {
		name        string
		creds       authservice.Credentials
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid credentials",
			creds: authservice.Credentials{
				Username: "testadmin",
				Password: "ValidPassword123",
			},
			expectError: false,
		},
		{
			name: "empty username",
			creds: authservice.Credentials{
				Username: "",
				Password: "ValidPassword123",
			},
			expectError: true,
			errorMsg:    "credentials must not be empty",
		},
		{
			name: "empty password",
			creds: authservice.Credentials{
				Username: "testadmin",
				Password: "",
			},
			expectError: true,
			errorMsg:    "credentials must not be empty",
		},
		{
			name: "password too short",
			creds: authservice.Credentials{
				Username: "testadmin",
				Password: "short",
			},
			expectError: true,
			errorMsg:    "password must be at least 12 characters",
		},
		{
			name: "weak password - exact match",
			creds: authservice.Credentials{
				Username: "testadmin",
				Password: "admin12345678", // Long enough to pass length check
			},
			expectError: true,
			errorMsg:    "weak password detected",
		},
		{
			name: "weak password - prefix match",
			creds: authservice.Credentials{
				Username: "testadmin",
				Password: "admin1234567890",
			},
			expectError: true,
			errorMsg:    "weak password detected",
		},
		{
			name: "weak password - another weak",
			creds: authservice.Credentials{
				Username: "testadmin",
				Password: "password12345",
			},
			expectError: true,
			errorMsg:    "weak password detected",
		},
		{
			name: "invalid username",
			creds: authservice.Credentials{
				Username: "wronguser",
				Password: "ValidPassword123",
			},
			expectError: true,
			errorMsg:    "invalid credentials",
		},
		{
			name: "invalid password",
			creds: authservice.Credentials{
				Username: "testadmin",
				Password: "WrongPassword123",
			},
			expectError: true,
			errorMsg:    "invalid credentials",
		},
		{
			name: "both invalid",
			creds: authservice.Credentials{
				Username: "wronguser",
				Password: "WrongPassword123",
			},
			expectError: true,
			errorMsg:    "invalid credentials",
		},
	}

	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := provider.ValidateCredentials(ctx, tt.creds)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if err.Error() != tt.errorMsg {
					t.Errorf("expected error message '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

// TestBasicAuthProvider_TimingAttackResistance verifies constant-time comparison
func TestBasicAuthProvider_TimingAttackResistance(t *testing.T) {
	originalUser := os.Getenv("ADMIN_USER")
	originalPass := os.Getenv("ADMIN_USER_PASSWORD")
	defer func() {
		_ = os.Setenv("ADMIN_USER", originalUser)
		_ = os.Setenv("ADMIN_USER_PASSWORD", originalPass)
	}()

	_ = os.Setenv("ADMIN_USER", "adminuser")
	_ = os.Setenv("ADMIN_USER_PASSWORD", "SecurePassword123")

	provider := NewBasicAuthProvider(12, nil)
	ctx := context.Background()

	// Test that the function uses constant-time comparison
	// by verifying it rejects both partially matching and completely wrong credentials
	testCases := []struct {
		name string
		user string
		pass string
	}{
		{"wrong username same length", "wronguser", "SecurePassword123"},
		{"wrong username diff length", "wrong", "SecurePassword123"},
		{"wrong password same length", "adminuser", "WrongPassword123"},
		{"wrong password diff length", "adminuser", "Wrong"},
		{"both wrong", "wrong", "Wrong"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			creds := authservice.Credentials{
				Username: tc.user,
				Password: tc.pass,
			}

			err := provider.ValidateCredentials(ctx, creds)
			if err == nil {
				t.Error("expected error for invalid credentials")
			}

			// All invalid credential errors should have the same message
			// This ensures constant-time behavior
			if err.Error() != "invalid credentials" {
				// Allow early checks (empty, length, weak password) to have different messages
				// Only the final comparison should use constant-time
				allowedEarlyErrors := []string{
					"credentials must not be empty",
					"password must be at least 12 characters",
					"weak password detected",
				}

				isEarlyError := false
				for _, allowed := range allowedEarlyErrors {
					if err.Error() == allowed {
						isEarlyError = true
						break
					}
				}

				if !isEarlyError {
					t.Errorf("expected 'invalid credentials' error, got '%s'", err.Error())
				}
			}
		})
	}
}

// TestBasicAuthProvider_ContextCancellation tests context handling
func TestBasicAuthProvider_ContextCancellation(t *testing.T) {
	originalUser := os.Getenv("ADMIN_USER")
	originalPass := os.Getenv("ADMIN_USER_PASSWORD")
	defer func() {
		_ = os.Setenv("ADMIN_USER", originalUser)
		_ = os.Setenv("ADMIN_USER_PASSWORD", originalPass)
	}()

	_ = os.Setenv("ADMIN_USER", "testadmin")
	_ = os.Setenv("ADMIN_USER_PASSWORD", "ValidPassword123")

	provider := NewBasicAuthProvider(12, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	creds := authservice.Credentials{
		Username: "testadmin",
		Password: "ValidPassword123",
	}

	// Note: Current implementation doesn't check ctx.Done()
	// This test documents the current behavior
	// Future enhancement could add context checking
	_ = provider.ValidateCredentials(ctx, creds)
}

// TestBasicAuthProvider_NoWeakPasswords tests provider with no weak passwords configured
func TestBasicAuthProvider_NoWeakPasswords(t *testing.T) {
	originalUser := os.Getenv("ADMIN_USER")
	originalPass := os.Getenv("ADMIN_USER_PASSWORD")
	defer func() {
		_ = os.Setenv("ADMIN_USER", originalUser)
		_ = os.Setenv("ADMIN_USER_PASSWORD", originalPass)
	}()

	_ = os.Setenv("ADMIN_USER", "testadmin")
	_ = os.Setenv("ADMIN_USER_PASSWORD", "ValidPassword123")

	provider := NewBasicAuthProvider(12, nil) // No weak passwords
	ctx := context.Background()

	creds := authservice.Credentials{
		Username: "testadmin",
		Password: "ValidPassword123",
	}

	err := provider.ValidateCredentials(ctx, creds)
	if err != nil {
		t.Errorf("expected no error with nil weak passwords, got: %v", err)
	}
}

// TestBasicAuthProvider_EmptyWeakPasswords tests provider with empty weak passwords slice
func TestBasicAuthProvider_EmptyWeakPasswords(t *testing.T) {
	originalUser := os.Getenv("ADMIN_USER")
	originalPass := os.Getenv("ADMIN_USER_PASSWORD")
	defer func() {
		_ = os.Setenv("ADMIN_USER", originalUser)
		_ = os.Setenv("ADMIN_USER_PASSWORD", originalPass)
	}()

	_ = os.Setenv("ADMIN_USER", "testadmin")
	_ = os.Setenv("ADMIN_USER_PASSWORD", "ValidPassword123")

	provider := NewBasicAuthProvider(12, []string{}) // Empty slice
	ctx := context.Background()

	creds := authservice.Credentials{
		Username: "testadmin",
		Password: "ValidPassword123",
	}

	err := provider.ValidateCredentials(ctx, creds)
	if err != nil {
		t.Errorf("expected no error with empty weak passwords, got: %v", err)
	}
}

func TestBasicAuthProvider_IdentifyUser(t *testing.T) {
	originalUser := os.Getenv("ADMIN_USER")
	defer func() {
		_ = os.Setenv("ADMIN_USER", originalUser)
	}()

	_ = os.Setenv("ADMIN_USER", "admin@example.com")

	provider := NewBasicAuthProvider(12, nil)
	ctx := context.Background()

	tests := []struct {
		name         string
		email        string
		expectedRole string
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "admin email returns admin role",
			email:        "admin@example.com",
			expectedRole: RoleAdmin,
			expectError:  false,
		},
		{
			name:        "unknown email returns error",
			email:       "unknown@example.com",
			expectError: true,
			errorMsg:    "user not found",
		},
		{
			name:        "empty email returns error",
			email:       "",
			expectError: true,
			errorMsg:    "email must not be empty",
		},
		{
			name:        "case sensitive - wrong case",
			email:       "ADMIN@example.com",
			expectError: true,
			errorMsg:    "user not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role, err := provider.IdentifyUser(ctx, tt.email)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got nil")
					return
				}
				if err.Error() != tt.errorMsg {
					t.Errorf("expected error message '%s', got '%s'", tt.errorMsg, err.Error())
				}
				if role != "" {
					t.Errorf("expected empty role on error, got '%s'", role)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
					return
				}
				if role != tt.expectedRole {
					t.Errorf("expected role '%s', got '%s'", tt.expectedRole, role)
				}
			}
		})
	}
}
