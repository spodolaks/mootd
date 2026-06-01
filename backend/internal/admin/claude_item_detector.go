package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ClaudeItemDetector is a stand-alone ItemDetector that talks directly
// to Anthropic's Messages API with vision and tool_use to force
// structured output. Used by the admin /archetype-defaults/detect
// endpoint when the upstream singleItemDetection orchestrator is in
// mock mode (its default until USE_REAL_STAGE1=true plus paid Replicate
// model paths are wired). The Claude path:
//
//   - costs ~$0.005 per detection vs ~$0.05+ for the orchestrator's
//     full pipeline (Replicate stage 1 + Anthropic stage 2 + Photoroom
//     stage 3),
//   - returns full structured attributes — color/fit/material/pattern/
//     silhouette — populating the curated default's traits map AND the
//     deeper structuredDescription the modal displays,
//   - skips image generation entirely (we keep the operator's photo as
//     the displayed item, which is exactly what the curator wants).
//
// Compile-time satisfies admin.ItemDetector.
var _ ItemDetector = (*ClaudeItemDetector)(nil)

// ClaudeItemDetector configuration values are kept simple — apiKey,
// model, base URL — so the constructor can be called from app/ with no
// more dependencies than the wardrobe-side detector.
type ClaudeItemDetector struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
	logger  *log.Logger
}

// NewClaudeItemDetector returns a vision-enabled Claude describer.
// model: e.g. "claude-sonnet-4-20250514". apiKey: ANTHROPIC_API_KEY.
// baseURL: empty defaults to https://api.anthropic.com.
func NewClaudeItemDetector(baseURL, apiKey, model string, logger *log.Logger) *ClaudeItemDetector {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}
	return &ClaudeItemDetector{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		// 60s ceiling: Claude's vision p99 with one image runs ~6-15s;
		// the timeout protects against a stuck model without making
		// the curator wait minutes.
		client: &http.Client{Timeout: 60 * time.Second},
		logger: logger,
	}
}

// claudeDescribeToolName is the tool name that the model is forced to
// invoke. Anthropic's tool_choice.name guarantees the response is a
// single tool_use block matching this schema.
const claudeDescribeToolName = "describe_clothing_item"

// describeClothingSchema mirrors the rough shape of the orchestrator's
// stage-2 structured description. The schema is intentionally
// expansive — we want the curator to see every attribute Claude can
// extract, not just the ones the wardrobe item table cares about.
// Optional fields are NOT in `required` so Claude can omit them when
// the photo doesn't show enough detail to be confident.
//
// Categories mirror the wardrobe-side enum used elsewhere in the admin
// UI; passing an enum forces Claude to map any item it sees into one
// of the slots the curator is already working with.
var describeClothingSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"category": map[string]any{
			"type": "string",
			"enum": []string{
				"tops", "bottoms", "outerwear", "footwear",
				"accessories", "dresses",
			},
			"description": "The wardrobe-item category that best fits this garment.",
		},
		"label": map[string]any{
			"type":        "string",
			"description": "Short human-readable label, max 80 chars. e.g. 'Black baseball cap', 'Light wash slim jeans'.",
			"maxLength":   80,
		},
		"color_primary": map[string]any{
			"type":        "string",
			"description": "Primary visible color, in plain English (e.g. 'black', 'navy', 'beige').",
		},
		"color_secondary": map[string]any{
			"type":        "string",
			"description": "Secondary color or trim, when present.",
		},
		"material": map[string]any{
			"type":        "string",
			"description": "Material or fabric (e.g. 'cotton twill', 'leather', 'denim', 'nylon').",
		},
		"pattern": map[string]any{
			"type":        "string",
			"description": "Pattern, when present (e.g. 'plain', 'striped', 'plaid', 'embroidered logo').",
		},
		"fit": map[string]any{
			"type":        "string",
			"description": "Fit / silhouette (e.g. 'slim', 'relaxed', 'oversized', 'cropped').",
		},
		"silhouette": map[string]any{
			"type":        "string",
			"description": "Garment-specific silhouette (e.g. 'crew neck', 'biker', 'baseball cap', 'high-waisted').",
		},
		"details": map[string]any{
			"type":        "string",
			"description": "Notable hardware / construction detail (e.g. 'metal zipper', 'embossed brand logo', 'fixed waistband').",
		},
		"season": map[string]any{
			"type":        "string",
			"description": "Implied season fit (e.g. 'summer', 'transitional', 'winter').",
		},
		"description": map[string]any{
			"type":        "string",
			"description": "One-sentence prose description suitable for a wardrobe card. Plain text, no markdown.",
			"maxLength":   200,
		},
		"confidence": map[string]any{
			"type":        "number",
			"description": "Self-rated confidence from 0 (guess) to 1 (highly certain).",
			"minimum":     0,
			"maximum":     1,
		},
	},
	"required": []string{"category", "label"},
}

// claudeDescribeRequest is the Messages API request body shape. We
// don't reuse the outfit-side type because admin shouldn't import
// outfit (one-way dep convention) and the request shape is small
// enough that duplicating it is cleaner than carving out a shared pkg.
type claudeDescribeRequest struct {
	Model      string                  `json:"model"`
	MaxTokens  int                     `json:"max_tokens"`
	Messages   []claudeDescribeMessage `json:"messages"`
	Tools      []claudeDescribeTool    `json:"tools"`
	ToolChoice claudeDescribeToolPick  `json:"tool_choice"`
}

