// Package outfit generates outfit suggestions using a pluggable LLM provider
// (Anthropic Claude or local Ollama).
package outfit

import (
	"encoding/json"
	"fmt"
)

// Outfit is a suggested combination of wardrobe items.
type Outfit struct {
	Name            string             `json:"name"`
	Description     string             `json:"description"`
	Items           []string           `json:"items"`                     // wardrobe item IDs (tops, bottoms, shoes, accessories)
	Rationale       string             `json:"rationale,omitempty"`       // 1-line stylist explanation tied to archetype/weather
	LayoutRoles     map[string]string  `json:"layoutRoles,omitempty"`     // itemID → "hero" | "support" | "accent"
	Suggestions     []string           `json:"suggestions,omitempty"`     // text hints for complementary items not in wardrobe
	ArchetypeScores map[string]float64 `json:"archetypeScores,omitempty"` // per-outfit archetype alignment
	SmartSuggestion string             `json:"smartSuggestion,omitempty"` // archetype-driven item suggestion (<20 items)
	Weather         *Weather           `json:"weather,omitempty"`         // weather context the outfit was generated for (display chip)
	Palette         []string           `json:"palette,omitempty"`         // dominant colors per item as #RRGGBB, up to 4, deduped
		PanelID         string             `json:"panelId,omitempty"`         // LLM-picked surface id; read on input, kept on output for debug
	BackgroundID    string             `json:"backgroundId,omitempty"`    // LLM-picked surface id; read on input, kept on output for debug
	PanelURL        string             `json:"panelUrl,omitempty"`        // resolved URL for the panel texture
	BackgroundURL   string             `json:"backgroundUrl,omitempty"`   // resolved URL for the ambient background
}

// GenerateResponse is returned from GET /v1/outfits.
type GenerateResponse struct {
	Outfits []Outfit `json:"outfits"`
}

// ollamaRequest is sent to POST /api/chat.
type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   string          `json:"format"`
	Think    bool            `json:"think"` // disable thinking phase for faster structured output
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaResponse is the shape returned by POST /api/chat.
type ollamaResponse struct {
	Message ollamaMessage `json:"message"`
}

// llmOutfitsResponse is the JSON shape the LLM is asked to return.
type llmOutfitsResponse struct {
	Outfits []Outfit `json:"outfits"`
}

// parseLLMResponse handles multiple JSON formats the LLM might return.
// It normalizes them all into []Outfit with flat Items arrays.
func parseLLMResponse(raw string) ([]Outfit, error) {
	// Try standard format first: {"outfits": [...]}
	var standard llmOutfitsResponse
	if err := json.Unmarshal([]byte(raw), &standard); err == nil && len(standard.Outfits) > 0 {
		// Check if items are populated — the LLM might return the right key but empty items.
		hasItems := false
		for _, o := range standard.Outfits {
			if len(o.Items) > 0 {
				hasItems = true
				break
			}
		}
		if hasItems {
			return standard.Outfits, nil
		}
	}

	// Try alternate format: {"outfit_combinations": [...]} with slot-based items.
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &rawMap); err != nil {
		return nil, fmt.Errorf("unmarshal raw map: %w", err)
	}

	// Find the outfits array regardless of key name.
	var outfitsRaw json.RawMessage
	for _, key := range []string{"outfits", "outfit_combinations", "combinations", "results"} {
		if v, ok := rawMap[key]; ok {
			outfitsRaw = v
			break
		}
	}
	if outfitsRaw == nil {
		return nil, fmt.Errorf("no recognized outfits key in response")
	}

	// Parse as array of generic objects.
	var rawOutfits []map[string]json.RawMessage
	if err := json.Unmarshal(outfitsRaw, &rawOutfits); err != nil {
		return nil, fmt.Errorf("unmarshal outfits array: %w", err)
	}

	var result []Outfit
	for _, ro := range rawOutfits {
		o := Outfit{}

		// Extract name from "name" or "outfit_name". Best-effort parses —
		// bad values fall through to the next key; we ignore decode errors
		// on purpose and rely on the caller's validation.
		for _, key := range []string{"name", "outfit_name"} {
			if v, ok := ro[key]; ok {
				_ = json.Unmarshal(v, &o.Name)
				if o.Name != "" {
					break
				}
			}
		}

		// Extract description.
		if v, ok := ro["description"]; ok {
			_ = json.Unmarshal(v, &o.Description)
		}

		// Extract suggestions.
		if v, ok := ro["suggestions"]; ok {
			_ = json.Unmarshal(v, &o.Suggestions)
		}

		// Extract surface picks — raw IDs the service validates/resolves.
		if v, ok := ro["panelId"]; ok {
			_ = json.Unmarshal(v, &o.PanelID)
		}
		if v, ok := ro["backgroundId"]; ok {
			_ = json.Unmarshal(v, &o.BackgroundID)
		}

		// Extract items — could be flat array of strings, array of objects, or slot-based.
		var itemIDs []string

		// Try "items" as array of strings.
		if v, ok := ro["items"]; ok {
			var strItems []string
			if err := json.Unmarshal(v, &strItems); err == nil && len(strItems) > 0 {
				itemIDs = strItems
			} else {
				// Try as array of objects with "id" field.
				var objItems []struct{ ID string `json:"id"` }
				if err := json.Unmarshal(v, &objItems); err == nil {
					for _, obj := range objItems {
						if obj.ID != "" {
							itemIDs = append(itemIDs, obj.ID)
						}
					}
				}
			}
		}

		// If no "items" found, try slot-based format (top, bottom, footwear, accessory, outerwear).
		if len(itemIDs) == 0 {
			for _, slot := range []string{"top", "bottom", "bottoms", "footwear", "shoes", "accessory", "accessories", "outerwear", "jacket"} {
				v, ok := ro[slot]
				if !ok {
					continue
				}
				// Could be a single object {"id": "..."} or a string ID.
				var obj struct{ ID string `json:"id"` }
				if err := json.Unmarshal(v, &obj); err == nil && obj.ID != "" {
					itemIDs = append(itemIDs, obj.ID)
					continue
				}
				var strID string
				if err := json.Unmarshal(v, &strID); err == nil && strID != "" {
					itemIDs = append(itemIDs, strID)
					continue
				}
				// Could be array of objects/strings (for accessories).
				var arr []json.RawMessage
				if err := json.Unmarshal(v, &arr); err == nil {
					for _, elem := range arr {
						var aObj struct{ ID string `json:"id"` }
						if err := json.Unmarshal(elem, &aObj); err == nil && aObj.ID != "" {
							itemIDs = append(itemIDs, aObj.ID)
						} else {
							var aStr string
							if err := json.Unmarshal(elem, &aStr); err == nil && aStr != "" {
								itemIDs = append(itemIDs, aStr)
							}
						}
					}
				}
			}
		}

		o.Items = itemIDs
		result = append(result, o)
	}

	return result, nil
}
