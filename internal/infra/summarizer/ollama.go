package summarizer

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

const (
	// ProviderOllama is the identifier recorded for Ollama summaries.
	ProviderOllama = "ollama"

	// defaultOllamaModel is a small local model with decent Japanese output;
	// override with OLLAMA_MODEL once the actual model is pulled on the Mac.
	defaultOllamaModel = "qwen2.5:7b"

	defaultOllamaHost = "http://localhost:11434"
)

// OllamaConfig configures the local Ollama provider.
type OllamaConfig struct {
	// Host is the Ollama origin, e.g. "http://mac.tailnet:11434" (OLLAMA_HOST).
	Host string

	// Model is the local model identifier (OLLAMA_MODEL).
	Model string

	// Options are the shared summarizer settings (char limit, timeout).
	Options Options
}

// LoadOllamaConfig loads Ollama settings from environment variables.
//
// Environment variables:
//   - OLLAMA_HOST: Ollama origin (default: http://localhost:11434)
//   - OLLAMA_MODEL: model identifier (default: qwen2.5:7b)
func LoadOllamaConfig(opts Options) OllamaConfig {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = defaultOllamaHost
	}
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = defaultOllamaModel
	}
	return OllamaConfig{
		Host:    host,
		Model:   model,
		Options: opts,
	}
}

// Ollama summarizes text via the local Ollama HTTP API (/api/generate).
// It is the last resort of the fallback chain: private-safe and free,
// at the cost of quality (§8 縮退許容).
type Ollama struct {
	config OllamaConfig
	client *http.Client
}

// NewOllama creates an Ollama provider. Empty Host/Model fall back to defaults.
func NewOllama(config OllamaConfig) *Ollama {
	if config.Host == "" {
		config.Host = defaultOllamaHost
	}
	if config.Model == "" {
		config.Model = defaultOllamaModel
	}
	config.Options = config.Options.withDefaults()
	return &Ollama{
		config: config,
		client: newHTTPClient(),
	}
}

// Name implements Provider.
func (o *Ollama) Name() string { return ProviderOllama }

// ollamaRequest is the minimal /api/generate request body.
type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// ollamaResponse is the minimal /api/generate response body.
type ollamaResponse struct {
	Response string `json:"response"`
}

// Summarize implements Provider using the /api/generate endpoint.
func (o *Ollama) Summarize(ctx context.Context, text string) (string, error) {
	prompt := buildPrompt(o.config.Options.CharacterLimit, truncateInput(ProviderOllama, text))
	return o.Generate(ctx, prompt)
}

// Generate implements Provider: the prompt is sent verbatim.
func (o *Ollama) Generate(ctx context.Context, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, o.config.Options.Timeout)
	defer cancel()

	reqBody := ollamaRequest{
		Model:  o.config.Model,
		Prompt: prompt,
		Stream: false,
	}

	url := strings.TrimSuffix(o.config.Host, "/") + "/api/generate"

	var resp ollamaResponse
	if err := postJSON(ctx, o.client, ProviderOllama, url, nil, reqBody, &resp); err != nil {
		return "", err
	}

	out := strings.TrimSpace(resp.Response)
	if out == "" {
		return "", fmt.Errorf("%s: api returned empty response", ProviderOllama)
	}
	return out, nil
}
