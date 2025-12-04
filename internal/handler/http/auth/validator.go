package auth

import (
	"fmt"
	"os"
	"strings"
)

// weakPasswordList contains common weak passwords that must be rejected.
// This list includes the most commonly used passwords and their variations.
var weakPasswordList = []string{
	"admin",
	"password",
	"123456",
	"secret",
	"admin123",
	"password123",
	"123456789",
	"12345678",
	"qwerty",
	"abc123",
	"letmein",
	"welcome",
	"monkey",
	"1234567890",
	"password1",
	"admin1",
	"test",
	"test123",
	"default",
	"root",
}

const (
	// minPasswordLength is the minimum required password length for admin credentials
	minPasswordLength = 12
)

// ValidateAdminCredentials validates admin credentials from environment variables
// at application startup. This function must be called before the server starts
// to prevent security vulnerabilities from empty or weak credentials.
//
// Security requirements:
//   - ADMIN_USER must not be empty
//   - ADMIN_USER_PASSWORD must not be empty
//   - Password must be at least 12 characters
//   - Password must not match any weak password patterns
//
// Returns an error if validation fails with a clear description of the issue.
// The error message is safe to log but does not leak sensitive information.
func ValidateAdminCredentials() error {
	user := os.Getenv("ADMIN_USER")
	pass := os.Getenv("ADMIN_USER_PASSWORD")

	// Check for empty username
	if user == "" {
		return fmt.Errorf("admin credentials validation failed: ADMIN_USER must not be empty")
	}

	// Check for empty password
	if pass == "" {
		return fmt.Errorf("admin credentials validation failed: ADMIN_USER_PASSWORD must not be empty")
	}

	// Check minimum password length
	if len(pass) < minPasswordLength {
		return fmt.Errorf("admin credentials validation failed: ADMIN_USER_PASSWORD must be at least %d characters (current length: %d)", minPasswordLength, len(pass))
	}

	// Check for simple numeric patterns (before checking weak password list)
	// This prevents numeric sequences from being caught by weak password prefix check
	if isSimpleNumericPattern(pass) {
		return fmt.Errorf("admin credentials validation failed: ADMIN_USER_PASSWORD must not be a simple numeric pattern")
	}

	// Check for keyboard patterns (before checking weak password list)
	// This prevents keyboard patterns from being caught by weak password prefix check
	if isKeyboardPattern(pass) {
		return fmt.Errorf("admin credentials validation failed: ADMIN_USER_PASSWORD must not be a keyboard pattern")
	}

	// Check against weak password blacklist
	// This includes both exact matches and common prefix patterns
	lowerPass := strings.ToLower(pass)
	for _, weak := range weakPasswordList {
		// Exact match check (case-insensitive)
		if lowerPass == weak {
			return fmt.Errorf("admin credentials validation failed: ADMIN_USER_PASSWORD must not be a weak password")
		}

		// Check if password starts with a weak pattern
		// This catches variations like "admin1234567890"
		if strings.HasPrefix(lowerPass, weak) && len(pass) < minPasswordLength+5 {
			return fmt.Errorf("admin credentials validation failed: ADMIN_USER_PASSWORD must not be based on common weak passwords")
		}
	}

	return nil
}

// isSimpleNumericPattern checks if the password is a simple numeric sequence.
// Examples: "111111111111", "123123123123"
func isSimpleNumericPattern(pass string) bool {
	if len(pass) < minPasswordLength {
		return false
	}

	// Check for repeated digits
	if isRepeatedChar(pass) {
		return true
	}

	// Check for simple sequences like "123456789012"
	hasOnlyDigits := true
	for _, ch := range pass {
		if ch < '0' || ch > '9' {
			hasOnlyDigits = false
			break
		}
	}

	if !hasOnlyDigits {
		return false
	}

	// Check for ascending or descending sequences
	isAscending := true
	isDescending := true
	for i := 1; i < len(pass); i++ {
		diff := int(pass[i]) - int(pass[i-1])
		// Ascending: diff is 1 or -9 (wraps 9->0)
		if diff != 1 && diff != -9 {
			isAscending = false
		}
		// Descending: diff is -1 or 9 (wraps 0->9)
		if diff != -1 && diff != 9 {
			isDescending = false
		}
	}

	return isAscending || isDescending
}

