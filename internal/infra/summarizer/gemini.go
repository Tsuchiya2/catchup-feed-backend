package summarizer

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const (
	// ProviderGemini is the identifier recorded for Gemini summaries.
	ProviderGemini = "gemini"

	// defaultGeminiModel is a free-tier model on Google AI Studio with a
	// generous daily quota; override with GEMINI_MODEL.
	defaultGeminiModel = "gemini-2.5-flash"

	defaultGeminiBaseURL = "https://generativelanguage.googleapis.com"
)

// GeminiConfig configures the Gemini provider.
type GeminiConfig struct {
	// APIKey is the Google AI Studio API key (GEMINI_API_KEY).
	APIKey string

	// Model is the generateContent model identifier (GEMINI_MODEL).
	Model string

	// BaseURL is the API origin. Defaults to the public endpoint;
	// overridable for tests.
	BaseURL string

	// Options are the shared summarizer settings (char limit, timeout).
	Options Options
}

// LoadGeminiConfig loads Gemini settings from environment variables.
//
// Environment variables:
//   - GEMINI_API_KEY: API key (empty means the provider is excluded from the chain)
//   - GEMINI_MODEL: model identifier (default: gemini-2.5-flash)
func LoadGeminiConfig(opts Options) GeminiConfig {
	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = defaultGeminiModel
	}
	return GeminiConfig{
		APIKey:  os.Getenv("GEMINI_API_KEY"),
		Model:   model,
		BaseURL: defaultGeminiBaseURL,
		Options: opts,
	}
}

// Gemini summarizes text via the Google AI Studio generateContent REST API.
// Plain net/http; no SDK.
type Gemini struct {
	config GeminiConfig
	client *http.Client
}

// NewGemini creates a Gemini provider. Empty Model/BaseURL fall back to defaults.
func NewGemini(config GeminiConfig) *Gemini {
	if config.Model == "" {
		config.Model = defaultGeminiModel
	}
	if config.BaseURL == "" {
		config.BaseURL = defaultGeminiBaseURL
	}
	config.Options = config.Options.withDefaults()
	return &Gemini{
		config: config,
		client: newHTTPClient(),
	}
}

// Name implements Provider.
func (g *Gemini) Name() string { return ProviderGemini }

// geminiRequest is the minimal generateContent request body.
type geminiRequest struct {
	Contents         []geminiContent         `json:"contents"`
	GenerationConfig *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiGenerationConfig struct {
	ThinkingConfig *geminiThinkingConfig `json:"thinkingConfig,omitempty"`
}

type geminiThinkingConfig struct {
	// ThinkingBudget 0 disables thinking. gemini-2.5-flash enables thinking
	// by default, which burns extra free-tier tokens with no benefit for
	// plain summarization (ゼロ円運用).
	ThinkingBudget int `json:"thinkingBudget"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

// geminiResponse is the minimal generateContent response body.
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// Summarize implements Provider using the generateContent endpoint.
func (g *Gemini) Summarize(ctx context.Context, text string) (string, error) {
	prompt := buildPrompt(g.config.Options.CharacterLimit, truncateInput(ProviderGemini, text))
	return g.Generate(ctx, prompt)
}

// Generate implements Provider: the prompt is sent verbatim.
func (g *Gemini) Generate(ctx context.Context, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.config.Options.Timeout)
	defer cancel()

	reqBody := geminiRequest{
		Contents: []geminiContent{{Parts: []geminiPart{{Text: prompt}}}},
		GenerationConfig: &geminiGenerationConfig{
			ThinkingConfig: &geminiThinkingConfig{ThinkingBudget: 0},
		},
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent",
		strings.TrimSuffix(g.config.BaseURL, "/"), g.config.Model)
	headers := map[string]string{"x-goog-api-key": g.config.APIKey}

	var resp geminiResponse
	if err := postJSON(ctx, g.client, ProviderGemini, url, headers, reqBody, &resp); err != nil {
		return "", err
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("%s: api returned no candidates", ProviderGemini)
	}

	var sb strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		sb.WriteString(part.Text)
	}
	out := strings.TrimSpace(sb.String())
	if out == "" {
		return "", fmt.Errorf("%s: api returned empty response", ProviderGemini)
	}
	return out, nil
}
