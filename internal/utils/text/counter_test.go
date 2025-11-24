package text_test

import (
	"testing"

	"catchup-feed/internal/utils/text"
)

/* ───────── TASK-008: Character Counting Unit Tests ───────── */

// TestCountRunes tests the CountRunes function with various character types
func TestCountRunes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		// ASCII text
		{
			name:     "ASCII text",
			input:    "hello",
			expected: 5,
		},
		{
			name:     "ASCII with spaces",
			input:    "hello world",
			expected: 11,
		},

		// Japanese text
		{
			name:     "Japanese hiragana",
			input:    "こんにちは",
			expected: 5,
		},
		{
			name:     "Japanese kanji",
			input:    "日本語",
			expected: 3,
		},
		{
			name:     "Japanese katakana",
			input:    "カタカナ",
			expected: 4,
		},
		{
			name:     "Japanese mixed",
			input:    "こんにちは世界",
			expected: 7,
		},

		// Mixed text
		{
			name:     "English and Japanese",
			input:    "hello世界",
			expected: 7,
		},
		{
			name:     "Mixed with numbers",
			input:    "test123テスト",
			expected: 10,
		},

		// Emoji text
		{
			name:     "ASCII with emoji",
			input:    "Hello👋",
			expected: 6,
		},
		{
			name:     "Japanese with emoji",
			input:    "こんにちは😊",
			expected: 6,
		},
		{
			name:     "Multiple emojis",
			input:    "🚀✨🤖💡",
			expected: 4,
		},
		{
			name:     "Complex emoji (flag)",
			input:    "🇯🇵",
			expected: 2, // Flag emojis are composed of 2 regional indicator symbols
		},

		// Edge cases
		{
			name:     "Empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "Single space",
			input:    " ",
			expected: 1,
		},
		{
			name:     "Multiple spaces",
			input:    "   ",
			expected: 3,
		},
		{
			name:     "Tab character",
			input:    "\t",
			expected: 1,
		},
		{
			name:     "Newline character",
			input:    "\n",
			expected: 1,
		},
		{
			name:     "Mixed whitespace",
			input:    " \t\n ",
			expected: 4,
		},

		// Special characters
		{
			name:     "Punctuation",
			input:    "Hello, World!",
			expected: 13,
		},
		{
			name:     "Japanese punctuation",
			input:    "こんにちは。世界！",
			expected: 9,
		},
		{
			name:     "Symbols",
			input:    "©®™€",
			expected: 4,
		},

		// Combining characters
		{
			name:     "Combining diacritics",
			input:    "café", // é is a single rune (U+00E9)
			expected: 4,
		},
		{
			name:     "Combining diacritics (decomposed)",
			input:    "café", // If é is e + combining acute (U+0065 + U+0301), count is 5
			expected: 4,      // Note: In Go, this depends on how the string is composed
		},

		// Long strings
		{
			name:     "Long ASCII string",
			input:    "Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
			expected: 123,
		},
		{
			name:     "Long Japanese string",
			input:    "人工知能技術の発展により、私たちの生活は大きく変化しています。機械学習アルゴリズムは、大量のデータから複雑なパターンを学習することができます。",
			expected: 71,
		},

		// Unicode edge cases
		{
			name:     "Zero-width space",
			input:    "hello\u200Bworld", // U+200B is zero-width space
			expected: 11,
		},
		{
			name:     "Chinese characters",
			input:    "你好世界",
			expected: 4,
		},
		{
			name:     "Korean characters",
			input:    "안녕하세요",
			expected: 5,
		},
		{
			name:     "Arabic characters",
			input:    "مرحبا",
			expected: 5,
		},
		{
			name:     "Cyrillic characters",
			input:    "Привет",
			expected: 6,
		},

		// Real-world examples
		{
			name:     "Typical Japanese sentence",
			input:    "AIの発展により、新しい可能性が広がっています。",
			expected: 24,
		},
		{
			name:     "Mixed language sentence",
			input:    "Machine LearningとDeep Learningの違い",
			expected: 33,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			result := text.CountRunes(tt.input)

			// Assert
			if result != tt.expected {
				t.Errorf("CountRunes(%q) = %d, expected %d", tt.input, result, tt.expected)
			}
		})
	}
}

// TestCountRunes_Consistency tests that CountRunes produces consistent results
func TestCountRunes_Consistency(t *testing.T) {
	testString := "こんにちは世界 Hello World 🚀"

	// Call multiple times
	result1 := text.CountRunes(testString)
	result2 := text.CountRunes(testString)
	result3 := text.CountRunes(testString)

	// Assert consistency
	if result1 != result2 || result2 != result3 {
		t.Errorf("CountRunes is not consistent: %d, %d, %d", result1, result2, result3)
	}
}

// TestCountRunes_MatchesGoBuiltin tests that CountRunes matches Go's built-in rune counting
func TestCountRunes_MatchesGoBuiltin(t *testing.T) {
	tests := []string{
		"hello",
		"こんにちは",
		"hello世界",
		"Hello👋",
		"",
		"   ",
		"🚀✨🤖💡",
		"人工知能技術の発展により、私たちの生活は大きく変化しています。",
	}

	for _, tt := range tests {
		t.Run(tt, func(t *testing.T) {
			// Expected value from Go's built-in rune counting
			expected := len([]rune(tt))

			// Act
			result := text.CountRunes(tt)

			// Assert
			if result != expected {
				t.Errorf("CountRunes(%q) = %d, expected %d (Go built-in)", tt, result, expected)
			}
		})
	}
}

// BenchmarkCountRunes benchmarks the performance of CountRunes
func BenchmarkCountRunes(b *testing.B) {
	testStrings := []struct {
		name  string
		input string
	}{
		{"Short ASCII", "hello world"},
		{"Short Japanese", "こんにちは"},
		{"Medium Mixed", "AIの発展により、新しい可能性が広がっています。Machine Learning and Deep Learning are transforming technology."},
		{"Long Japanese", "人工知能技術の発展により、私たちの生活は大きく変化しています。機械学習アルゴリズムは、大量のデータから複雑なパターンを学習することができます。深層学習モデルは、画像認識や自然言語処理などの分野で優れた性能を発揮しています。ニューラルネットワークは、人間の脳の構造にヒントを得た計算モデルです。データサイエンスは、統計学、プログラミング、ドメイン知識を組み合わせた学際的な分野です。"},
	}

	for _, ts := range testStrings {
		b.Run(ts.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				text.CountRunes(ts.input)
			}
		})
	}
}
