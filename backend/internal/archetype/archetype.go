// Package archetype scores wardrobe items against 12 Jungian style archetypes.
// Pure logic — no HTTP or database dependencies.
package archetype

import (
	"math"
	"sort"
	"strings"
)

// Scores maps archetype name → score (0.0–1.0).
type Scores map[string]float64

// ScoredArchetype is one archetype with its computed score.
type ScoredArchetype struct {
	Name  string  `json:"name"`
	Title string  `json:"title"`
	Score float64 `json:"score"`
}

// Profile defines the style signals for one archetype.
type Profile struct {
	Name            string
	Title           string
	Description     string
	ColorSignals    []string
	MaterialSignals []string
	FormalityRange  []string
	KeyTraits       []string
}

// ItemTraits is the minimal trait data needed for scoring.
type ItemTraits struct {
	Category       string
	Color          string
	ColorSecondary string
	Fabric         string
	Style          string
	Occasion       string
	OverallStyle   string
	Details        string
}

// Profiles defines all 12 archetypes.
var Profiles = map[string]Profile{
	"ruler": {
		Name: "ruler", Title: "The Ruler",
		Description:     "Authority through refinement. Every piece signals control, quality, and status.",
		ColorSignals:    []string{"black", "navy", "camel", "charcoal", "gold", "royal blue", "burgundy"},
		MaterialSignals: []string{"wool", "leather", "silk", "cashmere"},
		FormalityRange:  []string{"business casual", "business", "formal"},
		KeyTraits:       []string{"structured", "tailored", "power", "investment", "authority"},
	},
	"rebel": {
		Name: "rebel", Title: "The Rebel",
		Description:     "Rules are suggestions. Mix registers — leather with tailoring, streetwear with luxury.",
		ColorSignals:    []string{"black", "charcoal", "dark", "white", "red"},
		MaterialSignals: []string{"leather", "denim", "metal", "cotton twill"},
		FormalityRange:  []string{"casual", "smart casual"},
		KeyTraits:       []string{"edge", "utility", "bold", "mixed", "streetwear"},
	},
	"creator": {
		Name: "creator", Title: "The Creator",
		Description:     "Fashion as self-expression. Unexpected combinations and distinctive silhouettes.",
		ColorSignals:    []string{"red", "purple", "cobalt", "multi", "mustard", "emerald"},
		MaterialSignals: []string{"silk", "textured knit", "linen", "mixed"},
		FormalityRange:  []string{"casual", "smart casual", "creative formal"},
		KeyTraits:       []string{"distinctive", "creative", "statement", "curated", "pattern"},
	},
	"lover": {
		Name: "lover", Title: "The Lover",
		Description:     "Sensuality in fabric. Pieces that feel as good as they look.",
		ColorSignals:    []string{"blush", "rose", "cream", "soft pink", "burgundy", "champagne"},
		MaterialSignals: []string{"silk", "satin", "cashmere", "velvet", "lace"},
		FormalityRange:  []string{"smart casual", "creative formal", "formal"},
		KeyTraits:       []string{"luxe", "elegant", "romantic", "refined", "drape"},
	},
	"hero": {
		Name: "hero", Title: "The Hero",
		Description:     "Dressed for action. Clean lines, functional materials, confidence over flash.",
		ColorSignals:    []string{"navy", "white", "black", "steel", "forest green"},
		MaterialSignals: []string{"cotton", "performance", "structured wool", "nylon"},
		FormalityRange:  []string{"casual", "smart casual", "business casual"},
		KeyTraits:       []string{"clean", "functional", "reliable", "confident", "performance"},
	},
	"explorer": {
		Name: "explorer", Title: "The Explorer",
		Description:     "Freedom in movement. Layers that adapt, materials that travel.",
		ColorSignals:    []string{"earth", "olive", "tan", "rust", "sky blue", "sand"},
		MaterialSignals: []string{"canvas", "denim", "cotton", "linen", "suede"},
		FormalityRange:  []string{"casual", "smart casual"},
		KeyTraits:       []string{"versatile", "layering", "natural", "rugged", "travel"},
	},
	"sage": {
		Name: "sage", Title: "The Sage",
		Description:     "Quiet intelligence. Timeless pieces, muted palettes, understated confidence.",
		ColorSignals:    []string{"charcoal", "grey", "navy", "forest", "ivory", "slate"},
		MaterialSignals: []string{"wool", "cotton", "tweed", "fine knit"},
		FormalityRange:  []string{"smart casual", "business casual", "business"},
		KeyTraits:       []string{"timeless", "muted", "quality", "intellectual", "restrained"},
	},
	"magician": {
		Name: "magician", Title: "The Magician",
		Description:     "Transformation through style. Dark tones, unexpected textures, layered depth.",
		ColorSignals:    []string{"black", "deep purple", "midnight", "charcoal", "dark green"},
		MaterialSignals: []string{"silk", "velvet", "sheer", "textured", "leather"},
		FormalityRange:  []string{"smart casual", "creative formal"},
		KeyTraits:       []string{"dark", "layered", "textural", "mysterious", "depth"},
	},
	"innocent": {
		Name: "innocent", Title: "The Innocent",
		Description:     "Effortless simplicity. Clean whites, soft fabrics, uncomplicated shapes.",
		ColorSignals:    []string{"white", "light blue", "pastel", "cream", "soft yellow"},
		MaterialSignals: []string{"cotton", "linen", "jersey", "chambray"},
		FormalityRange:  []string{"casual", "smart casual"},
		KeyTraits:       []string{"clean", "simple", "soft", "effortless", "fresh"},
	},
	"caregiver": {
		Name: "caregiver", Title: "The Caregiver",
		Description:     "Warmth you can wear. Soft textures, approachable colors, comfortable fits.",
		ColorSignals:    []string{"light blue", "lavender", "cream", "soft pink", "warm grey"},
		MaterialSignals: []string{"cotton", "jersey", "soft knit", "linen", "fleece"},
		FormalityRange:  []string{"casual", "smart casual"},
		KeyTraits:       []string{"comfort", "soft", "approachable", "relaxed", "warm"},
	},
	"jester": {
		Name: "jester", Title: "The Jester",
		Description:     "Life's too short for boring clothes. Bold prints, playful colors.",
		ColorSignals:    []string{"bright", "yellow", "orange", "electric blue", "hot pink", "multi"},
		MaterialSignals: []string{"printed", "graphic", "novelty", "denim"},
		FormalityRange:  []string{"casual"},
		KeyTraits:       []string{"bold", "playful", "fun", "bright", "conversation"},
	},
	"orphan": {
		Name: "orphan", Title: "The Everyman",
		Description:     "Belonging through relatability. Versatile basics, dependable fits.",
		ColorSignals:    []string{"blue", "grey", "khaki", "white", "brown"},
		MaterialSignals: []string{"cotton", "denim", "jersey", "chino"},
		FormalityRange:  []string{"casual", "smart casual"},
		KeyTraits:       []string{"versatile", "basic", "dependable", "neutral", "authentic"},
	},
}

