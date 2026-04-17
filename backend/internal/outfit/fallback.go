package outfit

import (
	"fmt"
	"sort"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/wardrobe"
)

// buildFallbackOutfits constructs deterministic outfits from the wardrobe when
// the LLM generator fails or under-delivers. It uses the same archetype-scoring
// logic the rest of the system relies on, so the resulting outfits stay coherent
// with the user's style profile even without an LLM in the loop.
//
// The strategy is intentionally simple:
//  1. Bucket items by role (top/bottom/footwear/outerwear/accessory).
//  2. Score each item against the user's top archetypes and sort each bucket
//     so the most-aligned pieces come first.
//  3. Walk through up to `count` distinct top/bottom/footwear combinations and
//     attach the best-aligned outerwear (when present) and accessory.
//
// The function returns *unvalidated* outfits — the caller is expected to run
// them through Handler.validateOutfits, which dedupes, re-scores, and assigns
// SmartSuggestions.
func buildFallbackOutfits(items []wardrobe.ClothingItem, top []archetype.ScoredArchetype, count int) []Outfit {
	if count <= 0 || len(items) == 0 {
		return nil
	}

	tops := filterByRole(items, "TOPS")
	bottoms := filterByRole(items, "BOTTOMS")
	footwear := filterByRole(items, "FOOTWEAR")
	outerwear := filterByRole(items, "OUTERWEAR")
	accessories := filterByRole(items, "ACCESSORIES")

	if len(tops) == 0 || len(bottoms) == 0 || len(footwear) == 0 {
		// Without the three required slots there's nothing meaningful to fall back to.
		return nil
	}

	rankByArchetype(tops, top)
	rankByArchetype(bottoms, top)
	rankByArchetype(footwear, top)
	rankByArchetype(outerwear, top)
	rankByArchetype(accessories, top)

	primaryArchetype := "your style"
	if len(top) > 0 {
		if profile, ok := archetype.Profiles[top[0].Name]; ok {
			primaryArchetype = profile.Title
		}
	}

	out := make([]Outfit, 0, count)
	for i := 0; i < count; i++ {
		t := tops[i%len(tops)]
		b := bottoms[i%len(bottoms)]
		f := footwear[i%len(footwear)]

		ids := []string{t.ID, b.ID, f.ID}
		layout := map[string]string{
			t.ID: "support",
			b.ID: "support",
			f.ID: "support",
		}

		if len(outerwear) > 0 {
			o := outerwear[i%len(outerwear)]
			ids = append(ids, o.ID)
			layout[o.ID] = "hero"
		} else {
			// Promote the top to hero when there is no outerwear.
			layout[t.ID] = "hero"
		}

		if len(accessories) > 0 {
			a := accessories[i%len(accessories)]
			ids = append(ids, a.ID)
			layout[a.ID] = "accent"
		}

		out = append(out, Outfit{
			Name:        fmt.Sprintf("%s Look %d", primaryArchetype, i+1),
			Description: fmt.Sprintf("Wardrobe basics anchored by %s and %s.", t.Label, b.Label),
			Rationale:   fmt.Sprintf("Built from your highest-scoring items for %s.", primaryArchetype),
			Items:       ids,
			LayoutRoles: layout,
		})
	}

	return out
}

// filterByRole returns items whose category matches the requested role bucket.
// The role labels match those used by ClassifyRole in ollama_generator.go.
func filterByRole(items []wardrobe.ClothingItem, role string) []wardrobe.ClothingItem {
	out := make([]wardrobe.ClothingItem, 0, len(items))
	for _, item := range items {
		if ClassifyRole(item.Category) == role {
			out = append(out, item)
		}
	}
	return out
}

// rankByArchetype sorts items in place so the highest-scoring items for the
// user's top archetypes come first. The score for each item is computed by
// running it individually through ScoreItems and summing the contribution from
// each top archetype, weighted by that archetype's overall score.
func rankByArchetype(items []wardrobe.ClothingItem, top []archetype.ScoredArchetype) {
	if len(top) == 0 {
		return
	}
	itemScores := make(map[string]float64, len(items))
	for _, item := range items {
		traits := []archetype.ItemTraits{{
			Category:       item.Category,
			Color:          item.Traits["color"],
			ColorSecondary: item.Traits["color_secondary"],
			Fabric:         item.Traits["fabric"],
			Style:          item.Traits["style"],
			Occasion:       item.Traits["occasion"],
			OverallStyle:   item.Traits["overall_style"],
			Details:        item.Traits["details"],
		}}
		scores := archetype.ScoreItems(traits)
		var total float64
		for _, a := range top {
			total += scores[a.Name] * a.Score
		}
		itemScores[item.ID] = total
	}
	sort.SliceStable(items, func(i, j int) bool {
		return itemScores[items[i].ID] > itemScores[items[j].ID]
	})
}

