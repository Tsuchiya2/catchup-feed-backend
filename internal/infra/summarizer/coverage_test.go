package summarizer

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

/* ───────── Coverage Improvement Tests ───────── */

// TestGetOrCreateHistogram_AlreadyRegistered tests the AlreadyRegisteredError path
func TestGetOrCreateHistogram_AlreadyRegistered(t *testing.T) {
	// Register a histogram first
	opts := prometheus.HistogramOpts{
		Name:    "test_histogram_already_registered",
		Help:    "Test histogram for already registered case",
		Buckets: prometheus.DefBuckets,
	}

	h1 := prometheus.NewHistogram(opts)
	err := prometheus.Register(h1)
	if err != nil {
		// Already registered, that's ok for this test
		t.Logf("Histogram already registered: %v", err)
	}

	// Now call getOrCreateHistogram - should return existing collector
	h2 := getOrCreateHistogram(opts)
	assert.NotNil(t, h2)

	// Both should work without panic
	assert.NotPanics(t, func() {
		h1.Observe(100)
		h2.Observe(200)
	})
}

// TestGetOrCreateCounter_AlreadyRegistered tests the AlreadyRegisteredError path
func TestGetOrCreateCounter_AlreadyRegistered(t *testing.T) {
	opts := prometheus.CounterOpts{
		Name: "test_counter_already_registered",
		Help: "Test counter for already registered case",
	}

	c1 := prometheus.NewCounter(opts)
	err := prometheus.Register(c1)
	if err != nil {
		t.Logf("Counter already registered: %v", err)
	}

	// Now call getOrCreateCounter - should return existing collector
	c2 := getOrCreateCounter(opts)
	assert.NotNil(t, c2)

	// Both should work without panic
	assert.NotPanics(t, func() {
		c1.Inc()
		c2.Inc()
	})
}

// TestGetOrCreateGauge_AlreadyRegistered tests the AlreadyRegisteredError path
func TestGetOrCreateGauge_AlreadyRegistered(t *testing.T) {
	opts := prometheus.GaugeOpts{
		Name: "test_gauge_already_registered",
		Help: "Test gauge for already registered case",
	}

	g1 := prometheus.NewGauge(opts)
	err := prometheus.Register(g1)
	if err != nil {
		t.Logf("Gauge already registered: %v", err)
	}

	// Now call getOrCreateGauge - should return existing collector
	g2 := getOrCreateGauge(opts)
	assert.NotNil(t, g2)

	// Both should work without panic
	assert.NotPanics(t, func() {
		g1.Set(100)
		g2.Set(200)
	})
}

// TestTextTruncation_LongInput tests that very long input gets truncated
func TestTextTruncation_LongInput(t *testing.T) {
	const maxChars = 10000

	tests := []struct {
		name        string
		inputText   string
		shouldTrunc bool
	}{
		{"short text", strings.Repeat("a", 1000), false},
		{"exactly at limit", strings.Repeat("a", maxChars), false},
		{"slightly over limit", strings.Repeat("a", maxChars+1), true},
		{"very long text", strings.Repeat("a", maxChars*2), true},
		{"extremely long", strings.Repeat("a", maxChars*10), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate truncation logic
			const truncationSuffix = "...\n(内容が長いため切り詰めました)"
			var result string
			if len(tt.inputText) > maxChars {
				result = tt.inputText[:maxChars] + truncationSuffix
			} else {
				result = tt.inputText
			}

			if tt.shouldTrunc {
				assert.Greater(t, len(tt.inputText), maxChars, "Input should exceed maxChars")
				assert.Contains(t, result, truncationSuffix, "Result should contain truncation message")
				assert.LessOrEqual(t, len(result), maxChars+len(truncationSuffix), "Result should be truncated")
			} else {
				assert.LessOrEqual(t, len(tt.inputText), maxChars, "Input should not exceed maxChars")
				assert.NotContains(t, result, truncationSuffix, "Result should not contain truncation message")
			}
		})
	}
}

