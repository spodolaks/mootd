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

func (g *OpenAIGenerator) Generate(ctx context.Context, req GeneratorRequest) ([]Outfit, *Usage, error) {
	if g.cfg.APIKey == "" {
		return nil, nil, errors.New("openai generator: OPENAI_API_KEY is not set")
	}

	systemPrompt := buildSystemPromptWithOverrides(req.PromptOverrides, req.UserID, req.Weather, req.RecentBoards, req.TopArchetypes, req.Panels, req.Backgrounds)
	userMessage := buildUserMessageWithOverrides(req.PromptOverrides, req.UserID, req.Items)

	// mootd#67 — translate user creativity preference to
	// temperature when supplied; otherwise keep the historical
	// 0.9 default (so a request without creativity still hits
	// the same LLM behaviour as before).
	temperature := 0.9
	if t := CreativityToTemperature(req.Creativity); t > 0 {
		temperature = t
	}
	payload := openaiChatRequest{
		Model: g.cfg.Model,
		Messages: []openaiMessage{
			{Role: "system", Content: systemPrompt + "\n\nRespond with valid JSON only."},
			{Role: "user", Content: userMessage},
		},
		ResponseFormat: &openaiResponseFormat{Type: "json_object"},
		Temperature:    temperature,
		MaxTokens:      2048,
	}

	// Pre-populate a zero Usage so transport-level failures still get
	// a row in the observability ledger (provider + model are known
	// before we call out).
	usage := &Usage{
		Provider:      "openai",
		Model:         g.cfg.Model,
		PromptVersion: PromptVersion,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, usage, fmt.Errorf("marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, usage, fmt.Errorf("build openai request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, usage, fmt.Errorf("openai request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, usage, fmt.Errorf("read openai response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, usage, fmt.Errorf("openai returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result openaiChatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, usage, fmt.Errorf("decode openai response: %w", err)
	}

	// Stamp tokens before we attempt to parse the model output — even
	// a malformed response is billable. OpenAI's prompt_tokens INCLUDES
	// cached tokens; ComputeCost expects InputTokens to be the
	// uncached portion only, so subtract here. (Anthropic returns
	// input_tokens already-uncached, so the Claude generator's
	// extractClaudeUsage doesn't need this dance.)
	if result.Usage != nil {
		usage.OutputTokens = result.Usage.CompletionTokens
		cached := 0
		if result.Usage.PromptTokensDetails != nil {
			cached = result.Usage.PromptTokensDetails.CachedTokens
		}
		usage.CacheReadTokens = cached
		usage.InputTokens = result.Usage.PromptTokens - cached
		if usage.InputTokens < 0 {
			usage.InputTokens = 0
		}
	}

	if len(result.Choices) == 0 {
		return nil, usage, fmt.Errorf("openai returned no choices")
	}

	content := result.Choices[0].Message.Content
	g.logger.Printf("outfit: openai raw response length: %d bytes", len(content))
	usage.RawResponse = content

	parsed, err := parseLLMResponse(content)
	if err != nil {
		return nil, usage, fmt.Errorf("parse openai response: %w (raw: %s)", err, content)
	}
	return parsed, usage, nil
}

// ── OpenAI Chat Completions wire types ─────────────────────────────────────

type openaiChatRequest struct {
	Model          string                `json:"model"`
	Messages       []openaiMessage       `json:"messages"`
	ResponseFormat *openaiResponseFormat `json:"response_format,omitempty"`
	Temperature    float64               `json:"temperature,omitempty"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
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
	// Usage is the per-call billing breakdown. OpenAI returns this on
	// every successful response; absent on streaming or partial errors.
	// We stamp 0s in those cases — better to record a zero-token row
	// than drop the call from the ledger.
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
		// Prompt caching arrived in late 2025 — when present, splits
		// prompt_tokens into cached vs non-cached. Optional; absent
		// for older models / non-cached calls.
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
	} `json:"usage,omitempty"`
}
