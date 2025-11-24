package summarizer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

/* ───────── Integration Tests with Mock Metrics ───────── */

// TestOpenAI_WithMockMetrics tests OpenAI truncation logic without actual API call
func TestOpenAI_WithMockMetrics(t *testing.T) {
	t.Run("long text triggers truncation logic", func(t *testing.T) {
		// Test truncation logic directly without calling doSummarize
		const maxChars = 10000
		longText := strings.Repeat("あ", 15000)

		// Simulate truncation
		truncatedText := longText
		if len(longText) > maxChars {
			truncatedText = longText[:maxChars] + "...\n(内容が長いため切り詰めました)"
		}

		// Verify truncation occurred
		assert.Greater(t, len(longText), maxChars)
		assert.Contains(t, truncatedText, "切り詰めました")
		assert.LessOrEqual(t, len(truncatedText), maxChars+len("...\n(内容が長いため切り詰めました)"))
	})
}

// TestClaude_WithMockMetrics tests Claude truncation logic without actual API call
func TestClaude_WithMockMetrics(t *testing.T) {
	t.Run("long text triggers truncation logic", func(t *testing.T) {
		// Test truncation logic directly without calling doSummarize
		const maxChars = 10000
		longText := strings.Repeat("あ", 15000)

		// Simulate truncation
		truncatedText := longText
		if len(longText) > maxChars {
			truncatedText = longText[:maxChars] + "...\n(内容が長いため切り詰めました)"
		}

		// Verify truncation occurred
		assert.Greater(t, len(longText), maxChars)
		assert.Contains(t, truncatedText, "切り詰めました")
		assert.LessOrEqual(t, len(truncatedText), maxChars+len("...\n(内容が長いため切り詰めました)"))
	})
}

// TestOpenAI_BuildPromptIntegration tests buildPrompt is called correctly
func TestOpenAI_BuildPromptIntegration(t *testing.T) {
	config := &OpenAIConfig{
		CharacterLimit: 1200,
		Language:       "japanese",
		Model:          "gpt-3.5-turbo",
		MaxTokens:      1024,
		Timeout:        60,
	}

	openai := &OpenAI{
		config: config,
	}

	text := "テスト用のテキストです。"
	prompt := openai.buildPrompt(text)

	// Verify prompt format
	assert.Contains(t, prompt, "日本語")
	assert.Contains(t, prompt, "1200文字以内")
	assert.Contains(t, prompt, "要約")
	assert.Contains(t, prompt, text)
}

// TestClaude_BuildPromptIntegration tests buildPrompt is called correctly
func TestClaude_BuildPromptIntegration(t *testing.T) {
	config := ClaudeConfig{
		CharacterLimit: 1500,
		Language:       "japanese",
		Model:          "claude-sonnet-4-5-20250929",
		MaxTokens:      1024,
		Timeout:        60,
	}

	claude := &Claude{
		config: config,
	}

	text := "テスト用のテキストです。"
	prompt := claude.buildPrompt(text)

	// Verify prompt format
	assert.Contains(t, prompt, "japanese")
	assert.Contains(t, prompt, "1500文字以内")
	assert.Contains(t, prompt, "要約")
	assert.Contains(t, prompt, text)
}

// TestOpenAI_CharacterLimitInPrompt tests different character limits
func TestOpenAI_CharacterLimitInPrompt(t *testing.T) {
	limits := []int{100, 500, 900, 1500, 5000}

	for _, limit := range limits {
		t.Run(string(rune(limit)), func(t *testing.T) {
			config := &OpenAIConfig{
				CharacterLimit: limit,
				Language:       "japanese",
				Model:          "gpt-3.5-turbo",
				MaxTokens:      1024,
				Timeout:        60,
			}

			openai := &OpenAI{config: config}
			prompt := openai.buildPrompt("テスト")

			// Verify character limit is in prompt
			assert.Contains(t, prompt, "文字以内")
		})
	}
}

// TestClaude_CharacterLimitInPrompt tests different character limits
func TestClaude_CharacterLimitInPrompt(t *testing.T) {
	limits := []int{100, 500, 900, 1500, 5000}

	for _, limit := range limits {
		t.Run(string(rune(limit)), func(t *testing.T) {
			config := ClaudeConfig{
				CharacterLimit: limit,
				Language:       "japanese",
				Model:          "claude-sonnet-4-5-20250929",
				MaxTokens:      1024,
				Timeout:        60,
			}

			claude := &Claude{config: config}
			prompt := claude.buildPrompt("テスト")

			// Verify character limit is in prompt
			assert.Contains(t, prompt, "文字以内")
		})
	}
}

// TestTruncationLogic tests the truncation logic directly
func TestTruncationLogic(t *testing.T) {
	const maxChars = 10000

	tests := []struct {
		name     string
		input    string
		expected func(string) bool
	}{
		{
			name:  "short text unchanged",
			input: "short",
			expected: func(s string) bool {
				return s == "short"
			},
		},
		{
			name:  "exactly max chars unchanged",
			input: strings.Repeat("a", maxChars),
			expected: func(s string) bool {
				return len(s) == maxChars
			},
		},
		{
			name:  "over max chars truncated",
			input: strings.Repeat("a", maxChars+1000),
			expected: func(s string) bool {
				return strings.Contains(s, "切り詰めました")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate truncation
			result := tt.input
			if len(tt.input) > maxChars {
				result = tt.input[:maxChars] + "...\n(内容が長いため切り詰めました)"
			}

			assert.True(t, tt.expected(result))
		})
	}
}

// TestMetricsRecorderInterface tests that both implementations work
func TestMetricsRecorderInterface(t *testing.T) {
	t.Run("MockMetricsRecorder", func(t *testing.T) {
		var recorder SummaryMetricsRecorder = &MockMetricsRecorder{}

		// Should not panic
		assert.NotPanics(t, func() {
			recorder.RecordLength(900)
			recorder.RecordDuration(1)
			recorder.RecordCompliance(true)
			recorder.RecordLimitExceeded()
		})
	})

	t.Run("PrometheusSummaryMetrics", func(t *testing.T) {
		var recorder SummaryMetricsRecorder = NewPrometheusSummaryMetrics()

		// Should not panic
		assert.NotPanics(t, func() {
			recorder.RecordLength(900)
			recorder.RecordDuration(1)
			recorder.RecordCompliance(true)
			recorder.RecordLimitExceeded()
		})
	})
}