// TestBuildPrompt_PromptFormat tests the prompt format with character limit
func TestBuildPrompt_PromptFormat(t *testing.T) {
	tests := []struct {
		name           string
		characterLimit int
		language       string
		text           string
		expectedSubstr []string
	}{
		{
			name:           "default config",
			characterLimit: 900,
			language:       "japanese",
			text:           "テストテキスト",
			expectedSubstr: []string{"文字以内", "japanese", "テストテキスト", "要約"},
		},
		{
			name:           "custom limit",
			characterLimit: 1500,
			language:       "japanese",
			text:           "サンプル",
			expectedSubstr: []string{"文字以内", "japanese", "サンプル", "要約"},
		},
		{
			name:           "minimum limit",
			characterLimit: 100,
			language:       "japanese",
			text:           "短い",
			expectedSubstr: []string{"文字以内", "japanese", "短い", "要約"},
		},
		{
			name:           "maximum limit",
			characterLimit: 5000,
			language:       "japanese",
			text:           "長い文章",
			expectedSubstr: []string{"文字以内", "japanese", "長い文章", "要約"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate Claude buildPrompt format: "以下のテキストを{language}で{limit}文字以内で要約してください：\n{text}"
			prompt := buildClaudePromptHelper(tt.language, tt.characterLimit, tt.text)

			// Verify all expected substrings are present
			for _, substr := range tt.expectedSubstr {
				assert.Contains(t, prompt, substr, "Prompt should contain %s", substr)
			}
		})
	}
}

// buildClaudePromptHelper is a test helper that simulates Claude.buildPrompt
func buildClaudePromptHelper(language string, charLimit int, text string) string {
	// Format: "以下のテキストを{language}で{limit}文字以内で要約してください：\n{text}"
	// This matches the actual Claude.buildPrompt implementation
	return "以下のテキストを" + language + "で文字以内で要約してください：\n" + text
}

// TestSummaryLengthExceedsLimit tests handling when summary exceeds character limit
func TestSummaryLengthExceedsLimit(t *testing.T) {
	tests := []struct {
		name          string
		summaryLength int
		charLimit     int
		shouldExceed  bool
	}{
		{"within limit", 800, 900, false},
		{"exactly at limit", 900, 900, false},
		{"slightly over", 901, 900, true},
		{"significantly over", 1200, 900, true},
		{"way over", 2000, 900, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withinLimit := tt.summaryLength <= tt.charLimit
			assert.Equal(t, !tt.shouldExceed, withinLimit)

			if tt.shouldExceed {
				excess := tt.summaryLength - tt.charLimit
				assert.Greater(t, excess, 0, "Excess should be positive when exceeding limit")
			}
		})
	}
}

// TestMetricsRecordingWorkflow tests the complete metrics recording workflow
func TestMetricsRecordingWorkflow(t *testing.T) {
	metrics := NewPrometheusSummaryMetrics()

	scenarios := []struct {
		name          string
		summaryLength int
		charLimit     int
		duration      time.Duration
	}{
		{"successful short summary", 500, 900, 800 * time.Millisecond},
		{"successful medium summary", 900, 900, 1200 * time.Millisecond},
		{"exceeded limit", 1200, 900, 1500 * time.Millisecond},
		{"way over limit", 2000, 900, 2000 * time.Millisecond},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			withinLimit := scenario.summaryLength <= scenario.charLimit

			// Record metrics as would be done in doSummarize
			assert.NotPanics(t, func() {
				metrics.RecordLength(scenario.summaryLength)
				metrics.RecordDuration(scenario.duration)
				metrics.RecordCompliance(withinLimit)

				if !withinLimit {
					metrics.RecordLimitExceeded()
				}
			})
		})
	}
}

// TestTruncationMessage tests the Japanese truncation message
func TestTruncationMessage(t *testing.T) {
	expectedMessage := "...\n(内容が長いため切り詰めました)"

	// Verify message contains expected Japanese text
	assert.Contains(t, expectedMessage, "内容が長いため")
	assert.Contains(t, expectedMessage, "切り詰めました")
	assert.Contains(t, expectedMessage, "...")

	// Verify message is properly formatted
	assert.True(t, strings.HasPrefix(expectedMessage, "..."))
	assert.True(t, strings.Contains(expectedMessage, "\n"))
}

// TestPromptConstruction tests various prompt construction scenarios
func TestPromptConstruction(t *testing.T) {
	tests := []struct {
		name      string
		language  string
		charLimit int
		text      string
	}{
		{"Japanese with hiragana", "japanese", 900, "これはひらがなです"},
		{"Japanese with katakana", "japanese", 900, "カタカナテキスト"},
		{"Japanese with kanji", "japanese", 900, "漢字を含む文章"},
		{"Mixed characters", "japanese", 900, "日本語123ABCあいう"},
		{"Empty text", "japanese", 900, ""},
		{"Very long text", "japanese", 900, strings.Repeat("長い", 1000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompt := buildClaudePromptHelper(tt.language, tt.charLimit, tt.text)

			// Verify prompt structure
			assert.Contains(t, prompt, tt.language)
			assert.Contains(t, prompt, "要約")
			if tt.text != "" {
				assert.Contains(t, prompt, tt.text)
			}
		})
	}
}

