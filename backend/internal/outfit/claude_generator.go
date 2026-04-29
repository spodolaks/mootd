package outfit

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// visionImageParallelism caps concurrent GridFS reads during vision-mode
// image loading. Six is a balance: high enough to hide MongoDB round-
// trip latency (typical 5–10ms per read), low enough that simultaneous
// outfit generations don't saturate the default 100-connection pool.
const visionImageParallelism = 6

// ClaudeConfig holds the Anthropic API configuration.
type ClaudeConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	// Vision turns on image input. When true, the generator loads each item's
	// PNG bytes from the wardrobe repository and sends them alongside the text
	// prompt so Claude can reason about color, texture, and silhouette.
	Vision bool
	// MaxVisionItems caps the number of images sent in a single request to
	// keep latency and token cost bounded. Larger wardrobes are truncated to
	// the highest-ranked items by the caller (or natural ordering if no rank).
	MaxVisionItems int
}

// ClaudeGenerator implements Generator against the Anthropic Messages API.
// It uses tool use to force structured output and (optionally) sends item
// images for visual reasoning.
type ClaudeGenerator struct {
	cfg     ClaudeConfig
	client  *http.Client
	logger  *log.Logger
	imgRepo imageRepository
}

// imageRepository is the subset of the wardrobe repo needed to load PNG bytes
// for an item. The Claude generator pulls bytes lazily (and only when vision
// is enabled), so the dependency stays narrow.
type imageRepository interface {
	GetImage(ctx context.Context, itemID string) ([]byte, string, error)
}

// NewClaudeGenerator constructs a Claude-backed Generator.
// imgRepo may be nil when Vision is disabled.
func NewClaudeGenerator(cfg ClaudeConfig, logger *log.Logger, imgRepo imageRepository) *ClaudeGenerator {
	if cfg.MaxVisionItems == 0 {
		cfg.MaxVisionItems = 24
	}
	return &ClaudeGenerator{
		cfg:     cfg,
		client:  &http.Client{Timeout: 90 * time.Second},
		logger:  logger,
		imgRepo: imgRepo,
	}
}

// Name returns the provider identifier for logs.
func (g *ClaudeGenerator) Name() string {
	if g.cfg.Vision {
		return "claude-vision"
	}
	return "claude"
}

// Generate calls Anthropic's Messages API with a single forced tool use.
// The tool's input_schema embeds the wardrobe item IDs as an enum, which makes
// hallucinated IDs structurally impossible. Image input is added to the user
// message when Vision is enabled.
func (g *ClaudeGenerator) Generate(ctx context.Context, req GeneratorRequest) ([]Outfit, *Usage, error) {
	if g.cfg.APIKey == "" {
		return nil, nil, errors.New("claude generator: ANTHROPIC_API_KEY is not set")
	}

	systemPrompt := buildSystemPrompt(req.Weather, req.RecentBoards, req.TopArchetypes, req.Panels, req.Backgrounds)
	tool := g.buildOutfitTool(req.Items)
	userContent := g.buildUserContent(ctx, req)

	// B2: mark the system prompt block as cacheable (ephemeral, ~5min
	// TTL on Anthropic's side). The system prompt is 2–4k tokens of
	// stylist rules + archetype context + surface list + few-shot
	// examples — all stable across rapid regenerates for the same
	// user. First call writes the cache (~25% token surcharge on that
	// call); subsequent calls within the TTL read it back at ~10% of
	// normal price. For a typical regenerate-heavy session the net
	// saving is 20–30% input tokens and ~50% latency on the cached
	// portion. Only applied when the prompt is long enough to clear
	// Anthropic's 1024-token minimum — shorter prompts skip cache and
	// ship as a plain string block to avoid wasted cache-write cost.
	var systemBlocks []claudeSystemBlock
	if len(systemPrompt) >= 4096 { // ~1024 tokens of English prose
		systemBlocks = []claudeSystemBlock{{
			Type:         "text",
			Text:         systemPrompt,
			CacheControl: &claudeCacheControl{Type: "ephemeral"},
		}}
	} else {
		systemBlocks = []claudeSystemBlock{{Type: "text", Text: systemPrompt}}
	}

	payload := claudeRequest{
		Model:     g.cfg.Model,
		MaxTokens: 2048,
		System:    systemBlocks,
		Tools:     []claudeTool{tool},
		ToolChoice: &claudeToolChoice{
			Type: "tool",
			Name: tool.Name,
		},
		Messages: []claudeMessage{
			{Role: "user", Content: userContent},
		},
	}

	resp, err := g.callAPI(ctx, payload)
	// Usage is best-effort: any response we got back gets its tokens
	// extracted even when the parse below fails, so the observability
	// ledger captures partial-failure costs (Anthropic still bills for
	// tokens consumed before a 400/422 response).
	usage := extractClaudeUsage(resp, g.cfg.Model)
	if err != nil {
		return nil, usage, err
	}

	outfits, rawJSON, parseErr := parseClaudeToolUse(resp, tool.Name)
	usage.RawResponse = rawJSON
	return outfits, usage, parseErr
}

