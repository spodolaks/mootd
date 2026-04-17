package outfit

import (
	"io"
	"log"
	"testing"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/wardrobe"
)

// newTestService builds a Service with logs discarded so table-driven tests
// don't spam the output. It carries no dependencies — ValidateOutfits only
// reads the logger field.
func newTestService() *Service {
	return &Service{logger: log.New(io.Discard, "", 0)}
}

func newItem(id, category, label string) wardrobe.ClothingItem {
	return wardrobe.ClothingItem{
		ID:       id,
		Category: category,
		Label:    label,
		Traits:   map[string]string{},
	}
}

// TestValidateOutfits_DropsHallucinated covers the core contract: when the
// LLM references item IDs that aren't in the wardrobe, they're stripped. The
// remaining outfit must still satisfy the top/bottom/footwear requirement,
// otherwise the whole outfit is dropped.
func TestValidateOutfits_DropsHallucinated(t *testing.T) {
	svc := newTestService()
	items := []wardrobe.ClothingItem{
		newItem("t1", "tops", "white tee"),
		newItem("t2", "tops", "blue shirt"),
		newItem("b1", "bottoms", "black jeans"),
		newItem("f1", "footwear", "sneakers"),
		newItem("a1", "accessories", "watch"),
	}

	outfits := []Outfit{
		{
			Name:  "Valid",
			Items: []string{"t1", "b1", "f1", "a1"},
		},
		{
			Name:  "WithHallucination",
			Items: []string{"t2", "b1", "f1", "a1", "ghost-item-id"},
		},
		{
			Name:  "DroppedBecauseIncomplete",
			Items: []string{"t1", "b1", "ghost1", "ghost2"}, // no footwear
		},
	}

	got := svc.ValidateOutfits(outfits, items, archetype.Scores{})

	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "Valid" {
		t.Errorf("outfits[0].Name = %q, want %q", got[0].Name, "Valid")
	}
	if got[1].Name != "WithHallucination" {
		t.Errorf("outfits[1].Name = %q, want %q", got[1].Name, "WithHallucination")
	}
	// Hallucinated ID must be stripped from the surviving outfit.
	for _, id := range got[1].Items {
		if id == "ghost-item-id" {
			t.Errorf("ghost id leaked into Items: %v", got[1].Items)
		}
	}
	if len(got[1].Items) != 4 {
		t.Errorf("len(items) = %d, want 4 (ghost should be stripped)", len(got[1].Items))
	}
}

// TestValidateOutfits_MissingCategoryDropped ensures an outfit without a
// top, bottom, or footwear item is rejected even if all IDs are real.
func TestValidateOutfits_MissingCategoryDropped(t *testing.T) {
	svc := newTestService()
	items := []wardrobe.ClothingItem{
		newItem("t1", "tops", "tee"),
		newItem("t2", "tops", "tee two"),
		newItem("t3", "tops", "tee three"),
		newItem("t4", "tops", "tee four"),
	}
	outfits := []Outfit{
		{Name: "AllTops", Items: []string{"t1", "t2", "t3", "t4"}},
	}
	got := svc.ValidateOutfits(outfits, items, archetype.Scores{})
	if len(got) != 0 {
		t.Errorf("len = %d, want 0 — outfit missing bottom/footwear should be dropped", len(got))
	}
}

// TestValidateOutfits_DedupesIdenticalFingerprints ensures two outfits with
// the same item set collapse to one.
func TestValidateOutfits_DedupesIdenticalFingerprints(t *testing.T) {
	svc := newTestService()
	items := []wardrobe.ClothingItem{
		newItem("t1", "tops", "tee"),
		newItem("b1", "bottoms", "jeans"),
		newItem("f1", "footwear", "boots"),
		newItem("a1", "accessories", "hat"),
	}
	outfits := []Outfit{
		{Name: "FirstLook", Items: []string{"t1", "b1", "f1", "a1"}},
		// Same items, different order — should dedupe to first occurrence.
		{Name: "SecondLook", Items: []string{"a1", "f1", "b1", "t1"}},
	}
	got := svc.ValidateOutfits(outfits, items, archetype.Scores{})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 — duplicate outfits should collapse", len(got))
	}
	if got[0].Name != "FirstLook" {
		t.Errorf("kept %q, want %q (first occurrence)", got[0].Name, "FirstLook")
	}
}

// TestValidateOutfits_LayoutRolesStrippedForDroppedItems ensures the
// LayoutRoles map only contains entries for items that survived validation.
func TestValidateOutfits_LayoutRolesStrippedForDroppedItems(t *testing.T) {
	svc := newTestService()
	items := []wardrobe.ClothingItem{
		newItem("t1", "tops", "tee"),
		newItem("b1", "bottoms", "jeans"),
		newItem("f1", "footwear", "boots"),
		newItem("a1", "accessories", "belt"),
	}
	outfits := []Outfit{
		{
			Name:  "WithGhostRole",
			Items: []string{"t1", "b1", "f1", "a1", "ghost"},
			LayoutRoles: map[string]string{
				"t1":    "hero",
				"b1":    "support",
				"f1":    "accent",
				"a1":    "accent",
				"ghost": "hero", // must be cleaned out
			},
		},
	}
	got := svc.ValidateOutfits(outfits, items, archetype.Scores{})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if _, exists := got[0].LayoutRoles["ghost"]; exists {
		t.Errorf("ghost entry leaked into LayoutRoles: %v", got[0].LayoutRoles)
	}
	if len(got[0].LayoutRoles) != 4 {
		t.Errorf("len(LayoutRoles) = %d, want 4", len(got[0].LayoutRoles))
	}
}