// isRepeatedChar checks if the password consists of a single repeated character.
// Example: "aaaaaaaaaaaa"
func isRepeatedChar(pass string) bool {
	if len(pass) == 0 {
		return false
	}

	first := pass[0]
	for i := 1; i < len(pass); i++ {
		if pass[i] != first {
			return false
		}
	}
	return true
}

// isKeyboardPattern checks if the password is a keyboard pattern.
// Examples: "qwertyuiop", "asdfghjkl"
var keyboardPatterns = []string{
	"qwertyuiop",
	"asdfghjkl",
	"zxcvbnm",
	"qwerty",
	"asdfgh",
	"zxcvb",
}

func isKeyboardPattern(pass string) bool {
	lowerPass := strings.ToLower(pass)

	for _, pattern := range keyboardPatterns {
		if strings.Contains(lowerPass, pattern) {
			return true
		}
		// Check reverse pattern
		if strings.Contains(lowerPass, reverse(pattern)) {
			return true
		}
	}

	return false
}

// reverse returns the reversed string
func reverse(s string) string {
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return string(runes)
}

// ValidateViewerCredentials validates viewer credentials at startup.
// It implements graceful degradation: if viewer credentials are misconfigured,
// the viewer role is disabled but the application continues to run in admin-only mode.
//
// Validation Logic:
//  1. If DEMO_USER is empty → log INFO "viewer role not configured - running in admin-only mode", return nil
//  2. If DEMO_USER is set but DEMO_USER_PASSWORD is empty → log WARN "DEMO_USER_PASSWORD is empty - disabling viewer role", unset DEMO_USER, return nil
//  3. If DEMO_USER == ADMIN_USER → log WARN "DEMO_USER cannot be the same as ADMIN_USER - disabling viewer role", unset DEMO_USER, return nil
//  4. If DEMO_USER_PASSWORD < 12 chars → log WARN "DEMO_USER_PASSWORD must be at least 12 characters - disabling viewer role", unset both, return nil
//  5. If DEMO_USER_PASSWORD is a weak password → log WARN with message, unset both, return nil
//  6. Otherwise → log INFO "viewer role configured successfully for {DEMO_USER}", return nil
//
// Graceful Degradation: NEVER fails startup (always returns nil), just disables viewer role on misconfiguration.
func ValidateViewerCredentials(logger interface{ Info(msg string, args ...any); Warn(msg string, args ...any) }) error {
	demoUser := os.Getenv("DEMO_USER")
	demoPass := os.Getenv("DEMO_USER_PASSWORD")
	adminUser := os.Getenv("ADMIN_USER")

	// Case 1: DEMO_USER not configured - admin-only mode
	if demoUser == "" {
		logger.Info("viewer role not configured - running in admin-only mode")
		return nil
	}

	// Case 2: DEMO_USER set but password empty
	if demoPass == "" {
		logger.Warn("DEMO_USER_PASSWORD is empty - disabling viewer role")
		_ = os.Unsetenv("DEMO_USER")
		return nil
	}

	// Case 3: DEMO_USER same as ADMIN_USER
	if demoUser == adminUser {
		logger.Warn("DEMO_USER cannot be the same as ADMIN_USER - disabling viewer role")
		_ = os.Unsetenv("DEMO_USER")
		_ = os.Unsetenv("DEMO_USER_PASSWORD")
		return nil
	}

	// Case 4: Password too short
	if len(demoPass) < minPasswordLength {
		logger.Warn("DEMO_USER_PASSWORD must be at least 12 characters - disabling viewer role")
		_ = os.Unsetenv("DEMO_USER")
		_ = os.Unsetenv("DEMO_USER_PASSWORD")
		return nil
	}

	// Case 5: Weak password check
	lowerPass := strings.ToLower(demoPass)
	for _, weak := range weakPasswordList {
		// Exact match or starts with weak password
		if lowerPass == weak || strings.HasPrefix(lowerPass, weak) {
			logger.Warn("DEMO_USER_PASSWORD is a weak password - disabling viewer role",
				"hint", "avoid common passwords")
			_ = os.Unsetenv("DEMO_USER")
			_ = os.Unsetenv("DEMO_USER_PASSWORD")
			return nil
		}
	}

	// Success: viewer role configured
	logger.Info("viewer role configured successfully", "user", demoUser)
	return nil
}
