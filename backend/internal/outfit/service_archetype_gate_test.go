package outfit

import (
	"testing"

	"mootd/backend/internal/archetype"
)

// mootd#214 — gateByArchetypeFit is the hybrid gate: it surfaces on-archetype
// outfits first and drops off-archetype ones, but keeps a floor so the
// moodboard is never left empty when a batch skews off-archetype.

func mkOutfit(name string, scores map[string]float64) Outfit {
	return Outfit{Name: name, ArchetypeScores: scores}
}

// Off-archetype outfits are dropped when enough aligned ones remain, and the
// survivors come back ranked best-first by alignment with the user's profile.
func TestGateByArchetypeFit_DropsOffArchetypeAndRanksAligned(t *testing.T) {
	// User leans ruler (strongest) then creator.
	profile := archetype.Scores{"ruler": 1.0, "creator": 0.5}

	outfits := []Outfit{
		mkOutfit("rebel-look", map[string]float64{"rebel": 0.9, "ruler": 0.1}),   // off-brand
		mkOutfit("ruler-weak", map[string]float64{"ruler": 0.6, "creator": 0.2}), // aligned
		mkOutfit("ruler-strong", map[string]float64{"ruler": 0.95}),              // aligned (best)
		mkOutfit("creator-look", map[string]float64{"creator": 0.8, "ruler": 0.1}),
	}

	got := gateByArchetypeFit(outfits, profile)

	// 3 aligned (ruler-weak, ruler-strong, creator-look) meets the floor, so the
	// off-brand rebel outfit is dropped entirely.
	if len(got) != 3 {
		t.Fatalf("expected 3 gated outfits, got %d", len(got))
	}
	for _, o := range got {
		if o.Name == "rebel-look" {
			t.Errorf("off-archetype outfit %q should have been dropped", o.Name)
		}
	}
	// Ranked by weighted alignment (ruler*1.0 + creator*0.5):
	// ruler-strong 0.95 > ruler-weak 0.70 > creator-look 0.50.
	if got[0].Name != "ruler-strong" {
		t.Errorf("expected best-aligned outfit first, got %q", got[0].Name)
	}
}

// When too few outfits are on-archetype, the gate tops up with the best of the
// rest to honour the floor — aligned outfits still rank ahead of the rest.
func TestGateByArchetypeFit_FloorTopsUpWithBestOfRest(t *testing.T) {
	profile := archetype.Scores{"ruler": 1.0}

	outfits := []Outfit{
		mkOutfit("rebel-weak", map[string]float64{"rebel": 0.5, "ruler": 0.1}),
		mkOutfit("ruler", map[string]float64{"ruler": 0.9}), // only aligned one
		mkOutfit("rebel-strong", map[string]float64{"rebel": 0.9, "ruler": 0.3}),
	}

	got := gateByArchetypeFit(outfits, profile)

	if len(got) != archetypeGateFloor {
		t.Fatalf("expected floor of %d outfits, got %d", archetypeGateFloor, len(got))
	}
	if got[0].Name != "ruler" {
		t.Errorf("aligned outfit should rank first, got %q", got[0].Name)
	}
	// Topped up from the rest, ordered by alignment (ruler weight): rebel-strong
	// (0.3) before rebel-weak (0.1).
	if got[1].Name != "rebel-strong" {
		t.Errorf("expected best-of-rest (rebel-strong) second, got %q", got[1].Name)
	}
}

// With a non-empty but all-zero profile (no scoring signal), gating should be a no-op.
func TestGateByArchetypeFit_ZeroProfileReturnsUnchanged(t *testing.T) {
	outfits := []Outfit{
		mkOutfit("a", map[string]float64{"ruler": 0.9}),
		mkOutfit("b", map[string]float64{"rebel": 0.8}),
	}

	profile := archetype.Scores{"ruler": 0.0, "rebel": 0.0}
	got := gateByArchetypeFit(outfits, profile)

	if len(got) != len(outfits) {
		t.Fatalf("zero-signal profile should not gate: got %d, want %d", len(got), len(outfits))
	}
	for i := range outfits {
		if got[i].Name != outfits[i].Name {
			t.Fatalf("zero-signal profile should preserve order: got[%d]=%q, want %q", i, got[i].Name, outfits[i].Name)
		}
	}
}

	if len(got) != len(outfits) {
		t.Fatalf("empty profile should not gate: got %d, want %d", len(got), len(outfits))
	}
}
