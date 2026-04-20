package outfit

import (
	"fmt"
	"strings"

	"mootd/backend/internal/archetype"
)

// baseSystemPrompt is the shared rule-set for all outfit generators (Claude + Ollama).
// It establishes the stylist persona, the structural rules, and the response shape.
const baseSystemPrompt = `You are a professional fashion stylist building daily outfits from a user's existing wardrobe.

STRUCTURAL RULES — EVERY outfit MUST satisfy ALL of these. An outfit missing any rule is INVALID and will be rejected:
1. Use ONLY item IDs from the provided wardrobe list. Never invent IDs.
2. Each outfit MUST have exactly 4 or 5 items. No fewer, no more.
3. MANDATORY categories in EVERY outfit — this is non-negotiable:
   - Exactly ONE item from the TOPS category (shirt, t-shirt, blouse, sweater, polo).
   - Exactly ONE item from the BOTTOMS category (pants, trousers, jeans, shorts, skirt).
   - Exactly ONE item from the FOOTWEAR category (shoes, sneakers, boots, sandals).
   An outfit without all three of these categories is INVALID.
4. At most one OUTERWEAR item (jacket OR coat — never both). Outerwear is IN ADDITION to the required top, not a replacement for it. A jacket is NOT a top.
5. At least one ACCESSORY (watch, necklace, bracelet, tie, pocket square, bag, belt, hat, scarf, eyewear).
6. Across the 3–4 returned outfits, vary the core item combinations and overall vibe.
7. If a key category is empty in the wardrobe, add 1–3 short "suggestions" describing the missing piece.

STYLE GUIDANCE:
- Prioritize color harmony (complementary, analogous, or monochrome) and texture balance.
- Honor the user's style archetype — lean into the aesthetics described below, not against them.
- Respect the weather: layer for cold, breathable for hot, water-resistant for rain.
- Avoid repeating outfits the user has already worn this week.

WRITING RULES FOR description AND rationale:
- BANNED WORDS — never use: "perfect", "perfect for", "versatile", "blends", "effortless", "timeless", "curated", "elevate", "elevated", "sophisticated", "classic", "essential", "staple", "go-to", "everyday", "edge" (as a noun), "vibe", "chic", "sleek", "polished", "refined", "statement piece".
- BANNED OPENERS — never start with: "Perfect for", "A [adjective]", "The ultimate", "Blends", "Combines", "Pairs".
- description is ONE sentence, max 12 words, concrete and specific. Name at least one garment by its actual property (e.g. "the pleated cream trouser softens the wool bomber"). Not a mood summary.
- rationale is ONE sentence, max 18 words, tying ONE specific styling decision to archetype OR weather — not both, not generic. Say WHY a particular pairing works, not that the outfit "works".
- Never restate the archetype name or weather description verbatim — reference them obliquely via a concrete choice ("the sharper lapel nods to the Ruler side", "the cotton tee reads cooler than the forecast suggests").
- If you can't write something specific, write less. A 6-word description beats a 14-word platitude.

OUTPUT:
- Each outfit needs: name (2-4 words), description (per rules above), rationale (per rules above), items (array of IDs), layoutRoles (mapping each item ID to "hero", "support", or "accent"), and optional suggestions for missing items.`