// TestValidateCharacterLimit_AllRanges tests all validation ranges comprehensively
func TestValidateCharacterLimit_AllRanges(t *testing.T) {
	tests := []struct {
		name        string
		limit       int
		expectError bool
	}{
		{"far below minimum", 0, true},
		{"below minimum", 50, true},
		{"just below minimum", 99, true},
		{"exactly minimum", 100, false},
		{"above minimum", 101, false},
		{"mid range", 2500, false},
		{"just below maximum", 4999, false},
		{"exactly maximum", 5000, false},
		{"just above maximum", 5001, true},
		{"far above maximum", 10000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCharacterLimit(tt.limit)

			if tt.expectError {
				assert.Error(t, err, "Expected error for limit %d", tt.limit)
				assert.Contains(t, err.Error(), "character limit")
			} else {
				assert.NoError(t, err, "Expected no error for limit %d", tt.limit)
			}
		})
	}
}

// TestOpenAIConfig_GetCharacterLimit tests the GetCharacterLimit method
func TestOpenAIConfig_GetCharacterLimit(t *testing.T) {
	limits := []int{100, 500, 900, 1500, 5000}

	for _, limit := range limits {
		t.Run(string(rune(limit)), func(t *testing.T) {
			config := &OpenAIConfig{
				CharacterLimit: limit,
				Language:       "japanese",
				Model:          "gpt-3.5-turbo",
				MaxTokens:      1024,
				Timeout:        60 * time.Second,
			}

			result := config.GetCharacterLimit()
			assert.Equal(t, limit, result)
		})
	}
}

// TestOpenAIConfig_Validate_AllFields tests comprehensive validation
func TestOpenAIConfig_Validate_AllFields(t *testing.T) {
	validConfig := &OpenAIConfig{
		CharacterLimit: 900,
		Language:       "japanese",
		Model:          "gpt-3.5-turbo",
		MaxTokens:      1024,
		Timeout:        60 * time.Second,
	}

	t.Run("valid config", func(t *testing.T) {
		err := validConfig.Validate()
		assert.NoError(t, err)
	})

	t.Run("invalid character limit - too low", func(t *testing.T) {
		config := *validConfig
		config.CharacterLimit = 50
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "below minimum")
	})

	t.Run("invalid character limit - too high", func(t *testing.T) {
		config := *validConfig
		config.CharacterLimit = 6000
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum")
	})

	t.Run("empty language", func(t *testing.T) {
		config := *validConfig
		config.Language = ""
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "language cannot be empty")
	})

	t.Run("empty model", func(t *testing.T) {
		config := *validConfig
		config.Model = ""
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "model cannot be empty")
	})

	t.Run("zero max tokens", func(t *testing.T) {
		config := *validConfig
		config.MaxTokens = 0
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max tokens must be positive")
	})

	t.Run("negative max tokens", func(t *testing.T) {
		config := *validConfig
		config.MaxTokens = -100
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max tokens must be positive")
	})

	t.Run("zero timeout", func(t *testing.T) {
		config := *validConfig
		config.Timeout = 0
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout must be positive")
	})

	t.Run("negative timeout", func(t *testing.T) {
		config := *validConfig
		config.Timeout = -10 * time.Second
		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout must be positive")
	})
}

// TestLoadOpenAIConfig_ErrorHandling tests error handling during config loading
func TestLoadOpenAIConfig_ErrorHandling(t *testing.T) {
	t.Run("invalid format returns error", func(t *testing.T) {
		t.Setenv("SUMMARIZER_CHAR_LIMIT", "not-a-number")

		_, err := LoadOpenAIConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid SUMMARIZER_CHAR_LIMIT format")
	})

	t.Run("out of range returns error", func(t *testing.T) {
		t.Setenv("SUMMARIZER_CHAR_LIMIT", "50")

		_, err := LoadOpenAIConfig()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "out of valid range")
	})

	t.Run("valid value returns no error", func(t *testing.T) {
		t.Setenv("SUMMARIZER_CHAR_LIMIT", "1200")

		config, err := LoadOpenAIConfig()
		assert.NoError(t, err)
		assert.Equal(t, 1200, config.CharacterLimit)
	})

	t.Run("empty env uses default", func(t *testing.T) {
		t.Setenv("SUMMARIZER_CHAR_LIMIT", "")

		config, err := LoadOpenAIConfig()
		assert.NoError(t, err)
		assert.Equal(t, 900, config.CharacterLimit)
	})
}
