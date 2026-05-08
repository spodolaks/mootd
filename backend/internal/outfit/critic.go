package outfit

import "context"

// Critic is an optional capability a Generator can opt into to
// review its OWN proposed outfits with a cheaper second pass.
// mootd#64 — pre-eval data shows ~5% of generated outfits are
// visibly bad (wrong archetype register, conflicting palettes,
// weather mismatch). A small Haiku-tier follow-up pass that
// scores each outfit 1-10 catches the tail.
//
// Defined as a separate interface (not a Generate-shaped extension
// of Generator) so providers without a critic implementation —
// Ollama (local), OpenAI today — don't have to ship a no-op. The
// outfit service does a runtime type-assert to detect support.
type Critic interface {
	Critique(ctx context.Context, req CritiqueRequest) (CritiqueResult, error)
}

// CritiqueRequest is what the critic sees: the proposals the
// upstream Generate returned, plus the same archetype + weather
// the original prompt carried. We deliberately do NOT pass the
// full wardrobe trait list — the critic should only judge
// stylistic coherence, not refit to the catalog.
type CritiqueRequest struct {
	Outfits      []Outfit
	TopArchetype string
	Weather      Weather
	// UserID stamped through so any A/B prompt routing applied
	// to the critic prompt itself stays consistent for this user.
	UserID string
}

// CritiqueResult carries the per-outfit scores plus usage so the
// caller can stamp an llm_calls row alongside the original
// Generate call. Implementations that fail before a response
// return a non-nil error and zero Result; callers downgrade
// gracefully (skip the regenerate step rather than failing the
// whole outfit-gen flow).
type CritiqueResult struct {
	Scores []CritiqueScore
	Usage  *Usage
}

// CritiqueScore is the per-outfit verdict. Score is 1..10; the
// scorer is told 5 is the borderline (anything strictly below
// regenerates). Reason is a one-sentence explanation surfaced
// in logs + the optional admin trace view.
type CritiqueScore struct {
	OutfitName string `json:"outfitName"`
	Score      int    `json:"score"`
	Reason     string `json:"reason,omitempty"`
}

// LowScoreThreshold is the cutoff below which the service
// regenerates. Set as a package-level var (not const) so eval
// runs can tune it without recompiling. Default 5 matches the
// proposal in mootd#64.
var LowScoreThreshold = 5

// AnyBelowThreshold reports whether any outfit's score is
// strictly below LowScoreThreshold. Pulled out so the service
// wiring stays trivially testable.
func AnyBelowThreshold(scores []CritiqueScore) bool {
	for _, s := range scores {
		if s.Score < LowScoreThreshold {
			return true
		}
	}
	return false
}

// FormatScores renders the scores as a single line for log
// tagging — `[Edge: 8 · Soft: 3 · Bold: 7]`. Used in service-
// level "critic kicked in" logging so a glance at the output
// makes the verdicts visible.
func FormatScores(scores []CritiqueScore) string {
	if len(scores) == 0 {
		return "[no scores]"
	}
	out := "["
	for i, s := range scores {
		if i > 0 {
			out += " · "
		}
		out += s.OutfitName + ": " + intToStr(s.Score)
	}
	out += "]"
	return out
}

// intToStr is the smallest possible int-to-string — pulled out
// so FormatScores stays import-free of strconv. Single digit is
// the common case (1..10).
func intToStr(n int) string {
	if n >= 0 && n < 10 {
		return string(rune('0' + n))
	}
	if n == 10 {
		return "10"
	}
	// Fallback for unexpected values — keeps FormatScores from
	// blowing up on a buggy critic implementation.
	digits := []byte{}
	if n < 0 {
		digits = append(digits, '-')
		n = -n
	}
	for n > 0 {
		digits = append([]byte{'0' + byte(n%10)}, digits...)
		n /= 10
	}
	if len(digits) == 0 {
		return "0"
	}
	return string(digits)
}