// buildSystemPrompt constructs the full system prompt with archetype, weather, and history context.
// It is shared by Ollama, Claude, and OpenAI generators.
//
// recentBoards drives two sections:
//  1. Anti-example list ("avoid repeating by name") — prevents stale outputs.
//  2. Positive few-shot list ("user recently chose these — notice the specific
//     pairings and stylistic register") — uses description + rationale from
//     the saved moodboards so the model sees the user's *accepted* voice,
//     not just archetype scores. This is the largest non-infra quality bump
//     available: it replaces a generic prompt with one that carries the
//     user's actual taste trail.
func buildSystemPrompt(weather Weather, recentBoards []RecentBoard, topArchetypes []archetype.ScoredArchetype, panels, backgrounds []SurfaceOption) string {
	var sb strings.Builder
	sb.WriteString(baseSystemPrompt)

	if len(panels) > 0 || len(backgrounds) > 0 {
		sb.WriteString("\n\nSURFACES — each outfit MUST include a panelId (the textured surface the flat-lay sits on) and a backgroundId (the ambient environment around the panel). Pick the option whose description, mood, and archetype affinity best matches the outfit's vibe. IDs must be taken verbatim from the lists below.\n")
		if len(panels) > 0 {
			sb.WriteString("\nAvailable panels:\n")
			writeSurfaceList(&sb, panels)
		}
		if len(backgrounds) > 0 {
			sb.WriteString("\nAvailable backgrounds:\n")
			writeSurfaceList(&sb, backgrounds)
		}
	}

	// Expanded archetype context — pull color, material, and key trait signals
	// from the profile so the model has concrete aesthetics to lean into,
	// instead of just an archetype name.
	if len(topArchetypes) > 0 {
		sb.WriteString("\n\nUSER STYLE ARCHETYPE:\n")
		for i, a := range topArchetypes {
			p, ok := archetype.Profiles[a.Name]
			if !ok {
				continue
			}
			weight := "primary"
			if i > 0 {
				weight = "secondary"
			}
			fmt.Fprintf(&sb,
				"- %s (%s, score=%.2f): %s\n  Colors: %s. Materials: %s. Key traits: %s.\n",
				p.Title, weight, a.Score, p.Description,
				strings.Join(p.ColorSignals, ", "),
				strings.Join(p.MaterialSignals, ", "),
				strings.Join(p.KeyTraits, ", "),
			)
		}
		sb.WriteString("Lean into these aesthetics when picking combinations.\n")
	}

	if weather.Temperature != "" && weather.Condition != "" {
		fmt.Fprintf(&sb, "\nWEATHER: %s°%s and %s — choose items appropriate for this weather.\n",
			weather.Temperature, weather.Unit, weather.Condition)
	}

	if len(recentBoards) > 0 {
		// Anti-example list: just names, so the model knows what not to recycle.
		sb.WriteString("\nRECENTLY WORN (avoid repeating the exact combination):\n")
		for _, b := range recentBoards {
			if b.OutfitName == "" {
				continue
			}
			fmt.Fprintf(&sb, "- %q\n", b.OutfitName)
		}

		// Positive examples: only emit when we actually have description or
		// rationale worth showing. A bare name tells the model nothing about
		// why the pairing worked, and would just bloat the context window.
		hasRichExample := false
		for _, b := range recentBoards {
			if b.Description != "" || b.Rationale != "" {
				hasRichExample = true
				break
			}
		}
		if hasRichExample {
			sb.WriteString("\nRECENTLY CHOSEN — the user saved these. Lean into the same stylistic register (item interplay, specificity of language, archetype nods). Do NOT copy them; generate fresh outfits that feel authored by the same person.\n")
			for _, b := range recentBoards {
				// Skip if either the name is missing (nothing to anchor the
				// example to) or both description and rationale are empty
				// (nothing to learn from).
				if b.OutfitName == "" {
					continue
				}
				if b.Description == "" && b.Rationale == "" {
					continue
				}
				// Use a compact multi-line format — LLMs absorb structure
				// better than prose here.
				fmt.Fprintf(&sb, "- %q\n", b.OutfitName)
				if b.Description != "" {
					fmt.Fprintf(&sb, "    description: %s\n", b.Description)
				}
				if b.Rationale != "" {
					fmt.Fprintf(&sb, "    rationale: %s\n", b.Rationale)
				}
				if b.TopArchetype != "" {
					fmt.Fprintf(&sb, "    top archetype at save: %s\n", b.TopArchetype)
				}
				if len(b.Palette) > 0 {
					fmt.Fprintf(&sb, "    palette: %s\n", strings.Join(b.Palette, ", "))
				}
			}
		}
	}

	// One concrete few-shot example anchors the response shape and the
	// expected level of stylistic reasoning. The example uses placeholder
	// IDs that the model knows it must NOT use literally.
	sb.WriteString(`
EXAMPLE OUTPUT (uses placeholder IDs — do NOT reuse them; notice description and rationale are specific, short, and free of banned words):
{
  "outfits": [
    {
      "name": "Charcoal Quiet Authority",
      "description": "Structured wool jacket sharpens the tonal trouser.",
      "rationale": "The matte watch keeps the silhouette quiet where the wool wants to shout.",
      "items": ["item_top_a", "item_bottom_a", "item_shoes_a", "item_outer_a", "item_acc_a"],
      "layoutRoles": {
        "item_outer_a": "hero",
        "item_top_a": "support",
        "item_bottom_a": "support",
        "item_shoes_a": "support",
        "item_acc_a": "accent"
      },
      "panelId": "panel-marble-slate",
      "backgroundId": "background-studio-neutral",
      "suggestions": []
    }
  ]
}`)

	return sb.String()
}

