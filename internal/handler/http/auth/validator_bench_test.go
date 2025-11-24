package auth

import (
	"os"
	"testing"
)

// BenchmarkValidateAdminCredentials_Success benchmarks the validation with valid credentials.
// Target: <10ms startup overhead
func BenchmarkValidateAdminCredentials_Success(b *testing.B) {
	_ = os.Setenv("ADMIN_USER", "admin")
	_ = os.Setenv("ADMIN_USER_PASSWORD", "MyStr0ng!Pass@2024")
	defer func() {
		_ = os.Unsetenv("ADMIN_USER")
		_ = os.Unsetenv("ADMIN_USER_PASSWORD")
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateAdminCredentials()
	}
}

// BenchmarkValidateAdminCredentials_WeakPassword benchmarks detection of weak passwords.
func BenchmarkValidateAdminCredentials_WeakPassword(b *testing.B) {
	_ = os.Setenv("ADMIN_USER", "admin")
	_ = os.Setenv("ADMIN_USER_PASSWORD", "admin123456789")
	defer func() {
		_ = os.Unsetenv("ADMIN_USER")
		_ = os.Unsetenv("ADMIN_USER_PASSWORD")
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateAdminCredentials()
	}
}

// BenchmarkValidateAdminCredentials_NumericPattern benchmarks detection of numeric patterns.
func BenchmarkValidateAdminCredentials_NumericPattern(b *testing.B) {
	_ = os.Setenv("ADMIN_USER", "admin")
	_ = os.Setenv("ADMIN_USER_PASSWORD", "123456789012")
	defer func() {
		_ = os.Unsetenv("ADMIN_USER")
		_ = os.Unsetenv("ADMIN_USER_PASSWORD")
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateAdminCredentials()
	}
}

// BenchmarkValidateAdminCredentials_KeyboardPattern benchmarks detection of keyboard patterns.
func BenchmarkValidateAdminCredentials_KeyboardPattern(b *testing.B) {
	_ = os.Setenv("ADMIN_USER", "admin")
	_ = os.Setenv("ADMIN_USER_PASSWORD", "qwertyuiopas")
	defer func() {
		_ = os.Unsetenv("ADMIN_USER")
		_ = os.Unsetenv("ADMIN_USER_PASSWORD")
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateAdminCredentials()
	}
}

// BenchmarkIsSimpleNumericPattern benchmarks numeric pattern detection.
func BenchmarkIsSimpleNumericPattern(b *testing.B) {
	testCases := []struct {
		name string
		pass string
	}{
		{"repeated", "111111111111"},
		{"ascending", "123456789012"},
		{"descending", "987654321098"},
		{"random", "192837465012"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = isSimpleNumericPattern(tc.pass)
			}
		})
	}
}

// BenchmarkIsKeyboardPattern benchmarks keyboard pattern detection.
func BenchmarkIsKeyboardPattern(b *testing.B) {
	testCases := []struct {
		name string
		pass string
	}{
		{"qwerty", "qwertyuiopas"},
		{"asdf", "asdfghjklqwe"},
		{"no_pattern", "randompassword123"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = isKeyboardPattern(tc.pass)
			}
		})
	}
}

// BenchmarkIsRepeatedChar benchmarks repeated character detection.
func BenchmarkIsRepeatedChar(b *testing.B) {
	testCases := []struct {
		name string
		pass string
	}{
		{"repeated_a", "aaaaaaaaaaaa"},
		{"repeated_0", "000000000000"},
		{"mixed", "aabbaabbaabb"},
	}

	for _, tc := range testCases {
		b.Run(tc.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = isRepeatedChar(tc.pass)
			}
		})
	}
}
