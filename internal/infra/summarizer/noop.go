// Package summarizer provides AI-powered text summarization implementations.
package summarizer

import (
	"context"
)

// NoOp is a summarizer that returns the original text without modification.
// This is useful for testing and development when summarization is not needed.
type NoOp struct{}

// NewNoOp creates a new NoOp summarizer.
func NewNoOp() *NoOp {
	return &NoOp{}
}

// Summarize returns the original text truncated to a reasonable length.
// For the NoOp summarizer, we truncate to the first 500 characters
// to match the expected summary format.
func (n *NoOp) Summarize(_ context.Context, text string) (string, error) {
	const maxLength = 500
	if len(text) <= maxLength {
		return text, nil
	}
	// Truncate to maxLength and add ellipsis
	return text[:maxLength] + "...", nil
}
