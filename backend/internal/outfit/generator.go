package outfit

import (
	"context"

	"mootd/backend/internal/archetype"
)

// GeneratorRequest is the provider-agnostic input to outfit generation.
// The handler builds it once and hands it to whichever Generator is active.
type GeneratorRequest struct {
	UserID        string
	Items         []GenItem
	TopArchetypes []archetype.ScoredArchetype
	Weather       Weather
	// RecentBoards carries the last few outfits the user actually saved. They
	// appear in the prompt twice: as "avoid repeating" (by name) and as
	// concrete positive examples (with description + rationale) so the model
	// learns the user's preferred stylistic register instead of re-deriving
	// it every call. Pass the richest data the caller has — empty strings
	// are elided in the prompt.
	RecentBoards []RecentBoard
	Panels       []SurfaceOption // surfaces the LLM may pick a panel from
	Backgrounds  []SurfaceOption // surfaces the LLM may pick a background from
	UseVision    bool            // ask the provider to use image input if it supports it
}

// GenItem is the trimmed wardrobe-item shape passed to generators.
// Image bytes are loaded lazily by providers that need them (vision-capable).
type GenItem struct {
	ID       string
	Category string
	Label    string
	Traits   map[string]string
}

// Weather is the optional weather context for outfit selection.
type Weather struct {
	Temperature string `json:"temperature,omitempty"` // e.g. "12"
	Condition   string `json:"condition,omitempty"`   // e.g. "Rainy"
	Unit        string `json:"unit,omitempty"`        // "C" or "F"
}

// Generator is the provider-agnostic interface for producing outfit suggestions
// from a wardrobe. Implementations include OllamaGenerator (local Qwen),
// OpenAIGenerator, and ClaudeGenerator (Anthropic Messages API with tool use
// + vision).
type Generator interface {
	// Name identifies the provider for logging/metrics.
	Name() string
	// Generate returns 3-4 outfit suggestions plus the token usage of the
	// underlying LLM call. The Usage is non-nil when the call reached a
	// real provider (success or failure-with-response); on transport-level
	// errors before any response, both outfits and Usage may be nil. The
	// observability wrapper records whichever is available so we never
	// drop a billable call from the ledger.
	//
	// Returned outfits may have hallucinated item IDs — the caller is
	// expected to validate.
	Generate(ctx context.Context, req GeneratorRequest) ([]Outfit, *Usage, error)
}

// Usage captures the per-call billable inputs to the LLM. Populated by each
// Generator from its provider's response shape so the observability wrapper
// can compute cost and write a single llm_calls row per call.
//
// Zero-valued tokens are legitimate (Ollama is free, transport failures with
// no response, etc.); the wrapper still writes a row with whatever metadata
// is available so admins can see "this call happened, it failed before
// charging anything."
type Usage struct {
	Provider         string // "anthropic" | "openai" | "ollama"
	Model            string // exact model id, e.g. "claude-sonnet-4-20250514"
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int // Anthropic only — 0 for OpenAI/Ollama
	CacheWriteTokens int // Anthropic only — 0 for OpenAI/Ollama
	PromptVersion    string // PromptVersion at call time, stamped for filtering
}
