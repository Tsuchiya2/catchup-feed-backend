package fixtures_test

import (
	"testing"

	"catchup-feed/internal/utils/text"
	"catchup-feed/tests/fixtures"
)

// TestGenerateShortArticle tests that short article generation produces correct length
func TestGenerateShortArticle(t *testing.T) {
	article := fixtures.GenerateShortArticle()

	length := text.CountRunes(article)
	expectedMin := 450 // 500 - 10%
	expectedMax := 550 // 500 + 10%

	if length < expectedMin || length > expectedMax {
		t.Errorf("Expected length between %d and %d, got %d", expectedMin, expectedMax, length)
	}

	// Verify it's not empty
	if article == "" {
		t.Error("Generated article is empty")
	}
}

// TestGenerateMediumArticle tests that medium article generation produces correct length
func TestGenerateMediumArticle(t *testing.T) {
	article := fixtures.GenerateMediumArticle()

	length := text.CountRunes(article)
	expectedMin := 1800 // 2000 - 10%
	expectedMax := 2200 // 2000 + 10%

	if length < expectedMin || length > expectedMax {
		t.Errorf("Expected length between %d and %d, got %d", expectedMin, expectedMax, length)
	}

	if article == "" {
		t.Error("Generated article is empty")
	}
}

// TestGenerateLongArticle tests that long article generation produces correct length
func TestGenerateLongArticle(t *testing.T) {
	article := fixtures.GenerateLongArticle()

	length := text.CountRunes(article)
	expectedMin := 9000  // 10000 - 10%
	expectedMax := 11000 // 10000 + 10%

	if length < expectedMin || length > expectedMax {
		t.Errorf("Expected length between %d and %d, got %d", expectedMin, expectedMax, length)
	}

	if article == "" {
		t.Error("Generated article is empty")
	}
}

// TestGenerateArticleWithEmoji tests that emoji article contains emoji characters
func TestGenerateArticleWithEmoji(t *testing.T) {
	article := fixtures.GenerateArticleWithEmoji()

	if article == "" {
		t.Error("Generated article is empty")
	}

	// Check for emoji presence (simple heuristic)
	hasEmoji := false
	for _, r := range article {
		// Emoji ranges (simplified)
		if r >= 0x1F300 && r <= 0x1F9FF { // Miscellaneous Symbols and Pictographs, Emoticons, etc.
			hasEmoji = true
			break
		}
	}

	if !hasEmoji {
		t.Error("Article with emoji should contain at least one emoji character")
	}
}

// TestGenerateArticle_Japanese tests Japanese article generation
func TestGenerateArticle_Japanese(t *testing.T) {
	article := fixtures.GenerateArticle(fixtures.ArticleOptions{
		Length:       1000,
		Language:     "japanese",
		IncludeEmoji: false,
	})

	length := text.CountRunes(article)

	if length < 900 || length > 1100 {
		t.Errorf("Expected length around 1000 (±10%%), got %d", length)
	}

	// Check for Japanese characters
	hasJapanese := false
	for _, r := range article {
		if (r >= 0x3040 && r <= 0x309F) || // Hiragana
			(r >= 0x30A0 && r <= 0x30FF) || // Katakana
			(r >= 0x4E00 && r <= 0x9FFF) { // Kanji
			hasJapanese = true
			break
		}
	}

	if !hasJapanese {
		t.Error("Japanese article should contain Japanese characters")
	}
}

// TestGenerateArticle_English tests English article generation
func TestGenerateArticle_English(t *testing.T) {
	article := fixtures.GenerateArticle(fixtures.ArticleOptions{
		Length:       1000,
		Language:     "english",
		IncludeEmoji: false,
	})

	length := text.CountRunes(article)

	if length < 900 || length > 1100 {
		t.Errorf("Expected length around 1000 (±10%%), got %d", length)
	}

	if article == "" {
		t.Error("Generated article is empty")
	}
}

// TestGenerateArticle_Consistency tests that generated articles are consistent
func TestGenerateArticle_Consistency(t *testing.T) {
	opts := fixtures.ArticleOptions{
		Length:       500,
		Language:     "japanese",
		IncludeEmoji: false,
	}

	article1 := fixtures.GenerateArticle(opts)
	article2 := fixtures.GenerateArticle(opts)

	length1 := text.CountRunes(article1)
	length2 := text.CountRunes(article2)

	// Both should be approximately the same length (within ±10%)
	diff := length1 - length2
	if diff < 0 {
		diff = -diff
	}

	maxDiff := opts.Length / 5 // 20% difference allowed
	if diff > maxDiff {
		t.Errorf("Length difference too large: %d vs %d (diff: %d)", length1, length2, diff)
	}
}

// TestGenerateArticle_DifferentLengths tests various target lengths
func TestGenerateArticle_DifferentLengths(t *testing.T) {
	tests := []struct {
		name   string
		length int
	}{
		{"Very short", 200},
		{"Short", 500},
		{"Medium", 2000},
		{"Long", 5000},
		{"Very long", 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			article := fixtures.GenerateArticle(fixtures.ArticleOptions{
				Length:       tt.length,
				Language:     "japanese",
				IncludeEmoji: false,
			})

			actualLength := text.CountRunes(article)
			minLength := int(float64(tt.length) * 0.9)
			maxLength := int(float64(tt.length) * 1.1)

			if actualLength < minLength || actualLength > maxLength {
				t.Errorf("Length %d not within expected range [%d, %d]", actualLength, minLength, maxLength)
			}
		})
	}
}

// BenchmarkGenerateShortArticle benchmarks short article generation
func BenchmarkGenerateShortArticle(b *testing.B) {
	for i := 0; i < b.N; i++ {
		fixtures.GenerateShortArticle()
	}
}

// BenchmarkGenerateMediumArticle benchmarks medium article generation
func BenchmarkGenerateMediumArticle(b *testing.B) {
	for i := 0; i < b.N; i++ {
		fixtures.GenerateMediumArticle()
	}
}

// BenchmarkGenerateLongArticle benchmarks long article generation
func BenchmarkGenerateLongArticle(b *testing.B) {
	for i := 0; i < b.N; i++ {
		fixtures.GenerateLongArticle()
	}
}