// extractClaudeUsage pulls token counts from a Claude response's
// `usage` block. The shape is documented at
// https://docs.anthropic.com/en/api/messages — input_tokens,
// output_tokens, and (when prompt caching is engaged) the
// cache_creation_input_tokens / cache_read_input_tokens pair.
//
// Returns a zero-valued Usage when resp is nil or the usage block
// is absent, never nil. The caller can rely on a real pointer to
// stamp the row even on transport failures.
func extractClaudeUsage(resp *claudeResponse, model string) *Usage {
	u := &Usage{
		Provider:      "anthropic",
		Model:         model,
		PromptVersion: PromptVersion,
	}
	if resp == nil || resp.Usage == nil {
		return u
	}
	asInt := func(v any) int {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
		return 0
	}
	u.InputTokens = asInt(resp.Usage["input_tokens"])
	u.OutputTokens = asInt(resp.Usage["output_tokens"])
	u.CacheWriteTokens = asInt(resp.Usage["cache_creation_input_tokens"])
	u.CacheReadTokens = asInt(resp.Usage["cache_read_input_tokens"])
	return u
}

// buildOutfitTool constructs the propose_outfits tool definition. The items[]
// schema is constrained to the exact set of valid wardrobe IDs via enum, which
// makes ID hallucination structurally impossible.
func (g *ClaudeGenerator) buildOutfitTool(items []GenItem) claudeTool {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}

	itemsSchema := map[string]any{
		"type":        "array",
		"minItems":    4,
		"maxItems":    5,
		"description": "Wardrobe item IDs that make up the outfit.",
		"items": map[string]any{
			"type": "string",
			"enum": ids,
		},
	}

	layoutSchema := map[string]any{
		"type":        "object",
		"description": "Map each item ID in this outfit to a visual role: hero (centerpiece), support (anchors the look), or accent (small detail).",
		"additionalProperties": map[string]any{
			"type": "string",
			"enum": []string{"hero", "support", "accent"},
		},
	}

	// P1-H: mark the single signature piece per outfit so the Collage can
	// render a statement bag at roughly twice the size of a plain belt,
	// even when both carry the "accent" layoutRole.
	visualWeightsSchema := map[string]any{
		"type":        "object",
		"description": "Mark the signature piece with \"statement\". Supporting garments may be \"supporting\" or omitted. Quiet background items may be \"minor\".",
		"additionalProperties": map[string]any{
			"type": "string",
			"enum": []string{"statement", "supporting", "minor"},
		},
	}

	outfitSchema := map[string]any{
		"type":     "object",
		"required": []string{"name", "description", "items", "layoutRoles"},
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "2-4 word outfit name.",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "1-sentence vibe.",
			},
			"rationale": map[string]any{
				"type":        "string",
				"description": "1-sentence explanation tying the choice to the user's archetype and the weather.",
			},
			"items":         itemsSchema,
			"layoutRoles":   layoutSchema,
			"visualWeights": visualWeightsSchema,
			"suggestions": map[string]any{
				"type":        "array",
				"description": "Optional text hints for missing complementary items.",
				"items":       map[string]any{"type": "string"},
			},
		},
	}

	return claudeTool{
		Name:        "propose_outfits",
		Description: "Propose 3-4 distinct outfits for the user, each obeying the structural and stylistic rules in the system prompt.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"outfits"},
			"properties": map[string]any{
				"outfits": map[string]any{
					"type":     "array",
					"minItems": 3,
					"maxItems": 4,
					"items":    outfitSchema,
				},
			},
		},
	}
}

