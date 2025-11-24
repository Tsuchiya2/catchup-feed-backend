package auth

import (
	"os"
	"strings"
	"testing"
)

func TestValidateAdminCredentials(t *testing.T) {
	tests := []struct {
		name          string
		user          string
		pass          string
		wantErr       bool
		errorContains string
	}{
		// Empty credential tests
		{
			name:          "empty username",
			user:          "",
			pass:          "StrongPassword123!@#",
			wantErr:       true,
			errorContains: "ADMIN_USER must not be empty",
		},
		{
			name:          "empty password",
			user:          "admin",
			pass:          "",
			wantErr:       true,
			errorContains: "ADMIN_USER_PASSWORD must not be empty",
		},
		{
			name:          "both empty",
			user:          "",
			pass:          "",
			wantErr:       true,
			errorContains: "ADMIN_USER must not be empty",
		},

		// Password length tests
		{
			name:          "password too short - 11 chars",
			user:          "admin",
			pass:          "Short123!@#",
			wantErr:       true,
			errorContains: "must be at least 12 characters",
		},
		{
			name:          "password too short - 1 char",
			user:          "admin",
			pass:          "a",
			wantErr:       true,
			errorContains: "must be at least 12 characters",
		},
		{
			name:          "password exactly 12 chars - valid",
			user:          "admin",
			pass:          "ValidPass12!",
			wantErr:       false,
			errorContains: "",
		},
		{
			name:          "password 13 chars - valid",
			user:          "admin",
			pass:          "ValidPass123!",
			wantErr:       false,
			errorContains: "",
		},

		// Weak password exact match tests
		{
			name:          "weak password - admin",
			user:          "admin",
			pass:          "admin",
			wantErr:       true,
			errorContains: "must be at least 12 characters", // Caught by length first
		},
		{
			name:          "weak password - password",
			user:          "admin",
			pass:          "password",
			wantErr:       true,
			errorContains: "must be at least 12 characters", // Caught by length first
		},
		{
			name:          "weak password - 123456",
			user:          "admin",
			pass:          "123456",
			wantErr:       true,
			errorContains: "must be at least 12 characters", // Caught by length first
		},
		{
			name:          "weak password - secret",
			user:          "admin",
			pass:          "secret",
			wantErr:       true,
			errorContains: "must be at least 12 characters", // Caught by length first
		},

		// Weak password with sufficient length tests
		{
			name:          "weak password padded - admin123456789",
			user:          "admin",
			pass:          "admin123456789",
			wantErr:       true,
			errorContains: "must not be based on common weak passwords",
		},
		{
			name:          "weak password padded - password1234",
			user:          "admin",
			pass:          "password1234",
			wantErr:       true,
			errorContains: "must not be based on common weak passwords",
		},
		{
			name:          "weak password case variation - ADMIN12345678",
			user:          "admin",
			pass:          "ADMIN12345678",
			wantErr:       true,
			errorContains: "must not be based on common weak passwords",
		},
		{
			name:          "weak password case variation - Password1234",
			user:          "admin",
			pass:          "Password1234",
			wantErr:       true,
			errorContains: "must not be based on common weak passwords",
		},

		// Numeric pattern tests
		{
			name:          "simple numeric - 111111111111",
			user:          "admin",
			pass:          "111111111111",
			wantErr:       true,
			errorContains: "must not be a simple numeric pattern",
		},
		{
			name:          "simple numeric - 000000000000",
			user:          "admin",
			pass:          "000000000000",
			wantErr:       true,
			errorContains: "must not be a simple numeric pattern",
		},
		{
			name:          "ascending sequence - 123456789012",
			user:          "admin",
			pass:          "123456789012",
			wantErr:       true,
			errorContains: "must not be a simple numeric pattern",
		},

		// Keyboard pattern tests
		{
			name:          "keyboard pattern - qwertyuiopas",
			user:          "admin",
			pass:          "qwertyuiopas",
			wantErr:       true,
			errorContains: "must not be a keyboard pattern",
		},
		{
			name:          "keyboard pattern - asdfghjklqwe",
			user:          "admin",
			pass:          "asdfghjklqwe",
			wantErr:       true,
			errorContains: "must not be a keyboard pattern",
		},
		{
			name:          "keyboard pattern uppercase - QWERTYUIOPAS",
			user:          "admin",
			pass:          "QWERTYUIOPAS",
			wantErr:       true,
			errorContains: "must not be a keyboard pattern",
		},

		// Valid strong password tests
		{
			name:          "valid strong password - mixed case with symbols",
			user:          "admin",
			pass:          "MyStr0ng!Pass@2024",
			wantErr:       false,
			errorContains: "",
		},
		{
			name:          "valid strong password - long random",
			user:          "admin",
			pass:          "xK9$mP2@nQ5#vR8&",
			wantErr:       false,
			errorContains: "",
		},
		{
			name:          "valid strong password - passphrase style",
			user:          "admin",
			pass:          "CorrectHorseBatteryStaple42!",
			wantErr:       false,
			errorContains: "",
		},
		{
			name:          "valid strong password - exactly 12 chars with special chars",
			user:          "admin",
			pass:          "aB3$fG7&jK0#",
			wantErr:       false,
			errorContains: "",
		},
		{
			name:          "valid strong password - with spaces",
			user:          "admin",
			pass:          "My Super Secret Pass 2024!",
			wantErr:       false,
			errorContains: "",
		},

		// Edge cases
		{
			name:          "valid - non-english characters",
			user:          "admin",
			pass:          "パスワード安全12345!",
			wantErr:       false,
			errorContains: "",
		},
		{
			name:          "valid - emoji in password",
			user:          "admin",
			pass:          "MyPass🔒2024!Strong",
			wantErr:       false,
			errorContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup environment
			_ = os.Setenv("ADMIN_USER", tt.user)
			_ = os.Setenv("ADMIN_USER_PASSWORD", tt.pass)
			defer func() { _ = os.Unsetenv("ADMIN_USER") }()
			defer func() { _ = os.Unsetenv("ADMIN_USER_PASSWORD") }()

			// Execute validation
			err := ValidateAdminCredentials()

			// Verify result
			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateAdminCredentials() expected error but got nil")
					return
				}
				if tt.errorContains != "" && !strings.Contains(err.Error(), tt.errorContains) {
					t.Errorf("ValidateAdminCredentials() error = %v, should contain %q", err, tt.errorContains)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateAdminCredentials() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestIsSimpleNumericPattern(t *testing.T) {
	tests := []struct {
		name string
		pass string
		want bool
	}{
		{
			name: "all same digit",
			pass: "111111111111",
			want: true,
		},
		{
			name: "all zeros",
			pass: "000000000000",
			want: true,
		},
		{
			name: "ascending sequence",
			pass: "123456789012",
			want: true,
		},
		{
			name: "descending sequence",
			pass: "987654321098",
			want: true,
		},
		{
			name: "mixed digits - not pattern",
			pass: "192837465012",
			want: false,
		},
		{
			name: "contains letters",
			pass: "1234567890ab",
			want: false,
		},
		{
			name: "too short",
			pass: "12345",
			want: false,
		},
		{
			name: "random numbers",
			pass: "847293016582",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSimpleNumericPattern(tt.pass); got != tt.want {
				t.Errorf("isSimpleNumericPattern(%q) = %v, want %v", tt.pass, got, tt.want)
			}
		})
	}
}

func TestIsRepeatedChar(t *testing.T) {
	tests := []struct {
		name string
		pass string
		want bool
	}{
		{
			name: "all same letter",
			pass: "aaaaaaaaaa",
			want: true,
		},
		{
			name: "all same digit",
			pass: "0000000000",
			want: true,
		},
		{
			name: "mixed characters",
			pass: "aaabaaaa",
			want: false,
		},
		{
			name: "single character",
			pass: "a",
			want: true,
		},
		{
			name: "empty string",
			pass: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRepeatedChar(tt.pass); got != tt.want {
				t.Errorf("isRepeatedChar(%q) = %v, want %v", tt.pass, got, tt.want)
			}
		})
	}
}

func TestIsKeyboardPattern(t *testing.T) {
	tests := []struct {
		name string
		pass string
		want bool
	}{
		{
			name: "qwerty pattern",
			pass: "qwertyuiop",
			want: true,
		},
		{
			name: "qwerty uppercase",
			pass: "QWERTYUIOP",
			want: true,
		},
		{
			name: "asdf pattern",
			pass: "asdfghjkl",
			want: true,
		},
		{
			name: "qwerty in password",
			pass: "myqwertypass",
			want: true,
		},
		{
			name: "reverse qwerty",
			pass: "poiuytrewq",
			want: true,
		},
		{
			name: "no keyboard pattern",
			pass: "randompassword",
			want: false,
		},
		{
			name: "mixed with numbers",
			pass: "pass123word456",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isKeyboardPattern(tt.pass); got != tt.want {
				t.Errorf("isKeyboardPattern(%q) = %v, want %v", tt.pass, got, tt.want)
			}
		})
	}
}

