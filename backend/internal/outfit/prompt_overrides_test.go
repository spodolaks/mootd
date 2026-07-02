package outfit

import (
	"strings"
	"testing"

	"mootd/backend/internal/archetype"
)

// Overrides are how the admin eval runner tests a draft template
// version before promotion: the supplied body must win over both
// the provider (production version / A/B candidate) and the
// hardcoded fallback, for exactly the named block and nothing else.

func TestOverridesBeatProviderAndFallback(t *testing.T) {
	SetPromptTemplateProvider(fakeProvider{
		"outfit_system_base": "PROVIDER BASE",
		"outfit_safety":      "PROVIDER SAFETY {{userDataOpen}} {{userDataClose}}",
	})
	defer SetPromptTemplateProvider(nil)

	got := buildSystemPromptWithOverrides(map[string]string{
		"outfit_system_base": "OVERRIDE BASE",
	}, "user-1", Weather{}, nil, []archetype.ScoredArchetype{}, nil, nil)

	if !strings.Contains(got, "OVERRIDE BASE") {
		t.Fatalf("override body missing from prompt:\n%s", got[:min(200, len(got))])
	}
	if strings.Contains(got, "PROVIDER BASE") {
		t.Fatalf("provider base should have been overridden")
	}
	// Non-overridden blocks still resolve through the provider.
	if !strings.Contains(got, "PROVIDER SAFETY") {
		t.Fatalf("non-overridden safety block should come from the provider")
	}
}

func TestArchetypeSpecificOverrideWinsWithoutRoutingFlag(t *testing.T) {
	// Evaling a draft "outfit_system_base.<archetype>" must not
	// require flipping the global OUTFIT_PER_ARCHETYPE_PROMPTS flag —
	// the override applies whenever the tuple's top archetype
	// matches the suffixed name.
	SetPromptTemplateProvider(fakeProvider{
		"outfit_system_base": "PROVIDER BASE",
	})
	defer SetPromptTemplateProvider(nil)
	SetPerArchetypeRoutingEnabled(false)

	got := buildSystemPromptWithOverrides(map[string]string{
		"outfit_system_base.creator": "CREATOR DRAFT BASE",
	}, "", Weather{}, nil, []archetype.ScoredArchetype{{Name: "creator", Score: 0.9}}, nil, nil)

	if !strings.Contains(got, "CREATOR DRAFT BASE") {
		t.Fatalf("archetype-specific override should render for a creator-topped tuple")
	}

	// A tuple topped by a different archetype must NOT pick up the
	// creator draft — it falls through to the provider.
	got = buildSystemPromptWithOverrides(map[string]string{
		"outfit_system_base.creator": "CREATOR DRAFT BASE",
	}, "", Weather{}, nil, []archetype.ScoredArchetype{{Name: "minimalist", Score: 0.9}}, nil, nil)
	if strings.Contains(got, "CREATOR DRAFT BASE") {
		t.Fatalf("creator override leaked into a minimalist tuple")
	}
	if !strings.Contains(got, "PROVIDER BASE") {
		t.Fatalf("non-matching tuple should fall back to the provider base")
	}
}

func TestNilOverridesByteIdenticalToProductionPath(t *testing.T) {
	SetPromptTemplateProvider(fakeProvider{
		"outfit_system_base": "PROVIDER BASE",
	})
	defer SetPromptTemplateProvider(nil)

	boards := []RecentBoard{{OutfitName: "Saturday Quiet", Description: "soft layers"}}
	arch := []archetype.ScoredArchetype{}

	plain := buildSystemPrompt("user-1", Weather{Temperature: "18", Unit: "C", Condition: "clear"}, boards, arch, nil, nil)
	viaNil := buildSystemPromptWithOverrides(nil, "user-1", Weather{Temperature: "18", Unit: "C", Condition: "clear"}, boards, arch, nil, nil)
	if plain != viaNil {
		t.Fatalf("nil-overrides path must be byte-identical to buildSystemPrompt")
	}
}

func TestUserMessageOverride(t *testing.T) {
	SetPromptTemplateProvider(nil)

	items := []GenItem{{ID: "item_1", Category: "top", Label: "wool jacket", Preferred: true}}
	got := buildUserMessageWithOverrides(map[string]string{
		"outfit_user_instruction": "OVERRIDDEN INSTRUCTION",
	}, "", items)

	if !strings.Contains(got, "OVERRIDDEN INSTRUCTION") {
		t.Fatalf("user-instruction override missing:\n%s", got)
	}
	if strings.Contains(got, defaultUserInstruction) {
		t.Fatalf("default instruction should have been overridden")
	}
}