// buildUserContent assembles the user message: a compact text inventory plus
// (when vision is enabled) one image block per item, each preceded by a short
// caption so Claude can correlate the picture with the ID.
func (g *ClaudeGenerator) buildUserContent(ctx context.Context, req GeneratorRequest) []claudeContent {
	var content []claudeContent

	// Reuse the shared text inventory builder, then append the tool-use instruction.
	textMsg := BuildUserMessage(req.Items) + "\n\nPropose 3-4 outfits using the propose_outfits tool."

	content = append(content, claudeContent{
		Type: "text",
		Text: textMsg,
	})

	if !g.cfg.Vision || g.imgRepo == nil {
		return content
	}

	// Vision: send each item's PNG bytes (or JPEG fallback) as a base64 image
	// block, preceded by a short caption block linking the image to its ID.
	limit := g.cfg.MaxVisionItems
	if limit <= 0 || limit > len(req.Items) {
		limit = len(req.Items)
	}

	visionItems := req.Items[:limit]

	// B1: load images in parallel with a bounded semaphore. Serial
	// loading cost (visionItems × ~10ms GridFS round-trip) was blocking
	// the outfit hot path for 150–500ms on typical wardrobes. Bounded
	// concurrency keeps MongoDB connection use predictable — each
	// generation never uses more than visionImageParallelism pool
	// slots at once, so N simultaneous users can't starve each other.
	//
	// Results are collected into an indexed slice so the final content
	// blocks stay in the same order as the original items array —
	// Claude is insensitive to order today, but preserving it keeps
	// the image captions aligned with any eventual deterministic
	// replay/debugging.
	type loaded struct {
		item      GenItem
		data      []byte
		mediaType string
		err       error
	}
	results := make([]loaded, len(visionItems))
	sem := make(chan struct{}, visionImageParallelism)
	var wg sync.WaitGroup
	for i, item := range visionItems {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, it GenItem) {
			defer wg.Done()
			defer func() { <-sem }()
			data, mt, err := g.loadImage(ctx, it.ID)
			results[idx] = loaded{item: it, data: data, mediaType: mt, err: err}
		}(i, item)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			g.logger.Printf("claude: skipping image for %s: %v", r.item.ID, r.err)
			continue
		}
		caption := fmt.Sprintf("Image for ID=%s (%s — %s)", r.item.ID, r.item.Category, r.item.Label)
		content = append(content,
			claudeContent{Type: "text", Text: caption},
			claudeContent{
				Type: "image",
				Source: &claudeImageSource{
					Type:      "base64",
					MediaType: r.mediaType,
					Data:      base64.StdEncoding.EncodeToString(r.data),
				},
			},
		)
	}

	if len(req.Items) > limit {
		content = append(content, claudeContent{
			Type: "text",
			Text: fmt.Sprintf("(Note: only the first %d of %d wardrobe items have images attached. Trust the text inventory for items without images.)", limit, len(req.Items)),
		})
	}

	return content
}