// ScoreItems scores a set of wardrobe items against all 12 archetypes.
func ScoreItems(items []ItemTraits) Scores {
	// Collect all signals.
	var allColors, allMaterials []string
	var allSignals strings.Builder
	accessoryCount := 0
	totalCount := len(items)

	for _, item := range items {
		if item.Color != "" {
			allColors = append(allColors, strings.ToLower(item.Color))
		}
		if item.ColorSecondary != "" {
			allColors = append(allColors, strings.ToLower(item.ColorSecondary))
		}
		if item.Fabric != "" {
			allMaterials = append(allMaterials, strings.ToLower(item.Fabric))
		}
		cat := strings.ToLower(item.Category)
		if strings.Contains(cat, "accessor") || cat == "bag" {
			accessoryCount++
		}
		// Build combined signal text for keyword matching.
		for _, s := range []string{item.Style, item.Occasion, item.Details, item.OverallStyle} {
			if s != "" {
				allSignals.WriteString(" ")
				allSignals.WriteString(strings.ToLower(s))
			}
		}
	}

	signalText := allSignals.String()
	scores := make(Scores, len(Profiles))

	for key, profile := range Profiles {
		var score float64

		// Color match (0.3)
		colorHits := 0
		for _, signal := range profile.ColorSignals {
			for _, c := range allColors {
				if strings.Contains(c, signal) {
					colorHits++
					break
				}
			}
		}
		score += (float64(colorHits) / math.Max(float64(len(profile.ColorSignals)), 1)) * 0.3

		// Material match (0.25)
		materialHits := 0
		for _, signal := range profile.MaterialSignals {
			for _, m := range allMaterials {
				if strings.Contains(m, signal) {
					materialHits++
					break
				}
			}
		}
		score += (float64(materialHits) / math.Max(float64(len(profile.MaterialSignals)), 1)) * 0.25

		// Formality match (0.2)
		formalityHits := 0
		for _, f := range profile.FormalityRange {
			if strings.Contains(signalText, f) {
				formalityHits++
			}
		}
		score += (float64(formalityHits) / math.Max(float64(len(profile.FormalityRange)), 1)) * 0.2

		// Style keyword match (0.15)
		traitHits := 0
		for _, kw := range profile.KeyTraits {
			if strings.Contains(signalText, kw) {
				traitHits++
			}
		}
		score += (float64(traitHits) / math.Max(float64(len(profile.KeyTraits)), 1)) * 0.15

		// Accessory intensity (0.1) — favors archetypes that value accessories.
		if totalCount > 0 && accessoryCount > 0 {
			intensity := math.Min(float64(accessoryCount)/float64(totalCount), 1)
			switch key {
			case "ruler", "rebel", "creator", "magician", "lover":
				score += intensity * 0.1
			}
		}

		scores[key] = math.Round(score*1000) / 1000 // 3 decimal places
	}

	return scores
}

