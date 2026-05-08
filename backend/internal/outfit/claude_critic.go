// Critique implementation for ClaudeGenerator (mootd#64). Lives in
// its own file so claude_generator.go doesn't grow further; the
// existing wire types (claudeRequest, claudeMessage, claudeContent,
// claudeTool, claudeToolChoice, claudeResponse) and the callAPI +
// extractClaudeUsage helpers are re-used.

package outfit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CriticModelDefault is the cheap+fast model the critic pass
// runs on by default — the whole point of the critic is to be
// ~10× cheaper than the primary Generate call so it can act as
// a quality gate without doubling per-outfit cost. Operators
// override via OUTFIT_CRITIC_MODEL when a newer Haiku ships.
const CriticModelDefault = "claude-haiku-4-5"

// criticToolName is the forced tool the model invokes — gives
// us schema-validated structured output (per-outfit name + score
// + reason) without the brittle "extract JSON from prose" path.
const criticToolName = "rate_outfits"

// Critique implements the Critic interface for Claude. Sends a
// compact prompt listing the proposed outfits and asks Haiku to
// score each 1-10 against the user's archetype + weather. Returns
// per-outfit scores so the service can decide whether to
// regenerate (mootd#64).
//
// The critic carries its OWN model selection (Haiku by default
// even when the primary Generate uses Sonnet) and its OWN
// request — it doesn't share the Generate request body, just the
// outfits + archetype + weather context.
func (g *ClaudeGenerator) Critique(ctx context.Context, req CritiqueRequest) (CritiqueResult, error) {
	if g.cfg.APIKey == "" {
		return CritiqueResult{}, fmt.Errorf("claude critic: ANTHROPIC_API_KEY is empty")
	}
	if len(req.Outfits) == 0 {
		// Nothing to score — skip the round-trip rather than
		// spend tokens on an empty list.
		return CritiqueResult{}, nil
	}

	model := g.cfg.CriticModel
	if model == "" {
		model = CriticModelDefault
	}

	system := buildCriticSystemPrompt(req.TopArchetype, req.Weather)
	user := buildCriticUserMessage(req.Outfits)
	tool := buildCriticTool(req.Outfits)

	payload := claudeRequest{
		Model:     model,
		MaxTokens: 1024,
		System: []claudeSystemBlock{{
			Type: "text",
			Text: system,
		}},
		Messages: []claudeMessage{{
			Role:    "user",
			Content: []claudeContent{{Type: "text", Text: user}},
		}},
		Tools: []claudeTool{tool},
		ToolChoice: &claudeToolChoice{
			Type: "tool",
			Name: criticToolName,
		},
	}

	resp, err := g.callAPI(ctx, payload)
	if err != nil {
		return CritiqueResult{}, fmt.Errorf("claude critic: api call: %w", err)
	}

	scores, raw, err := parseCriticResponse(resp)
	if err != nil {
		return CritiqueResult{}, fmt.Errorf("claude critic: parse: %w", err)
	}

	// Re-use the same usage extractor the primary Generate path
	// uses so the cache-token + raw-response stamping stays
	// consistent across both calls. Override the model field
	// because extractClaudeUsage stamps cfg.Model by default and
	// the critic ran on a different model.
	usage := extractClaudeUsage(resp, model)
	usage.RawResponse = raw

	return CritiqueResult{Scores: scores, Usage: usage}, nil
}

// buildCriticSystemPrompt constructs the small instruction
// block. Deliberately compact — Haiku does best with short,
// concrete rubrics. The 5-as-borderline rule mirrors
// LowScoreThreshold so the model's behaviour and the service's
// regeneration trigger stay aligned.
func buildCriticSystemPrompt(topArchetype string, weather Weather) string {
	var sb strings.Builder
	sb.WriteString("You are a stylist QA reviewer. You are given outfit proposals built from a user's wardrobe and asked to score each one 1-10 on whether it works for the user's archetype and current weather.\n\n")
	sb.WriteString("Scoring rubric:\n")
	sb.WriteString(" - 9-10 : exemplary — strong archetype fit, palette coherent, weather-appropriate.\n")
	sb.WriteString(" - 7-8  : solid — minor friction but the outfit works.\n")
	sb.WriteString(" - 5-6  : borderline — wearable but feels generic, off-archetype, or weather-mismatched.\n")
	sb.WriteString(" - 1-4  : bad — should be regenerated. Wrong register, conflicting palette, ignores weather, or bizarre pairing.\n\n")
	sb.WriteString("Use the full 1-10 range. Don't rate everything 7. Be honest — the service regenerates anything you score below 5.\n\n")
	if topArchetype != "" {
		sb.WriteString("User's top archetype: ")
		sb.WriteString(topArchetype)
		sb.WriteString(".\n")
	}
	if weather.Temperature != "" || weather.Condition != "" {
		sb.WriteString("Current weather: ")
		if weather.Temperature != "" {
			sb.WriteString(weather.Temperature)
			if weather.Unit != "" {
				sb.WriteString(weather.Unit)
			}
		}
		if weather.Condition != "" {
			if weather.Temperature != "" {
				sb.WriteString(", ")
			}
			sb.WriteString(weather.Condition)
		}
		sb.WriteString(".\n")
	}
	sb.WriteString("\nReturn one score per outfit via the rate_outfits tool.")
	return sb.String()
}

