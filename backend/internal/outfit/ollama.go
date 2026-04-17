package outfit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaConfig holds the Ollama service configuration.
// Override via OLLAMA_BASE_URL and OLLAMA_MODEL environment variables.
type OllamaConfig struct {
	BaseURL string
	Model   string
}

// ollamaClient sends chat completion requests to the local Ollama service.
type ollamaClient struct {
	cfg    OllamaConfig
	client *http.Client
}

func newOllamaClient(cfg OllamaConfig) *ollamaClient {
	return &ollamaClient{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

// chat sends a system + user message pair and returns the assistant's response content.
// format:"json" instructs Ollama to constrain output to valid JSON.
func (c *ollamaClient) chat(ctx context.Context, systemPrompt, userMessage string) (string, error) {
	req := ollamaRequest{
		Model: c.cfg.Model,
		Messages: []ollamaMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
		Stream: false,
		Format: "json",
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result ollamaResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return result.Message.Content, nil
}
