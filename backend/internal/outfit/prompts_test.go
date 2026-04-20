package outfit

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt_NoHistory(t *testing.T) {
	p := buildSystemPrompt(Weather{}, nil, nil, nil, nil)

	if strings.Contains(p, "RECENTLY WORN") {
		t.Error("prompt should not contain RECENTLY WORN when history is empty")
	}
	if strings.Contains(p, "RECENTLY CHOSEN") {
		t.Error("prompt should not contain RECENTLY CHOSEN when history is empty")
	}
}

func TestBuildSystemPrompt_NameOnlyRecent_EmitsAvoidListOnly(t *testing.T) {
	// Seven bare names — the old payload shape. We still emit the anti-list,
	// but without description/rationale there's nothing worth showing as a
	// positive example, so that block must be skipped.
	boards := []RecentBoard{
		{OutfitName: "Monday Mood"},
		{OutfitName: "Tuesday Bloom"},
	}
	p := buildSystemPrompt(Weather{}, boards, nil, nil, nil)

	if !strings.Contains(p, "RECENTLY WORN") {
		t.Error("expected RECENTLY WORN section for name-only boards")
	}
	if !strings.Contains(p, "Monday Mood") {
		t.Errorf("expected outfit name in avoid list; prompt:\n%s", p)
	}
	if strings.Contains(p, "RECENTLY CHOSEN") {
		t.Error("positive-example section must be skipped when there's nothing beyond a name")
	}
}

func TestBuildSystemPrompt_RichRecent_EmitsBothSections(t *testing.T) {
	boards := []RecentBoard{
		{
			OutfitName:   "Saturday Quiet",
			Description:  "Structured wool jacket sharpens the tonal trouser.",
			Rationale:    "The matte watch keeps the silhouette quiet where the wool wants to shout.",
			TopArchetype: "ruler",
			Palette:      []string{"#1A1A1A", "#D8D2C3"},
		},
		// A second board with only a name — should still show up in "avoid"
		// but be elided from "chosen".
		{OutfitName: "Name Only"},
	}
	p := buildSystemPrompt(Weather{}, boards, nil, nil, nil)

	// Anti-list includes both
	if !strings.Contains(p, "Saturday Quiet") || !strings.Contains(p, "Name Only") {
		t.Errorf("expected both names in RECENTLY WORN; prompt:\n%s", p)
	}
	// Positive list only includes the rich one
	if !strings.Contains(p, "RECENTLY CHOSEN") {
		t.Error("expected positive-example section when at least one board has description/rationale")
	}
	if !strings.Contains(p, "Structured wool jacket") {
		t.Errorf("expected description text in prompt; prompt:\n%s", p)
	}
	if !strings.Contains(p, "matte watch keeps the silhouette quiet") {
		t.Errorf("expected rationale text in prompt; prompt:\n%s", p)
	}
	if !strings.Contains(p, "top archetype at save: ruler") {
		t.Errorf("expected archetype tag in prompt; prompt:\n%s", p)
	}
	if !strings.Contains(p, "#1A1A1A") {
		t.Errorf("expected palette colors in prompt; prompt:\n%s", p)
	}

	// Make sure the "Name Only" entry does NOT appear with empty
	// description/rationale lines — guard against bloat regression.
	idx := strings.Index(p, "RECENTLY CHOSEN")
	chosenSection := p[idx:]
	if strings.Contains(chosenSection, `"Name Only"`) {
		t.Errorf("bare-name entry should be elided from RECENTLY CHOSEN; prompt:\n%s", p)
	}
}

func TestBuildSystemPrompt_EmptyOutfitNameIsSkipped(t *testing.T) {
	boards := []RecentBoard{
		{OutfitName: "", Description: "something"},
	}
	p := buildSystemPrompt(Weather{}, boards, nil, nil, nil)

	// Anti-list doesn't render empty names as "" entries.
	if strings.Contains(p, `- ""`) {
		t.Errorf("empty name should not produce a bare-quote line; prompt:\n%s", p)
	}
}
