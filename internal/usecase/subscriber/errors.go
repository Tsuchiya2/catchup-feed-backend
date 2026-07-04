// Package subscriber provides the friend-management use cases (§5.1
// admin API): subscriber CRUD (logical deletion only, C-8) and the feed
// token lifecycle (issue / revoke, §5.2, D-5).
package subscriber

import "errors"

// Sentinel errors. Messages deliberately contain respond.SafeError's safe
// words ("not found", "required", "cannot be") so they reach the client
// verbatim instead of being masked as internal errors.
var (
	// ErrSubscriberNotFound indicates the subscriber does not exist.
	ErrSubscriberNotFound = errors.New("subscriber not found")

	// ErrSubscriberDeactivated indicates the operation conflicts with the
	// subscriber's deactivated state (e.g. issuing a token, HTTP 409).
	ErrSubscriberDeactivated = errors.New("token cannot be issued: subscriber is deactivated")

	// ErrTokenNotFound indicates the feed token does not exist.
	ErrTokenNotFound = errors.New("token not found")

	// ErrNameRequired indicates a missing subscriber name.
	ErrNameRequired = errors.New("name is required")
)