// loadImage tries the background-removed PNG first (better for visual
// reasoning — transparent background, no scene clutter) and falls back to the
// original JPEG. The wardrobe service stores PNGs under "{itemID}-png".
func (g *ClaudeGenerator) loadImage(ctx context.Context, itemID string) ([]byte, string, error) {
	// Prefer the cutout PNG.
	data, ct, err := g.imgRepo.GetImage(ctx, itemID+"-png")
	if err == nil && len(data) > 0 {
		if ct == "" {
			ct = "image/png"
		}
		return data, ct, nil
	}
	// Fall back to the original image.
	data, ct, err = g.imgRepo.GetImage(ctx, itemID)
	if err != nil {
		return nil, "", err
	}
	if ct == "" {
		ct = "image/jpeg"
	}
	return data, ct, nil
}

// callAPI sends the request to Anthropic's Messages API and returns the parsed
// response. It does not retry — Anthropic's typical p99 is well under the 90s
// HTTP client timeout, and a single failure should fall through to the
// deterministic fallback rather than burn budget on a hopeful retry.
func (g *ClaudeGenerator) callAPI(ctx context.Context, payload claudeRequest) (*claudeResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal claude request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build claude request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", g.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("claude request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read claude response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("claude returned %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed claudeResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode claude response: %w", err)
	}
	return &parsed, nil
}

// parseClaudeToolUse extracts the propose_outfits tool input from Claude's
// response and unmarshals it into []Outfit. Because the tool was forced via
// tool_choice, exactly one tool_use block is expected.
//
// Returns (outfits, rawJSON, err) — rawJSON is the unparsed tool input,
// captured for prompt archival (P1-11). Empty string when no matching
// block was found.
func parseClaudeToolUse(resp *claudeResponse, expectedTool string) ([]Outfit, string, error) {
	for _, block := range resp.Content {
		if block.Type != "tool_use" || block.Name != expectedTool {
			continue
		}
		var payload struct {
			Outfits []Outfit `json:"outfits"`
		}
		if err := json.Unmarshal(block.Input, &payload); err != nil {
			return nil, string(block.Input), fmt.Errorf("unmarshal tool input: %w", err)
		}
		return payload.Outfits, string(block.Input), nil
	}
	return nil, "", fmt.Errorf("no %q tool_use block in claude response (stop_reason=%s)", expectedTool, resp.StopReason)
}

// ── Anthropic Messages API wire types ───────────────────────────────────────

type claudeRequest struct {
	Model      string              `json:"model"`
	MaxTokens  int                 `json:"max_tokens"`
	System     []claudeSystemBlock `json:"system,omitempty"`
	Messages   []claudeMessage     `json:"messages"`
	Tools      []claudeTool        `json:"tools,omitempty"`
	ToolChoice *claudeToolChoice   `json:"tool_choice,omitempty"`
}

// claudeSystemBlock is a system-prompt content block. Switched from a
// plain string to the block form in B2 so cache_control can mark the
// block cacheable (ephemeral, ~5min TTL) — Anthropic's prompt-caching
// feature. The wire format accepts both, but blocks are required for
// any per-block metadata like caching.
type claudeSystemBlock struct {
	Type         string              `json:"type"` // "text"
	Text         string              `json:"text"`
	CacheControl *claudeCacheControl `json:"cache_control,omitempty"`
}

// claudeCacheControl annotates a content block for Anthropic's prompt
// cache. Type "ephemeral" is currently the only supported option;
// cache lives ~5min on Anthropic's side, read-cost ~10% of normal,
// write-cost ~25% surcharge on first use.
type claudeCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content []claudeContent `json:"content"`
}

type claudeContent struct {
	Type   string             `json:"type"`
	Text   string             `json:"text,omitempty"`
	Source *claudeImageSource `json:"source,omitempty"`
}

type claudeImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type claudeTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type claudeToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type claudeResponse struct {
	ID         string                 `json:"id"`
	StopReason string                 `json:"stop_reason"`
	Content    []claudeResponseBlock  `json:"content"`
	Usage map[string]any `json:"usage,omitempty"`
}

type claudeResponseBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}