func TestReverse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple string",
			input: "hello",
			want:  "olleh",
		},
		{
			name:  "single character",
			input: "a",
			want:  "a",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "with numbers",
			input: "abc123",
			want:  "321cba",
		},
		{
			name:  "unicode characters",
			input: "こんにちは",
			want:  "はちにんこ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reverse(tt.input); got != tt.want {
				t.Errorf("reverse(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestWeakPasswordList verifies that all passwords in the weak password list
// would be rejected by the validator (either by length or pattern check)
func TestWeakPasswordList(t *testing.T) {
	for _, weak := range weakPasswordList {
		t.Run("weak_password_"+weak, func(t *testing.T) {
			_ = os.Setenv("ADMIN_USER", "testuser")
			_ = os.Setenv("ADMIN_USER_PASSWORD", weak)
			defer func() { _ = os.Unsetenv("ADMIN_USER") }()
			defer func() { _ = os.Unsetenv("ADMIN_USER_PASSWORD") }()

			err := ValidateAdminCredentials()
			if err == nil {
				t.Errorf("Expected weak password %q to be rejected, but it was accepted", weak)
			}
		})
	}
}

// TestRealWorldStrongPasswords tests that realistic strong passwords are accepted
func TestRealWorldStrongPasswords(t *testing.T) {
	strongPasswords := []string{
		"MyC0mplex!Pass@2024",
		"xK9$mP2@nQ5#vR8&wL3%",
		"CorrectHorseBatteryStaple42!",
		"Tr0ub4dor&3Extended",
		"aB3$fG7&jK0#mN9^",
		"!QAZ2wsx#EDC4rfv",
		"P@ssw0rd!Strength#2024",
		"MySuper$ecureP@ss123",
	}

	for _, pass := range strongPasswords {
		t.Run("strong_password_"+pass[:8], func(t *testing.T) {
			_ = os.Setenv("ADMIN_USER", "admin")
			_ = os.Setenv("ADMIN_USER_PASSWORD", pass)
			defer func() { _ = os.Unsetenv("ADMIN_USER") }()
			defer func() { _ = os.Unsetenv("ADMIN_USER_PASSWORD") }()

			err := ValidateAdminCredentials()
			if err != nil {
				t.Errorf("Expected strong password %q to be accepted, but got error: %v", pass, err)
			}
		})
	}
}
