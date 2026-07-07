// Package learning provides the §8.1 admin-API use cases of the Phase 3
// learning loop: grading (一発確定), the tracker item list, manual retire,
// and the D-20 book review management. The SRS transition itself lives in
// internal/learning (learning.Transition) and is applied inside the
// repository's grading transaction — this package never re-implements it.
package learning

import "errors"

// Sentinel errors. Messages deliberately contain respond.SafeError's safe
// words ("not found", "must be", "cannot be") so they reach the client
// verbatim instead of being masked as internal errors.
var (
	// ErrInvalidResult rejects a grade body whose result is not a manual
	// verdict. 'auto' is the radio batch's word (D-17) and is rejected
	// here too.
	ErrInvalidResult = errors.New("result must be one of good, fuzzy, forgot")

	// ErrInvalidStatus rejects an unknown ?status= filter value.
	ErrInvalidStatus = errors.New("status must be one of active, retired")

	// ErrReviewNotFound: the review log id does not exist (HTTP 404).
	ErrReviewNotFound = errors.New("review not found")

	// ErrReviewAlreadyGraded: the log is already resolved — by a manual
	// grade, the 48h auto-resolve, or a concurrent grade; all one case
	// (HTTP 409, §8.1 一発確定: a recorded grade cannot be changed).
	ErrReviewAlreadyGraded = errors.New("review already graded: the result cannot be changed")

	// ErrItemNotFound: the learning item id does not exist (HTTP 404).
	ErrItemNotFound = errors.New("learning item not found")

	// ErrBookNotFound: the book id does not exist (HTTP 404).
	ErrBookNotFound = errors.New("book not found")
)
