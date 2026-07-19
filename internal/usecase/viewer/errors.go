// Package viewer provides the read-only account use cases (D-27):
// admin-managed viewer CRUD (create / update / activate / deactivate /
// physical delete) plus the login-time and per-request credential checks the
// auth layer delegates here.
package viewer

import "errors"

// Sentinel errors. Messages deliberately contain respond.SafeError's safe
// words ("not found", "required", "invalid", "already") so they reach the
// client verbatim instead of being masked as internal errors.
var (
	// ErrViewerNotFound indicates the viewer does not exist.
	ErrViewerNotFound = errors.New("viewer not found")

	// ErrNameRequired indicates a missing viewer name.
	ErrNameRequired = errors.New("name is required")

	// ErrInvalidEmail indicates a malformed viewer email address. The email
	// is the login identifier, so it is validated at the door.
	ErrInvalidEmail = errors.New("email is invalid")

	// ErrEmailTaken indicates another viewer already uses the email
	// (viewers.email UNIQUE, HTTP 409).
	ErrEmailTaken = errors.New("email is already registered")

	// ErrPasswordTooShort indicates the password is shorter than
	// MinPasswordLength.
	ErrPasswordTooShort = errors.New("password is required and must be at least 8 characters")

	// ErrPasswordTooLong indicates the password exceeds bcrypt's 72-byte
	// input limit (MaxPasswordLength). Rejected at validation (400) so it
	// never reaches bcrypt.GenerateFromPassword's ErrPasswordTooLong (500).
	ErrPasswordTooLong = errors.New("password is invalid: must be at most 72 bytes")

	// ErrInvalidCredentials is the generic login failure: unknown email,
	// wrong password or deactivated viewer. Deliberately indistinguishable
	// so login responses do not enumerate accounts.
	ErrInvalidCredentials = errors.New("invalid credentials")
)
