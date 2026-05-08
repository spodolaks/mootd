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

	// Creativity is a 0..1 user preference that maps to the
	// LLM's temperature (mootd#67). 0 = predictable, 0.5 =
	// current default, 1 = high variance. See CreativityToTemperature
	// for the mapping. Zero-value (no profile preference) means
	// "use the provider's existing default" — same behaviour as
	// pre-#67 callers.
	Creativity float64

	// OnProgress, when non-nil, opts the request into streaming
	// (mootd#62). Generators that support SSE / streaming JSON
	// fire callbacks as partial output arrives so the FE can
	// render description + items as they're produced. Generators
	// without streaming support ignore the callback and return
	// the full result at end of call (back-compat).
	//
	// The callback receives a snapshot of the partial state. It
	// may be called many times (anywhere from 0 to dozens per
	// generation depending on chunk granularity) and is expected
	// to be cheap — generators serialise calls so the callback
	// can write to the wire without locking.
	//
	// Returning a non-nil error from the callback aborts the
	// stream. Generators wrap it as an outer error from Generate.
	OnProgress StreamCallback
}

// StreamCallback receives partial generation progress (mootd#62).
// Implementations are expected to be cheap — typically writing
// one SSE event to the response stream. See SubmitGenerate for
// the wire format.
type StreamCallback func(GenerateProgress) error

// GenerateProgress is one snapshot of an in-flight generation.
// Each event carries the cumulative state, not a delta — so a
// late-joining client (or a client that drops events) sees a
// consistent picture from any single event.
type GenerateProgress struct {
	// Stage is a coarse milestone label so the FE can render an
	// indeterminate progress message early ("connecting" → "drafting"
	// → "finishing") before any outfit content arrives.
	Stage ProgressStage `json:"stage"`
	// Outfits is the cumulative outfit list seen so far. Empty
	// during the connecting / streaming-prelude stages; populated
	// progressively as the LLM emits each entry.
	Outfits []Outfit `json:"outfits,omitempty"`
	// Description is a free-text "what is the model doing right
	// now" hint surfaced by some providers (e.g. Anthropic content
	// blocks). Optional; FE may ignore.
	Description string `json:"description,omitempty"`
}

// ProgressStage is the coarse-grained progress label.
type ProgressStage string

const (
	StageConnecting ProgressStage = "connecting" // SSE established
	StageStreaming  ProgressStage = "streaming"  // tokens flowing
	StageDone       ProgressStage = "done"       // final result attached
	StageError      ProgressStage = "error"      // generation failed; details on Description
)

// CreativityToTemperature translates the user-facing 0..1 slider
// to a provider temperature (mootd#67). Values are clamped to
// the LLM's safe range:
//
//	0.0 → 0.5  (conservative, low variance)
//	0.5 → 0.9  (current default; identical behaviour to pre-#67)
//	1.0 → 1.2  (high variance)
//
// Linear in between. Returns 0 when creativity is 0 (caller's
// signal that no preference was supplied), so generators can
// keep their compiled-in defaults.
func CreativityToTemperature(creativity float64) float64 {
	if creativity <= 0 {
		return 0
	}
	if creativity > 1 {
		creativity = 1
	}
	// Linear: 0 → 0.5, 0.5 → 0.9, 1 → 1.2.
	// Two-segment: 0..0.5 maps to 0.5..0.9 (slope 0.8);
	//              0.5..1 maps to 0.9..1.2 (slope 0.6).
	if creativity <= 0.5 {
		return 0.5 + creativity*0.8
	}
	return 0.9 + (creativity-0.5)*0.6
}

// GenItem is the trimmed wardrobe-item shape passed to generators.
// Image bytes are loaded lazily by providers that need them (vision-capable).
type GenItem struct {
	ID       string
	Category string
	Label    string
	Traits   map[string]string
	// Preferred is true for items the user actually owns (their
	// uploads). False for archetype-default fillers injected to widen
	// the pool. Kept alongside Weight as a fast boolean for callers
	// that just need to know the source. Backwards-compatible —
	// existing tests that build GenItems directly continue to work.
	Preferred bool
	// Weight is the LLM-facing preference signal, in [0,1]. Owned
	// items default to 1.0, fillers to FillerWeight (currently 0.5).
	// The prompt prints this number inline and includes a per-outfit
	// filler quota that scales with wardrobe size — small wardrobes
	// get a higher target filler count so the LLM keeps producing
	// fresh combinations even with only 3-4 owned items, instead of
	// looping over the same permutations.
	Weight float64
}

// FillerWeight is the default weight assigned to archetype-default
// fillers in the LLM-facing pool. 0.5 — half the weight of an owned
// item — turns "filler" from a binary "use only when needed" hint
// into a real preference signal the LLM can balance numerically.
const FillerWeight = 0.5

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
	// RawResponse is the textual content the model produced, captured
	// before our parser ran. Anthropic: tool-use input JSON.
	// OpenAI: choices[0].message.content. Ollama: response body.
	// Used by P1-11 prompt archival. Empty when transport failed
	// before any response.
	RawResponse string
}
