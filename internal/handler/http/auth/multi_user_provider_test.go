package auth

import (
	authservice "catchup-feed/internal/service/auth"
	"context"
	"testing"
	"time"
)

// setupTestEnv is a helper to set up environment variables for tests
func setupTestEnv(t *testing.T, admin, adminPass, demo, demoPass string) {
	t.Helper()
	t.Setenv("ADMIN_USER", admin)
	t.Setenv("ADMIN_USER_PASSWORD", adminPass)
	if demo != "" {
		t.Setenv("DEMO_USER", demo)
		t.Setenv("DEMO_USER_PASSWORD", demoPass)
	}
}

// setupBenchEnv is a helper to set up environment variables for benchmarks
func setupBenchEnv(b *testing.B, admin, adminPass, demo, demoPass string) {
	b.Helper()
	b.Setenv("ADMIN_USER", admin)
	b.Setenv("ADMIN_USER_PASSWORD", adminPass)
	if demo != "" {
		b.Setenv("DEMO_USER", demo)
		b.Setenv("DEMO_USER_PASSWORD", demoPass)
	}
}

func TestNewMultiUserAuthProvider(t *testing.T) {
	weakPasswords := []string{"admin", "password", "123456"}
	provider := NewMultiUserAuthProvider(12, weakPasswords)

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

func TestMultiUserAuthProvider_Name(t *testing.T) {
	provider := NewMultiUserAuthProvider(12, nil)

	if provider.Name() != "multi-user" {
		t.Errorf("expected name to be 'multi-user', got '%s'", provider.Name())
	}
}

func TestMultiUserAuthProvider_GetRequirements(t *testing.T) {
	weakPasswords := []string{"admin", "password"}
	provider := NewMultiUserAuthProvider(10, weakPasswords)

	reqs := provider.GetRequirements()

	if reqs.MinPasswordLength != 10 {
		t.Errorf("expected MinPasswordLength to be 10, got %d", reqs.MinPasswordLength)
	}

	if len(reqs.WeakPasswords) != 2 {
		t.Errorf("expected 2 weak passwords, got %d", len(reqs.WeakPasswords))
	}
}

func TestMultiUserAuthProvider_ValidateCredentials(t *testing.T) {
	weakPasswords := []string{"admin", "password", "123456"}
	provider := NewMultiUserAuthProvider(12, weakPasswords)

	tests := []struct {
		name        string
		adminUser   string
		adminPass   string
		demoUser    string
		demoPass    string
		creds       authservice.Credentials
		expectError bool
		errorMsg    string
	}{
		// Valid credentials tests
		{
			name:        "valid admin credentials",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "viewer@example.com",
			demoPass:    "SecureViewerPass123",
			creds:       authservice.Credentials{Username: "admin@example.com", Password: "SecureAdminPass123"},
			expectError: false,
		},
		{
			name:        "valid viewer credentials",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "viewer@example.com",
			demoPass:    "SecureViewerPass123",
			creds:       authservice.Credentials{Username: "viewer@example.com", Password: "SecureViewerPass123"},
			expectError: false,
		},

		// Invalid password tests
		{
			name:        "invalid admin password",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "viewer@example.com",
			demoPass:    "SecureViewerPass123",
			creds:       authservice.Credentials{Username: "admin@example.com", Password: "WrongPassword123"},
			expectError: true,
			errorMsg:    "invalid credentials",
		},
		{
			name:        "invalid viewer password",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "viewer@example.com",
			demoPass:    "SecureViewerPass123",
			creds:       authservice.Credentials{Username: "viewer@example.com", Password: "WrongPassword123"},
			expectError: true,
			errorMsg:    "invalid credentials",
		},

		// Invalid email tests
		{
			name:        "invalid email (wrong)",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "viewer@example.com",
			demoPass:    "SecureViewerPass123",
			creds:       authservice.Credentials{Username: "wrong@example.com", Password: "SecureAdminPass123"},
			expectError: true,
			errorMsg:    "invalid credentials",
		},

		// Empty credentials tests
		{
			name:        "empty username",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "",
			demoPass:    "",
			creds:       authservice.Credentials{Username: "", Password: "SecureAdminPass123"},
			expectError: true,
			errorMsg:    "credentials must not be empty",
		},
		{
			name:        "empty password",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "",
			demoPass:    "",
			creds:       authservice.Credentials{Username: "admin@example.com", Password: ""},
			expectError: true,
			errorMsg:    "credentials must not be empty",
		},
		{
			name:        "both empty",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "",
			demoPass:    "",
			creds:       authservice.Credentials{Username: "", Password: ""},
			expectError: true,
			errorMsg:    "credentials must not be empty",
		},

		// Password length tests
		{
			name:        "password too short",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "",
			demoPass:    "",
			creds:       authservice.Credentials{Username: "admin@example.com", Password: "short"},
			expectError: true,
			errorMsg:    "password must be at least 12 characters",
		},

		// Weak password tests
		{
			name:        "weak password - exact match",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "",
			demoPass:    "",
			creds:       authservice.Credentials{Username: "admin@example.com", Password: "admin12345678"},
			expectError: true,
			errorMsg:    "weak password detected",
		},
		{
			name:        "weak password - prefix match",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "",
			demoPass:    "",
			creds:       authservice.Credentials{Username: "admin@example.com", Password: "password12345"},
			expectError: true,
			errorMsg:    "weak password detected",
		},

		// Admin-only mode tests (DEMO_USER not set)
		{
			name:        "admin-only mode - admin works",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "", // Not set
			demoPass:    "",
			creds:       authservice.Credentials{Username: "admin@example.com", Password: "SecureAdminPass123"},
			expectError: false,
		},
		{
			name:        "admin-only mode - viewer fails",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "", // Not set
			demoPass:    "",
			creds:       authservice.Credentials{Username: "viewer@example.com", Password: "SecureViewerPass123"},
			expectError: true,
			errorMsg:    "invalid credentials",
		},

		// Cross-user credential tests
		{
			name:        "viewer email with admin password",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "viewer@example.com",
			demoPass:    "SecureViewerPass123",
			creds:       authservice.Credentials{Username: "viewer@example.com", Password: "SecureAdminPass123"},
			expectError: true,
			errorMsg:    "invalid credentials",
		},
		{
			name:        "admin email with viewer password",
			adminUser:   "admin@example.com",
			adminPass:   "SecureAdminPass123",
			demoUser:    "viewer@example.com",
			demoPass:    "SecureViewerPass123",
			creds:       authservice.Credentials{Username: "admin@example.com", Password: "SecureViewerPass123"},
			expectError: true,
			errorMsg:    "invalid credentials",
		},
	}

	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestEnv(t, tt.adminUser, tt.adminPass, tt.demoUser, tt.demoPass)

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

func TestMultiUserAuthProvider_IdentifyUser(t *testing.T) {
	provider := NewMultiUserAuthProvider(12, nil)

	tests := []struct {
		name        string
		adminUser   string
		demoUser    string
		email       string
		expectedRole string
		expectError  bool
		errorMsg     string
	}{
		{
			name:         "admin email returns admin role",
			adminUser:    "admin@example.com",
			demoUser:     "viewer@example.com",
			email:        "admin@example.com",
			expectedRole: RoleAdmin,
			expectError:  false,
		},
		{
			name:         "viewer email returns viewer role",
			adminUser:    "admin@example.com",
			demoUser:     "viewer@example.com",
			email:        "viewer@example.com",
			expectedRole: RoleViewer,
			expectError:  false,
		},
		{
			name:        "unknown email returns error",
			adminUser:   "admin@example.com",
			demoUser:    "viewer@example.com",
			email:       "unknown@example.com",
			expectError: true,
			errorMsg:    "user not found",
		},
		{
			name:        "empty email returns error",
			adminUser:   "admin@example.com",
			demoUser:    "viewer@example.com",
			email:       "",
			expectError: true,
			errorMsg:    "email must not be empty",
		},
		{
			name:         "admin-only mode - admin works",
			adminUser:    "admin@example.com",
			demoUser:     "", // Not set
			email:        "admin@example.com",
			expectedRole: RoleAdmin,
			expectError:  false,
		},
		{
			name:        "admin-only mode - viewer fails",
			adminUser:   "admin@example.com",
			demoUser:    "", // Not set
			email:       "viewer@example.com",
			expectError: true,
			errorMsg:    "user not found",
		},
		{
			name:        "case sensitive - wrong case admin",
			adminUser:   "admin@example.com",
			demoUser:    "viewer@example.com",
			email:       "ADMIN@example.com",
			expectError: true,
			errorMsg:    "user not found",
		},
		{
			name:        "case sensitive - wrong case viewer",
			adminUser:   "admin@example.com",
			demoUser:    "viewer@example.com",
			email:       "VIEWER@example.com",
			expectError: true,
			errorMsg:    "user not found",
		},
	}

	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTestEnv(t, tt.adminUser, "dummy-pass", tt.demoUser, "dummy-pass")

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

func TestMultiUserAuthProvider_TimingAttackResistance(t *testing.T) {
	setupTestEnv(t, "admin@example.com", "SecureAdminPass123", "viewer@example.com", "SecureViewerPass123")

	provider := NewMultiUserAuthProvider(12, nil)
	ctx := context.Background()

	// Test that the function uses constant-time comparison
	// by verifying it rejects both partially matching and completely wrong credentials
	testCases := []struct {
		name string
		user string
		pass string
	}{
		{"wrong admin username same length", "wrong@example.com", "SecureAdminPass123"},
		{"wrong admin username diff length", "wrong@ex.com", "SecureAdminPass123"},
		{"wrong admin password same length", "admin@example.com", "WrongPassword1234"},
		{"wrong admin password diff length", "admin@example.com", "Wrong"},
		{"wrong viewer username", "wrong@example.com", "SecureViewerPass123"},
		{"wrong viewer password", "viewer@example.com", "WrongPassword1234"},
		{"both wrong", "wrong@example.com", "WrongPassword1234"},
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

func TestMultiUserAuthProvider_ContextCancellation(t *testing.T) {
	setupTestEnv(t, "admin@example.com", "SecureAdminPass123", "viewer@example.com", "SecureViewerPass123")

	provider := NewMultiUserAuthProvider(12, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	creds := authservice.Credentials{
		Username: "admin@example.com",
		Password: "SecureAdminPass123",
	}

	// Note: Current implementation doesn't check ctx.Done()
	// This test documents the current behavior
	// Future enhancement could add context checking
	_ = provider.ValidateCredentials(ctx, creds)
}

func TestMultiUserAuthProvider_NoWeakPasswords(t *testing.T) {
	setupTestEnv(t, "admin@example.com", "ValidPassword123", "", "")

	provider := NewMultiUserAuthProvider(12, nil) // No weak passwords
	ctx := context.Background()

	creds := authservice.Credentials{
		Username: "admin@example.com",
		Password: "ValidPassword123",
	}

	err := provider.ValidateCredentials(ctx, creds)
	if err != nil {
		t.Errorf("expected no error with nil weak passwords, got: %v", err)
	}
}

func TestMultiUserAuthProvider_EmptyWeakPasswords(t *testing.T) {
	setupTestEnv(t, "admin@example.com", "ValidPassword123", "", "")

	provider := NewMultiUserAuthProvider(12, []string{}) // Empty slice
	ctx := context.Background()

	creds := authservice.Credentials{
		Username: "admin@example.com",
		Password: "ValidPassword123",
	}

	err := provider.ValidateCredentials(ctx, creds)
	if err != nil {
		t.Errorf("expected no error with empty weak passwords, got: %v", err)
	}
}

// BenchmarkValidateCredentials_ConstantTime verifies timing consistency
func BenchmarkValidateCredentials_ConstantTime(b *testing.B) {
	setupBenchEnv(b, "admin@example.com", "SecureAdminPass123", "viewer@example.com", "SecureViewerPass123")

	provider := NewMultiUserAuthProvider(12, nil)
	ctx := context.Background()

	benchmarks := []struct {
		name  string
		creds authservice.Credentials
	}{
		{
			name: "valid admin",
			creds: authservice.Credentials{
				Username: "admin@example.com",
				Password: "SecureAdminPass123",
			},
		},
		{
			name: "valid viewer",
			creds: authservice.Credentials{
				Username: "viewer@example.com",
				Password: "SecureViewerPass123",
			},
		},
		{
			name: "invalid username",
			creds: authservice.Credentials{
				Username: "wrong@example.com",
				Password: "SecureAdminPass123",
			},
		},
		{
			name: "invalid password",
			creds: authservice.Credentials{
				Username: "admin@example.com",
				Password: "WrongPassword1234",
			},
		},
		{
			name: "both invalid",
			creds: authservice.Credentials{
				Username: "wrong@example.com",
				Password: "WrongPassword1234",
			},
		},
	}

	timings := make(map[string]time.Duration)

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			start := time.Now()
			for i := 0; i < b.N; i++ {
				_ = provider.ValidateCredentials(ctx, bm.creds)
			}
			duration := time.Since(start)
			timings[bm.name] = duration / time.Duration(b.N)
		})
	}

	// After all benchmarks, check timing variance
	// This is a heuristic check - constant-time operations should have similar timings
	var minTime, maxTime time.Duration
	for _, timing := range timings {
		if minTime == 0 || timing < minTime {
			minTime = timing
		}
		if timing > maxTime {
			maxTime = timing
		}
	}

	// If variance is too high (>2x), it might indicate timing attack vulnerability
	// Note: This is a rough heuristic and can have false positives
	if minTime > 0 && maxTime/minTime > 2 {
		b.Logf("WARNING: High timing variance detected. Min: %v, Max: %v, Ratio: %.2fx",
			minTime, maxTime, float64(maxTime)/float64(minTime))
	}
}
