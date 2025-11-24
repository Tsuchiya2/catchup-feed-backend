// Package text provides utilities for text processing and analysis.
// This package includes reusable functions for character counting and text manipulation
// that can be used across different AI providers and text processing features.
package text

// CountRunes counts the number of Unicode characters (runes) in the given text.
// This function correctly handles multi-byte characters including Japanese, Chinese,
// emoji, and other Unicode characters by counting runes instead of bytes.
//
// This utility is designed to be reused across multiple AI summarization providers
// (Claude, OpenAI, Gemini, etc.) to ensure consistent character counting behavior.
//
// Examples:
//
//	CountRunes("hello")          // returns 5 (ASCII text)
//	CountRunes("こんにちは")       // returns 5 (Japanese text)
//	CountRunes("hello世界")       // returns 7 (mixed text)
//	CountRunes("Hello👋")         // returns 6 (text with emoji)
//	CountRunes("")               // returns 0 (empty string)
func CountRunes(text string) int {
	return len([]rune(text))
}
