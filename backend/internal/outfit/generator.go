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
	RecentOutfits []string        // names of recently-worn outfits to avoid repeating
	Panels        []SurfaceOption // surfaces the LLM may pick a panel from
	Backgrounds   []SurfaceOption // surfaces the LLM may pick a background from
	UseVision     bool            // ask the provider to use image input if it supports it
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
// from a wardrobe. Implementations include OllamaGenerator (local Qwen) and
// ClaudeGenerator (Anthropic Messages API with tool use + vision).
type Generator interface {
	// Name identifies the provider for logging/metrics.
	Name() string
	// Generate returns 3-4 outfit suggestions. Returned outfits may have
	// hallucinated item IDs — the caller is expected to validate.
	Generate(ctx context.Context, req GeneratorRequest) ([]Outfit, error)
}