// buildCriticUserMessage formats the proposed outfits as a
// compact list. We pass each outfit's name + description +
// rationale + items (by id+label when snapshots are present)
// so the model sees what was proposed, not just what we plan
// to render.
func buildCriticUserMessage(outfits []Outfit) string {
	var sb strings.Builder
	sb.WriteString("Outfits to review:\n\n")
	for i, o := range outfits {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, o.Name)
		if o.Description != "" {
			fmt.Fprintf(&sb, "   description: %s\n", sanitiseUserText(o.Description))
		}
		if o.Rationale != "" {
			fmt.Fprintf(&sb, "   rationale: %s\n", sanitiseUserText(o.Rationale))
		}
		// outfit-side ItemSnapshots carry label/category which is
		// what the model actually wants for scoring; raw ids alone
		// are nearly useless. Fall through to ids when the
		// upstream Generate didn't attach snapshots.
		if len(o.ItemSnapshots) > 0 {
			sb.WriteString("   items:\n")
			for _, s := range o.ItemSnapshots {
				fmt.Fprintf(&sb, "     - %s (%s)\n", sanitiseUserText(s.Label), s.Category)
			}
		} else if len(o.Items) > 0 {
			fmt.Fprintf(&sb, "   item ids: %s\n", strings.Join(o.Items, ", "))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// buildCriticTool builds the tool schema. The outfits enum
// pinned on `outfitName` makes hallucinated names structurally
// impossible — Haiku has to score the exact outfits we sent.
func buildCriticTool(outfits []Outfit) claudeTool {
	names := make([]string, 0, len(outfits))
	for _, o := range outfits {
		names = append(names, o.Name)
	}
	return claudeTool{
		Name:        criticToolName,
		Description: "Record a 1-10 score and short reason for each proposed outfit.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"scores": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"outfitName", "score"},
						"properties": map[string]any{
							"outfitName": map[string]any{
								"type":        "string",
								"enum":        names,
								"description": "The name of the outfit you're rating, copied verbatim from the input.",
							},
							"score": map[string]any{
								"type":        "integer",
								"minimum":     1,
								"maximum":     10,
								"description": "1-10 quality score per the rubric.",
							},
							"reason": map[string]any{
								"type":        "string",
								"maxLength":   140,
								"description": "One-sentence rationale for the score. Be specific.",
							},
						},
					},
				},
			},
			"required": []string{"scores"},
		},
	}
}

// parseCriticResponse extracts the rate_outfits tool input and
// unmarshals it into []CritiqueScore. Returns the raw JSON of
// the tool call alongside so the caller can stamp it on the
// llm_calls archive row (P1-11).
func parseCriticResponse(resp *claudeResponse) ([]CritiqueScore, string, error) {
	for _, block := range resp.Content {
		if block.Type != "tool_use" || block.Name != criticToolName {
			continue
		}
		var payload struct {
			Scores []CritiqueScore `json:"scores"`
		}
		if err := json.Unmarshal(block.Input, &payload); err != nil {
			return nil, string(block.Input), fmt.Errorf("unmarshal tool input: %w", err)
		}
		return payload.Scores, string(block.Input), nil
	}
	return nil, "", fmt.Errorf("no %q tool_use block in response (stop_reason=%s)", criticToolName, resp.StopReason)
}

// Compile-time satisfies Critic.
var _ Critic = (*ClaudeGenerator)(nil)
