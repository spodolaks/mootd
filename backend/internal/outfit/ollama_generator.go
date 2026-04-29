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
//
// Ollama runs locally and is free, so token counts always land at zero. The
// observability ledger still gets a row per call (with cost_usd=0) so the
// admin panel can see "this user is using local generation, not the paid
// providers" without inferring it from the absence of rows.
func (g *OllamaGenerator) Generate(ctx context.Context, req GeneratorRequest) ([]Outfit, *Usage, error) {
	usage := &Usage{
		Provider:      "ollama",
		Model:         g.client.cfg.Model,
		PromptVersion: PromptVersion,
	}

	sysPrompt := buildSystemPrompt(req.Weather, req.RecentBoards, req.TopArchetypes, req.Panels, req.Backgrounds)
	userMessage := BuildUserMessage(req.Items)

	llmContent, err := g.client.chat(ctx, sysPrompt, userMessage)
	if err != nil {
		return nil, usage, fmt.Errorf("ollama chat: %w", err)
	}
	usage.RawResponse = llmContent

	parsed, err := parseLLMResponse(llmContent)
	if err != nil {
		return nil, usage, fmt.Errorf("parse ollama response: %w (raw: %s)", err, llmContent)
	}
	return parsed, usage, nil
}

