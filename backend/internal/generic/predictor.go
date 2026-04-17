package generic

import (
	"strings"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/wardrobe"
)

// categoryGroup maps detailed category strings to broad groups.
func categoryGroup(category string) string {
	cat := strings.ToLower(category)
	switch {
	case strings.Contains(cat, "top") || strings.Contains(cat, "shirt") ||
		strings.Contains(cat, "blouse") || strings.Contains(cat, "tshirt") ||
		strings.Contains(cat, "sweater") || strings.Contains(cat, "hoodie"):
		return "top"
	case strings.Contains(cat, "bottom") || strings.Contains(cat, "pant") ||
		strings.Contains(cat, "trouser") || strings.Contains(cat, "jean") ||
		strings.Contains(cat, "skirt") || strings.Contains(cat, "short"):
		return "bottom"
	case strings.Contains(cat, "outer") || strings.Contains(cat, "jacket") ||
		strings.Contains(cat, "coat") || strings.Contains(cat, "blazer"):
		return "outerwear"
	case strings.Contains(cat, "footwear") || strings.Contains(cat, "shoe") ||
		strings.Contains(cat, "sneaker") || strings.Contains(cat, "boot"):
		return "footwear"
	default:
		return "accessory"
	}
}

// PredictMissingItems returns items the user likely needs based on their
// archetype profile and current wardrobe gaps.
//
// It considers:
//   - Completely missing required categories (top/bottom/footwear/accessory)
//   - Shallow categories (< 2 items) that limit outfit variety
//   - Archetype-specific item preferences
func PredictMissingItems(
	items []wardrobe.ClothingItem,
	scores archetype.Scores,
	threshold int,
) []PredictedItem {
	if len(items) >= threshold {
		return nil
	}

	// Count items per broad category group.
	groupCounts := map[string]int{}
	for _, item := range items {
		g := categoryGroup(item.Category)
		groupCounts[g]++
	}

	// Determine what we need.
	// Priority 1: completely missing required slots.
	// Priority 2: categories with < 2 items (need variety for 3 distinct outfits).
	// Priority 3: nice-to-have variety.
	type need struct {
		category string
		priority int
	}

	requiredSlots := []string{"top", "bottom", "footwear", "accessory"}
	var needs []need

	for _, slot := range requiredSlots {
		count := groupCounts[slot]
		if count == 0 {
			needs = append(needs, need{category: slot, priority: 1})
			needs = append(needs, need{category: slot, priority: 2}) // need at least 2
		} else if count < 2 {
			needs = append(needs, need{category: slot, priority: 2})
		}
	}

	// Outerwear is nice-to-have but useful for layering variety.
	if groupCounts["outerwear"] == 0 {
		needs = append(needs, need{category: "outerwear", priority: 3})
	}

	// Add variety needs: extra tops/bottoms if we have few.
	for _, slot := range []string{"top", "bottom"} {
		if groupCounts[slot] >= 2 && groupCounts[slot] < 3 {
			needs = append(needs, need{category: slot, priority: 3})
		}
	}

	top := archetype.TopN(scores, 3)
	if len(top) == 0 {
		return nil
	}

	// Collect existing labels to avoid suggesting items too similar to what user has.
	existingLabels := make(map[string]bool)
	for _, item := range items {
		existingLabels[strings.ToLower(item.Label)] = true
	}

	// Fill needs from archetype suggestions.
	budget := threshold - len(items)
	if budget > MaxGenericItems {
		budget = MaxGenericItems
	}

	var predictions []PredictedItem
	usedLabels := make(map[string]bool)

	for _, n := range needs {
		if len(predictions) >= budget {
			break
		}

		// Find a suggestion from top archetypes for this category.
		found := false
		for _, arch := range top {
			suggestions, ok := archetype.Suggestions[arch.Name]
			if !ok {
				continue
			}
			for _, s := range suggestions {
				if s.Category != n.category {
					continue
				}
				key := strings.ToLower(s.Label)
				if usedLabels[key] || existingLabels[key] {
					continue
				}

				predictions = append(predictions, PredictedItem{
					Category:        s.Category,
					Label:           s.Label,
					SourceArchetype: arch.Name,
					Priority:        n.priority,
					Traits: map[string]string{
						"color":  s.Color,
						"fabric": s.Material,
					},
				})
				usedLabels[key] = true
				found = true
				break
			}
			if found {
				break
			}
		}
	}

	return predictions
}
