package outfit

import (
	"log"
	"os"
	"testing"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/wardrobe"
)

// TestValidateOutfits_ExcludesFillersFromArchetypeScoring proves the
// fix for mootd#70: outfits that reference archetype-default fillers
// (id prefix "ad_") MUST score only the user's own items so the
// moodboard-save flow doesn't drift the user's archetypeProfile
// toward the curator's seeding bias.
func TestValidateOutfits_ExcludesFillersFromArchetypeScoring(t *testing.T) {
	owned := wardrobe.ClothingItem{
		ID:       "wi_owned_top",
		Category: "tops",
		Label:    "Plain creator-leaning shirt",
		Traits:   map[string]string{"color": "red", "style": "creative"},
	}
	ownedBottom := wardrobe.ClothingItem{
		ID:       "wi_owned_bottom",
		Category: "bottoms",
		Label:    "Plain pants",
		Traits:   map[string]string{"color": "black"},
	}
	ownedShoes := wardrobe.ClothingItem{
		ID:       "wi_owned_shoes",
		Category: "footwear",
		Label:    "Sneakers",
		Traits:   map[string]string{"color": "white"},
	}
	// Filler is rebel-leaning: black leather jacket. Without the
	// fix, this would push o.ArchetypeScores toward rebel, which
	// the moodboard handler would then merge into the user's
	// stored archetypeProfile. That's the bug.
	fillerJacket := wardrobe.ClothingItem{
		ID:       "ad_rebel_jacket",
		Category: "outerwear",
		Label:    "Black leather biker jacket",
		Traits:   map[string]string{"color": "black", "style": "edge", "fabric": "leather"},
	}
	items := []wardrobe.ClothingItem{owned, ownedBottom, ownedShoes, fillerJacket}

	outfits := []Outfit{{
		Name:        "Mixed",
		Description: "user picks own items + one rebel filler",
		Items:       []string{owned.ID, ownedBottom.ID, ownedShoes.ID, fillerJacket.ID},
	}}

	svc := &Service{logger: log.New(os.Stderr, "", 0)}
	got := svc.ValidateOutfits(outfits, items, archetype.Scores{})

	if len(got) != 1 {
		t.Fatalf("expected 1 validated outfit, got %d (drops or dedup misfired)", len(got))
	}
	scores := got[0].ArchetypeScores
	if len(scores) == 0 {
		t.Fatalf("expected ArchetypeScores to be populated by ValidateOutfits")
	}

	// Compute what scoring SHOULD produce — owned items only.
	wantOwnedOnly := archetype.ScoreItems(itemsToTraits([]wardrobe.ClothingItem{owned, ownedBottom, ownedShoes}))
	// And what scoring used to produce — combined.
	withFiller := archetype.ScoreItems(itemsToTraits([]wardrobe.ClothingItem{owned, ownedBottom, ownedShoes, fillerJacket}))

	// The fix means o.ArchetypeScores ≈ owned-only scores, not the
	// combined version. Compare the rebel score specifically:
	// fillerJacket carries "edge" which the rebel profile keys on,
	// so the broken behaviour produces a higher rebel score than
	// the fixed behaviour.
	if scores["rebel"] != wantOwnedOnly["rebel"] {
		t.Errorf("rebel score = %.3f, want owned-only %.3f (combined-with-filler would have been %.3f) — fillers leaked into archetype scoring",
			scores["rebel"], wantOwnedOnly["rebel"], withFiller["rebel"])
	}
}

// TestValidateOutfits_AllFillersFallsBackToFullScoring documents the
// edge case: a sparse cold-start user might end up with an outfit
// where every picked item is a filler. ValidateOutfits then falls
// back to scoring the full set so the response still ships non-
// empty ArchetypeScores. Proves the fallback branch (mootd#70).
func TestValidateOutfits_AllFillersFallsBackToFullScoring(t *testing.T) {
	a := wardrobe.ClothingItem{ID: "ad_a", Category: "tops", Label: "Filler top", Traits: map[string]string{"color": "red"}}
	b := wardrobe.ClothingItem{ID: "ad_b", Category: "bottoms", Label: "Filler bottom", Traits: map[string]string{"color": "blue"}}
	c := wardrobe.ClothingItem{ID: "ad_c", Category: "footwear", Label: "Filler shoes", Traits: map[string]string{"color": "white"}}
	d := wardrobe.ClothingItem{ID: "ad_d", Category: "accessories", Label: "Filler belt", Traits: map[string]string{"color": "black"}}
	items := []wardrobe.ClothingItem{a, b, c, d}
	outfits := []Outfit{{
		Name:        "All filler",
		Description: "edge case",
		Items:       []string{a.ID, b.ID, c.ID, d.ID},
	}}

	svc := &Service{logger: log.New(os.Stderr, "", 0)}
	got := svc.ValidateOutfits(outfits, items, archetype.Scores{})
	if len(got) != 1 {
		t.Fatalf("expected 1 outfit, got %d", len(got))
	}
	if len(got[0].ArchetypeScores) == 0 {
		t.Errorf("all-filler outfit should still produce non-empty ArchetypeScores via fallback (got empty map)")
	}
}
