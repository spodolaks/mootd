package outfit

import (
	"context"
	"fmt"
)

// OllamaGenerator implements Generator against a local Ollama server (Qwen3 etc).
// It is text-only — vision input is silently ignored.
type OllamaGenerator struct {
	client *ollamaClient
}

// NewOllamaGenerator constructs an Ollama-backed Generator.
func NewOllamaGenerator(cfg OllamaConfig) *OllamaGenerator {
	return &OllamaGenerator{client: newOllamaClient(cfg)}
}

// Name returns the provider identifier for logs.
func (g *OllamaGenerator) Name() string { return "ollama" }

// Generate calls the local Ollama chat API with a JSON-mode system prompt and
// parses whatever shape the model returns into []Outfit.
func (g *OllamaGenerator) Generate(ctx context.Context, req GeneratorRequest) ([]Outfit, error) {
	sysPrompt := buildSystemPrompt(req.Weather, req.RecentBoards, req.TopArchetypes, req.Panels, req.Backgrounds)
	userMessage := BuildUserMessage(req.Items)

	llmContent, err := g.client.chat(ctx, sysPrompt, userMessage)
	if err != nil {
		return nil, fmt.Errorf("ollama chat: %w", err)
	}

	parsed, err := parseLLMResponse(llmContent)
	if err != nil {
		return nil, fmt.Errorf("parse ollama response: %w (raw: %s)", err, llmContent)
	}
	return parsed, nil
}

