package book

import "errors"

// Use-case errors mapped to HTTP statuses by the handler. The messages
// contain the words respond.SafeError treats as safe to surface.
var (
	// ErrInvalidFilename: the name is not a single, sane path element.
	ErrInvalidFilename = errors.New("invalid filename")
	// ErrNotPDF: extension or %PDF magic check failed (D-25 validation).
	ErrNotPDF = errors.New("invalid file: must be a PDF (.pdf extension and %PDF magic)")
	// ErrTooLarge: the upload exceeds the per-book ceiling (D-25: 100MB).
	ErrTooLarge = errors.New("invalid file: exceeds the upload size limit")
	// ErrNotFound: delete target had no file, no books row and no pending job.
	ErrNotFound = errors.New("book not found")
)
