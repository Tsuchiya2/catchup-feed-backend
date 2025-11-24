// Package source provides use cases for managing news feed sources.
// It implements business logic for creating, updating, deleting, and querying sources,
// including validation and interaction with the source repository.
package source

import "errors"

// Sentinel errors for source use case operations.
var (
	// ErrSourceNotFound indicates that the requested source was not found.
	// This error is typically returned when attempting to retrieve or update
	// a source that does not exist in the repository.
	ErrSourceNotFound = errors.New("source not found")

	// ErrInvalidSourceURL indicates that the provided source URL is invalid.
	// Source URLs must be valid HTTP/HTTPS URLs with proper format.
	ErrInvalidSourceURL = errors.New("invalid source URL")

	// ErrDuplicateSource indicates that a source with the same feed URL already exists.
	// This prevents duplicate sources from being created in the system.
	ErrDuplicateSource = errors.New("source with this feed URL already exists")
)