type claudeDescribeMessage struct {
	Role    string                  `json:"role"`
	Content []claudeDescribeContent `json:"content"`
}

type claudeDescribeContent struct {
	Type   string                     `json:"type"`
	Text   string                     `json:"text,omitempty"`
	Source *claudeDescribeImageSource `json:"source,omitempty"`
}

type claudeDescribeImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type claudeDescribeTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type claudeDescribeToolPick struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

type claudeDescribeResponse struct {
	StopReason string                `json:"stop_reason"`
	Content    []claudeDescribeBlock `json:"content"`
}

type claudeDescribeBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// DetectFromBytes runs the photo through Claude vision and returns
// the structured description. Maps tool-use output into both the flat
// traits (string-only top-level fields) and the deeper
// structuredDescription (the full JSON, preserved verbatim).
func (d *ClaudeItemDetector) DetectFromBytes(ctx context.Context, imageData []byte, filename string) (DetectionPrefill, error) {
	if d.apiKey == "" {
		return DetectionPrefill{}, fmt.Errorf("claude detector: ANTHROPIC_API_KEY is empty")
	}
	if len(imageData) == 0 {
		return DetectionPrefill{}, fmt.Errorf("claude detector: empty image")
	}

	mediaType := http.DetectContentType(imageData)
	if !strings.HasPrefix(mediaType, "image/") {
		return DetectionPrefill{}, fmt.Errorf("claude detector: not an image (sniffed %q)", mediaType)
	}

	payload := claudeDescribeRequest{
		Model:     d.model,
		MaxTokens: 1024,
		Messages: []claudeDescribeMessage{{
			Role: "user",
			Content: []claudeDescribeContent{
				{
					Type: "image",
					Source: &claudeDescribeImageSource{
						Type:      "base64",
						MediaType: mediaType,
						Data:      base64.StdEncoding.EncodeToString(imageData),
					},
				},
				{
					Type: "text",
					Text: `You are describing a single garment for a fashion app's wardrobe.

Look at the photo and call the describe_clothing_item tool with every attribute you can extract confidently. Be specific (e.g. "black cotton twill baseball cap with embroidered logo" rather than "cap"). Use plain English — no marketing copy. Pick the most-fitting category from the schema's enum.

Items in this app are personal wardrobe pieces, not outfits — describe ONE garment, the most prominent in the frame.`,
				},
			},
		}},
		Tools: []claudeDescribeTool{{
			Name:        claudeDescribeToolName,
			Description: "Record the structured description of the clothing item visible in the photo.",
			InputSchema: describeClothingSchema,
		}},
		ToolChoice: claudeDescribeToolPick{
			Type: "tool",
			Name: claudeDescribeToolName,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return DetectionPrefill{}, fmt.Errorf("claude detector: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return DetectionPrefill{}, fmt.Errorf("claude detector: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", d.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	startedAt := time.Now()
	resp, err := d.client.Do(req)
	if err != nil {
		return DetectionPrefill{}, fmt.Errorf("claude detector: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return DetectionPrefill{}, fmt.Errorf("claude detector: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return DetectionPrefill{}, fmt.Errorf("claude detector: api returned %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed claudeDescribeResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return DetectionPrefill{}, fmt.Errorf("claude detector: decode: %w", err)
	}

	for _, block := range parsed.Content {
		if block.Type != "tool_use" || block.Name != claudeDescribeToolName {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(block.Input, &raw); err != nil {
			return DetectionPrefill{}, fmt.Errorf("claude detector: decode tool input: %w", err)
		}
		prefill := mapClaudeToolToPrefill(raw)
		d.logger.Printf("claude detector: filename=%q model=%s latency=%dms category=%s label=%q traits=%d",
			filename, d.model, time.Since(startedAt).Milliseconds(),
			prefill.Category, prefill.Label, len(prefill.Traits))
		return prefill, nil
	}

	return DetectionPrefill{}, fmt.Errorf("claude detector: no %s tool_use block in response (stop_reason=%s)",
		claudeDescribeToolName, parsed.StopReason)
}

// mapClaudeToolToPrefill flattens the rich tool-use output into the
// admin DetectionPrefill — string-valued fields land in Traits (so
// the wardrobe item table picks them up), the full nested map is
// stashed verbatim in StructuredDescription so the modal can show
// every attribute Claude returned.
func mapClaudeToolToPrefill(raw map[string]any) DetectionPrefill {
	pref := DetectionPrefill{
		StructuredDescription: raw,
		Traits:                map[string]string{},
	}
	if v, ok := raw["category"].(string); ok {
		pref.Category = v
	}
	if v, ok := raw["label"].(string); ok {
		pref.Label = v
	}
	if v, ok := raw["confidence"].(float64); ok {
		pref.Confidence = v
	}
	for k, v := range raw {
		// Don't double-record category/label/confidence as traits —
		// they have first-class slots already.
		switch k {
		case "category", "label", "confidence":
			continue
		}
		if s, ok := v.(string); ok && s != "" {
			pref.Traits[k] = s
		}
	}
	return pref
}
