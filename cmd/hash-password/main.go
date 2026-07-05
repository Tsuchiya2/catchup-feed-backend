// Command hash-password generates a bcrypt hash for the ADMIN_PASSWORD_HASH
// environment variable (C-7/C-20: 管理者資格情報は環境変数+bcrypt)。
//
// Usage:
//
//	make admin-hash
//	printf '%s' 'your-password' | go run ./cmd/hash-password
//
// The password is read from stdin (first line, without the trailing
// newline); the hash is written to stdout. Note that an interactively typed
// password is echoed to the terminal — prefer piping from a password
// manager or use the printed hash and clear your scrollback.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	// bcryptCost is the cost used for generated hashes. Login happens a few
	// times a day on a Raspberry Pi 5, so a cost above the default is
	// affordable.
	bcryptCost = 12

	// minPasswordLength is enforced at generation time because the server
	// only ever sees the hash and cannot check password strength.
	minPasswordLength = 12

	// maxPasswordLength is the bcrypt input limit (72 bytes).
	maxPasswordLength = 72
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if isTerminal(os.Stdin) {
		fmt.Fprint(os.Stderr, "Password (echoed): ")
	}

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("failed to read password from stdin: %w", err)
	}
	password := strings.TrimRight(line, "\r\n")

	if len(password) < minPasswordLength {
		return fmt.Errorf("password must be at least %d characters", minPasswordLength)
	}
	if len(password) > maxPasswordLength {
		return fmt.Errorf("password must be at most %d bytes (bcrypt limit)", maxPasswordLength)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return fmt.Errorf("failed to generate bcrypt hash: %w", err)
	}

	fmt.Println(string(hash))
	fmt.Fprintln(os.Stderr, "Set this value as ADMIN_PASSWORD_HASH. In .env files read by docker compose, escape each '$' as '$$'.")
	return nil
}

// isTerminal reports whether f is attached to a terminal, without pulling in
// golang.org/x/term for a prompt-only decision.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