// TopN returns the top N archetypes sorted by score descending.
func TopN(scores Scores, n int) []ScoredArchetype {
	result := make([]ScoredArchetype, 0, len(scores))
	for name, score := range scores {
		p := Profiles[name]
		result = append(result, ScoredArchetype{Name: name, Title: p.Title, Score: score})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Score > result[j].Score })
	if n > 0 && n < len(result) {
		result = result[:n]
	}
	return result
}

// Merge blends two score maps with a weight for the new scores.
// result = alpha * newScores + (1-alpha) * existing
func Merge(existing, newScores Scores, alpha float64) Scores {
	merged := make(Scores, len(Profiles))
	for key := range Profiles {
		e := existing[key]
		n := newScores[key]
		merged[key] = math.Round((alpha*n+(1-alpha)*e)*1000) / 1000
	}
	return merged
}

// Suggestion is a predicted item for a specific archetype and category.
type Suggestion struct {
	Category string // "outerwear", "top", "bottom", "footwear", "accessory"
	Label    string // human-readable item description
	Color    string // primary color signal
	Material string // primary material signal
}

// Suggestions maps each archetype to its predicted item suggestions.
// Multiple items per category allow for wardrobe variety.
var Suggestions = map[string][]Suggestion{
	"ruler": {
		{Category: "outerwear", Label: "structured wool overcoat", Color: "charcoal", Material: "wool"},
		{Category: "outerwear", Label: "navy double-breasted blazer", Color: "navy", Material: "wool blend"},
		{Category: "accessory", Label: "leather dress belt", Color: "black", Material: "leather"},
		{Category: "accessory", Label: "gold cufflinks", Color: "gold", Material: "metal"},
		{Category: "footwear", Label: "oxford dress shoes", Color: "black", Material: "leather"},
		{Category: "footwear", Label: "burgundy leather loafers", Color: "burgundy", Material: "leather"},
		{Category: "top", Label: "crisp white dress shirt", Color: "white", Material: "cotton"},
		{Category: "top", Label: "light blue poplin shirt", Color: "light blue", Material: "cotton"},
		{Category: "bottom", Label: "charcoal wool trousers", Color: "charcoal", Material: "wool"},
		{Category: "bottom", Label: "navy chinos", Color: "navy", Material: "cotton"},
	},
	"rebel": {
		{Category: "outerwear", Label: "black leather jacket", Color: "black", Material: "leather"},
		{Category: "outerwear", Label: "distressed denim jacket", Color: "dark blue", Material: "denim"},
		{Category: "accessory", Label: "silver chain bracelet", Color: "silver", Material: "metal"},
		{Category: "accessory", Label: "black leather cuff", Color: "black", Material: "leather"},
		{Category: "footwear", Label: "black combat boots", Color: "black", Material: "leather"},
		{Category: "footwear", Label: "black high-top sneakers", Color: "black", Material: "canvas"},
		{Category: "top", Label: "graphic band tee", Color: "black", Material: "cotton"},
		{Category: "top", Label: "black henley shirt", Color: "black", Material: "cotton"},
		{Category: "bottom", Label: "black slim jeans", Color: "black", Material: "denim"},
		{Category: "bottom", Label: "dark cargo pants", Color: "charcoal", Material: "cotton twill"},
	},
	"creator": {
		{Category: "accessory", Label: "statement oversized scarf", Color: "multi", Material: "silk"},
		{Category: "accessory", Label: "artisan ceramic pendant", Color: "earth", Material: "mixed"},
		{Category: "outerwear", Label: "patterned kimono jacket", Color: "multi", Material: "silk"},
		{Category: "footwear", Label: "unique printed sneakers", Color: "multi", Material: "canvas"},
		{Category: "top", Label: "asymmetric draped top", Color: "cobalt", Material: "silk blend"},
		{Category: "top", Label: "hand-dyed linen shirt", Color: "mustard", Material: "linen"},
		{Category: "bottom", Label: "wide-leg linen trousers", Color: "cream", Material: "linen"},
		{Category: "bottom", Label: "printed palazzo pants", Color: "multi", Material: "viscose"},
	},
	"lover": {
		{Category: "accessory", Label: "delicate gold pendant necklace", Color: "gold", Material: "gold"},
		{Category: "accessory", Label: "pearl drop earrings", Color: "cream", Material: "pearl"},
		{Category: "top", Label: "silk camisole", Color: "blush", Material: "silk"},
		{Category: "top", Label: "cashmere wrap sweater", Color: "rose", Material: "cashmere"},
		{Category: "footwear", Label: "strappy heeled sandals", Color: "nude", Material: "leather"},
		{Category: "outerwear", Label: "cashmere wrap", Color: "cream", Material: "cashmere"},
		{Category: "bottom", Label: "silk midi skirt", Color: "champagne", Material: "silk"},
	},
	"hero": {
		{Category: "outerwear", Label: "navy performance blazer", Color: "navy", Material: "performance fabric"},
		{Category: "footwear", Label: "clean white leather sneakers", Color: "white", Material: "leather"},
		{Category: "accessory", Label: "sport chronograph watch", Color: "steel", Material: "metal"},
		{Category: "top", Label: "fitted polo shirt", Color: "white", Material: "cotton pique"},
		{Category: "top", Label: "performance henley", Color: "navy", Material: "technical cotton"},
		{Category: "bottom", Label: "athletic-fit chinos", Color: "khaki", Material: "stretch cotton"},
	},
	"explorer": {
		{Category: "outerwear", Label: "waxed canvas field jacket", Color: "olive", Material: "waxed canvas"},
		{Category: "footwear", Label: "suede desert boots", Color: "sand", Material: "suede"},
		{Category: "accessory", Label: "canvas backpack", Color: "tan", Material: "canvas"},
		{Category: "top", Label: "chambray utility shirt", Color: "light blue", Material: "chambray"},
		{Category: "top", Label: "merino base layer", Color: "charcoal", Material: "merino wool"},
		{Category: "bottom", Label: "ripstop cargo shorts", Color: "olive", Material: "cotton ripstop"},
	},
	"sage": {
		{Category: "outerwear", Label: "charcoal tweed blazer", Color: "charcoal", Material: "tweed"},
		{Category: "accessory", Label: "minimalist leather watch", Color: "brown", Material: "leather"},
		{Category: "footwear", Label: "brown suede loafers", Color: "brown", Material: "suede"},
		{Category: "top", Label: "merino wool turtleneck", Color: "charcoal", Material: "merino wool"},
		{Category: "top", Label: "oxford button-down", Color: "grey", Material: "cotton oxford"},
		{Category: "bottom", Label: "grey flannel trousers", Color: "grey", Material: "wool flannel"},
	},
	"magician": {
		{Category: "outerwear", Label: "black velvet evening jacket", Color: "black", Material: "velvet"},
		{Category: "accessory", Label: "dark stone ring", Color: "black", Material: "silver"},
		{Category: "footwear", Label: "black Chelsea boots", Color: "black", Material: "leather"},
		{Category: "top", Label: "sheer layering shirt", Color: "black", Material: "sheer fabric"},
		{Category: "bottom", Label: "black tapered trousers", Color: "black", Material: "wool"},
	},
	"innocent": {
		{Category: "top", Label: "white linen button-down", Color: "white", Material: "linen"},
		{Category: "footwear", Label: "white canvas sneakers", Color: "white", Material: "canvas"},
		{Category: "accessory", Label: "simple straw tote", Color: "natural", Material: "straw"},
		{Category: "outerwear", Label: "light denim jacket", Color: "light blue", Material: "denim"},
		{Category: "bottom", Label: "white cotton chinos", Color: "white", Material: "cotton"},
	},
	"caregiver": {
		{Category: "top", Label: "soft cashmere pullover", Color: "lavender", Material: "cashmere"},
		{Category: "outerwear", Label: "cozy knit cardigan", Color: "cream", Material: "soft knit"},
		{Category: "accessory", Label: "woven fabric tote", Color: "warm grey", Material: "woven fabric"},
		{Category: "footwear", Label: "comfortable slip-on loafers", Color: "tan", Material: "soft leather"},
		{Category: "bottom", Label: "soft jersey wide-leg pants", Color: "light grey", Material: "jersey"},
	},
	"jester": {
		{Category: "top", Label: "colorful printed Hawaiian shirt", Color: "multi", Material: "rayon"},
		{Category: "accessory", Label: "fun patterned socks", Color: "multi", Material: "cotton blend"},
		{Category: "footwear", Label: "bright colored sneakers", Color: "electric blue", Material: "canvas"},
		{Category: "outerwear", Label: "color-block windbreaker", Color: "multi", Material: "nylon"},
		{Category: "bottom", Label: "bright printed shorts", Color: "yellow", Material: "cotton"},
	},
	"orphan": {
		{Category: "top", Label: "classic crew neck t-shirt", Color: "white", Material: "cotton"},
		{Category: "top", Label: "striped breton tee", Color: "navy", Material: "cotton"},
		{Category: "footwear", Label: "versatile white sneakers", Color: "white", Material: "leather"},
		{Category: "outerwear", Label: "navy zip-up hoodie", Color: "navy", Material: "cotton fleece"},
		{Category: "accessory", Label: "simple canvas belt", Color: "brown", Material: "canvas"},
		{Category: "bottom", Label: "classic blue jeans", Color: "blue", Material: "denim"},
	},
}

// SuggestMissingCategory returns a single human-readable suggestion for an item
// the user likely needs based on their archetype profile and existing categories.
func SuggestMissingCategory(topArchetypes []ScoredArchetype, existingCategories map[string]bool) string {
	if len(topArchetypes) == 0 {
		return ""
	}
	for _, arch := range topArchetypes {
		suggestions, ok := Suggestions[arch.Name]
		if !ok {
			continue
		}
		for _, s := range suggestions {
			if !existingCategories[s.Category] {
				return s.Label
			}
		}
	}
	return ""
}