// BuildUserMessage produces a single compact representation of the wardrobe
// (one section grouped by role + a small per-item trait block). Shared by all
// generators: Ollama, OpenAI, and Claude (text part).
func BuildUserMessage(items []GenItem) string {
	type itemRef struct{ ID, Label string }
	groups := map[string][]itemRef{
		"TOPS": {}, "BOTTOMS": {}, "OUTERWEAR": {}, "FOOTWEAR": {}, "ACCESSORIES": {},
	}
	for _, item := range items {
		role := ClassifyRole(item.Category)
		groups[role] = append(groups[role], itemRef{item.ID, item.Label})
	}

	var inventory strings.Builder
	for _, role := range []string{"TOPS", "BOTTOMS", "OUTERWEAR", "FOOTWEAR", "ACCESSORIES"} {
		refs := groups[role]
		if len(refs) == 0 {
			continue
		}
		fmt.Fprintf(&inventory, "\n%s:\n", role)
		for _, ref := range refs {
			fmt.Fprintf(&inventory, "  - %s (%s)\n", ref.ID, ref.Label)
		}
	}

	// Compact per-item trait lines (id | category | key: value, key: value).
	var details strings.Builder
	for _, item := range items {
		fmt.Fprintf(&details, "%s | %s | %s", item.ID, item.Category, item.Label)
		if len(item.Traits) > 0 {
			details.WriteString(" |")
			for _, k := range []string{"color", "fabric", "style", "occasion", "overall_style"} {
				if v, ok := item.Traits[k]; ok && v != "" {
					fmt.Fprintf(&details, " %s=%s;", k, v)
				}
			}
		}
		details.WriteString("\n")
	}

	return fmt.Sprintf(
		"Wardrobe grouped by role:%s\nItem details:\n%s\nCreate 3-4 unique outfit combinations. Each MUST include a top + bottom + footwear + at least one accessory. Use only IDs from this list.",
		inventory.String(), details.String(),
	)
}

// writeSurfaceList formats `- id: Name — description. Mood: m1/m2. Archetypes: k1(0.8),k2(0.4).`
// one per line. Kept compact so the full menu fits in the prompt budget.
func writeSurfaceList(sb *strings.Builder, options []SurfaceOption) {
	for _, o := range options {
		fmt.Fprintf(sb, "- %s: %s", o.ID, o.Name)
		if o.Description != "" {
			fmt.Fprintf(sb, " — %s", o.Description)
		}
		if len(o.MoodTags) > 0 {
			fmt.Fprintf(sb, ". Mood: %s", strings.Join(o.MoodTags, "/"))
		}
		if len(o.ArchetypeAffinity) > 0 {
			sb.WriteString(". Archetypes: ")
			first := true
			for k, v := range o.ArchetypeAffinity {
				if !first {
					sb.WriteString(",")
				}
				fmt.Fprintf(sb, "%s(%.1f)", k, v)
				first = false
			}
		}
		sb.WriteString("\n")
	}
}

// ClassifyRole maps a wardrobe category string to a coarse role bucket used
// for grouping items in the LLM prompt.
func ClassifyRole(category string) string {
	cat := strings.ToLower(category)
	switch {
	case strings.Contains(cat, "top") || strings.Contains(cat, "shirt") ||
		strings.Contains(cat, "blouse") || strings.Contains(cat, "tshirt"):
		return "TOPS"
	case strings.Contains(cat, "bottom") || strings.Contains(cat, "pant") ||
		strings.Contains(cat, "trouser") || strings.Contains(cat, "jean") ||
		strings.Contains(cat, "skirt") || strings.Contains(cat, "short"):
		return "BOTTOMS"
	case strings.Contains(cat, "outer") || strings.Contains(cat, "jacket") ||
		strings.Contains(cat, "coat") || strings.Contains(cat, "blazer"):
		return "OUTERWEAR"
	case strings.Contains(cat, "footwear") || strings.Contains(cat, "shoe") ||
		strings.Contains(cat, "sneaker") || strings.Contains(cat, "boot"):
		return "FOOTWEAR"
	default:
		return "ACCESSORIES"
	}
}
