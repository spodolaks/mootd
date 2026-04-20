package outfit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// OpenAIConfig holds the OpenAI API configuration for outfit generation.
type OpenAIConfig struct {
	BaseURL string
	APIKey  string
	Model   string // defaults to "gpt-4o"
}

// OpenAIGenerator implements Generator using OpenAI's chat completions API
// with JSON mode. No vision — text-only wardrobe inventory.
type OpenAIGenerator struct {
	cfg    OpenAIConfig
	client *http.Client
	logger *log.Logger
}

// NewOpenAIGenerator constructs an OpenAI-backed Generator.
func NewOpenAIGenerator(cfg OpenAIConfig, logger *log.Logger) *OpenAIGenerator {
	if cfg.Model == "" {
		cfg.Model = "gpt-4o"
	}
	return &OpenAIGenerator{
		cfg:    cfg,
		client: &http.Client{Timeout: 90 * time.Second},
		logger: logger,
	}
}

func (g *OpenAIGenerator) Name() string { return "openai" }

func (g *OpenAIGenerator) Generate(ctx context.Context, req GeneratorRequest) ([]Outfit, error) {
	if g.cfg.APIKey == "" {
		return nil, errors.New("openai generator: OPENAI_API_KEY is not set")
	}

	systemPrompt := buildSystemPrompt(req.Weather, req.RecentBoards, req.TopArchetypes, req.Panels, req.Backgrounds)
	userMessage := BuildUserMessage(req.Items)

	payload := openaiChatRequest{
		Model: g.cfg.Model,
		Messages: []openaiMessage{
			{Role: "system", Content: systemPrompt + "\n\nRespond with valid JSON only."},
			{Role: "user", Content: userMessage},
		},
		ResponseFormat: &openaiResponseFormat{Type: "json_object"},
		Temperature:    0.9,
		MaxTokens:      2048,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build openai request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result openaiChatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("openai returned no choices")
	}

	content := result.Choices[0].Message.Content
	g.logger.Printf("outfit: openai raw response length: %d bytes", len(content))

	parsed, err := parseLLMResponse(content)
	if err != nil {
		return nil, fmt.Errorf("parse openai response: %w (raw: %s)", err, content)
	}
	return parsed, nil
}

// ── OpenAI Chat Completions wire types ─────────────────────────────────────

type openaiChatRequest struct {
	Model          string               `json:"model"`
	Messages       []openaiMessage      `json:"messages"`
	ResponseFormat *openaiResponseFormat `json:"response_format,omitempty"`
	Temperature    float64              `json:"temperature,omitempty"`
	MaxTokens      int                  `json:"max_tokens,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponseFormat struct {
	Type string `json:"type"` // "json_object"
}

type openaiChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}
