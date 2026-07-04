package summarizer

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const (
	// ProviderGroq is the identifier recorded for Groq summaries.
	ProviderGroq = "groq"

	// defaultGroqModel is a free-tier model on Groq; override with GROQ_MODEL.
	defaultGroqModel = "llama-3.3-70b-versatile"

	defaultGroqBaseURL = "https://api.groq.com"
)

// GroqConfig configures the Groq provider (OpenAI-compatible REST API).
type GroqConfig struct {
	// APIKey is the Groq API key (GROQ_API_KEY).
	APIKey string

	// Model is the chat/completions model identifier (GROQ_MODEL).
	Model string

	// BaseURL is the API origin. Defaults to the public endpoint;
	// overridable for tests.
	BaseURL string

	// Options are the shared summarizer settings (char limit, timeout).
	Options Options
}

// LoadGroqConfig loads Groq settings from environment variables.
//
// Environment variables:
//   - GROQ_API_KEY: API key (empty means the provider is excluded from the chain)
//   - GROQ_MODEL: model identifier (default: llama-3.3-70b-versatile)
func LoadGroqConfig(opts Options) GroqConfig {
	model := os.Getenv("GROQ_MODEL")
	if model == "" {
		model = defaultGroqModel
	}
	return GroqConfig{
		APIKey:  os.Getenv("GROQ_API_KEY"),
		Model:   model,
		BaseURL: defaultGroqBaseURL,
		Options: opts,
	}
}

// Groq summarizes text via Groq's OpenAI-compatible chat/completions API.
// Plain net/http; no SDK.
type Groq struct {
	config GroqConfig
	client *http.Client
}

// NewGroq creates a Groq provider. Empty Model/BaseURL fall back to defaults.
func NewGroq(config GroqConfig) *Groq {
	if config.Model == "" {
		config.Model = defaultGroqModel
	}
	if config.BaseURL == "" {
		config.BaseURL = defaultGroqBaseURL
	}
	config.Options = config.Options.withDefaults()
	return &Groq{
		config: config,
		client: newHTTPClient(),
	}
}

// Name implements Provider.
func (g *Groq) Name() string { return ProviderGroq }

// groqRequest is the minimal chat/completions request body.
type groqRequest struct {
	Model    string        `json:"model"`
	Messages []groqMessage `json:"messages"`
}

type groqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// groqResponse is the minimal chat/completions response body.
type groqResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Summarize implements Provider using the chat/completions endpoint.
func (g *Groq) Summarize(ctx context.Context, text string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.config.Options.Timeout)
	defer cancel()

	prompt := buildPrompt(g.config.Options.CharacterLimit, truncateInput(ProviderGroq, text))
	reqBody := groqRequest{
		Model:    g.config.Model,
		Messages: []groqMessage{{Role: "user", Content: prompt}},
	}

	url := strings.TrimSuffix(g.config.BaseURL, "/") + "/openai/v1/chat/completions"
	headers := map[string]string{"Authorization": "Bearer " + g.config.APIKey}

	var resp groqResponse
	if err := postJSON(ctx, g.client, ProviderGroq, url, headers, reqBody, &resp); err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("%s: api returned no choices", ProviderGroq)
	}
	summary := strings.TrimSpace(resp.Choices[0].Message.Content)
	if summary == "" {
		return "", fmt.Errorf("%s: api returned empty summary", ProviderGroq)
	}
	return summary, nil
}
