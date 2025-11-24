// Package article provides use cases for managing article entities.
// It implements business logic for creating, updating, deleting, and querying articles,
// including validation and interaction with the article repository.
package article

import "errors"

// Sentinel errors for article use case operations.
var (
	// ErrArticleNotFound indicates that the requested article was not found.
	// This error is typically returned when attempting to retrieve or update
	// an article that does not exist in the repository.
	ErrArticleNotFound = errors.New("article not found")

	// ErrInvalidArticleID indicates that the provided article ID is invalid.
	// Article IDs must be positive integers.
	ErrInvalidArticleID = errors.New("invalid article ID")

	// ErrDuplicateArticle indicates that an article with the same URL already exists.
	// This prevents duplicate articles from being created in the system.
	ErrDuplicateArticle = errors.New("article with this URL already exists")
)
