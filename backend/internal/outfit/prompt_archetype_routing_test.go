package outfit

import (
	"strings"
	"testing"

	"mootd/backend/internal/archetype"
)

// TestPerArchetypeRouting_OffByDefault locks in the safety
// guarantee for mootd#65: with the routing flag off, the prompt
// path is byte-identical to the pre-#65 path even when
// archetype-specific templates exist in the provider. Operators
// can safely curate templates without flipping the flag.
func TestPerArchetypeRouting_OffByDefault(t *testing.T) {
	t.Cleanup(func() {
		SetPromptTemplateProvider(nil)
		SetPerArchetypeRoutingEnabled(false)
	})
	SetPromptTemplateProvider(fakeProvider{
		"outfit_system_base":         "UNIVERSAL BASE",
		"outfit_system_base.creator": "CREATOR-SPECIFIC BASE", // would be picked if routing were on
	})
	// Routing flag stays at its zero value (false).

	got := buildSystemPrompt("", Weather{}, nil, []archetype.ScoredArchetype{
		{Name: "creator", Score: 0.9},
	}, nil, nil)

	if !strings.HasPrefix(got, "UNIVERSAL BASE") {
		t.Errorf("expected universal template when routing off; got prefix %q", got[:min(40, len(got))])
	}
	if strings.Contains(got, "CREATOR-SPECIFIC BASE") {
		t.Error("archetype-specific template leaked through with routing off")
	}
}

// TestPerArchetypeRouting_PicksArchetypeTemplate covers the
// happy path: flag on + archetype-specific template present →
// archetype-specific wins.
func TestPerArchetypeRouting_PicksArchetypeTemplate(t *testing.T) {
	t.Cleanup(func() {
		SetPromptTemplateProvider(nil)
		SetPerArchetypeRoutingEnabled(false)
	})
	SetPerArchetypeRoutingEnabled(true)
	SetPromptTemplateProvider(fakeProvider{
		"outfit_system_base":         "UNIVERSAL BASE",
		"outfit_system_base.creator": "CREATOR-TUNED BASE",
	})

	got := buildSystemPrompt("", Weather{}, nil, []archetype.ScoredArchetype{
		{Name: "creator", Score: 0.9},
	}, nil, nil)

	if !strings.HasPrefix(got, "CREATOR-TUNED BASE") {
		t.Errorf("expected archetype-specific template to lead; got prefix %q", got[:min(40, len(got))])
	}
}

// TestPerArchetypeRouting_FallsBackWhenArchetypeMissing covers
// the safe-degradation path: routing on, but no archetype-
// specific template curated yet → universal template wins.
// Operators get to phase template authoring without breaking the
// hot path.
func TestPerArchetypeRouting_FallsBackWhenArchetypeMissing(t *testing.T) {
	t.Cleanup(func() {
		SetPromptTemplateProvider(nil)
		SetPerArchetypeRoutingEnabled(false)
	})
	SetPerArchetypeRoutingEnabled(true)
	SetPromptTemplateProvider(fakeProvider{
		"outfit_system_base": "UNIVERSAL BASE",
		// no outfit_system_base.rebel curated yet
	})

	got := buildSystemPrompt("", Weather{}, nil, []archetype.ScoredArchetype{
		{Name: "rebel", Score: 0.85},
	}, nil, nil)

	if !strings.HasPrefix(got, "UNIVERSAL BASE") {
		t.Errorf("expected fallback to universal when archetype-specific missing; got prefix %q", got[:min(40, len(got))])
	}
}

// TestPerArchetypeRouting_NoTopArchetype covers the cold-start
// case: a user with empty archetype scores still gets the
// universal template, no panicking on a nil index.
func TestPerArchetypeRouting_NoTopArchetype(t *testing.T) {
	t.Cleanup(func() {
		SetPromptTemplateProvider(nil)
		SetPerArchetypeRoutingEnabled(false)
	})
	SetPerArchetypeRoutingEnabled(true)
	SetPromptTemplateProvider(fakeProvider{
		"outfit_system_base":         "UNIVERSAL BASE",
		"outfit_system_base.creator": "CREATOR-TUNED BASE",
	})

	got := buildSystemPrompt("", Weather{}, nil, []archetype.ScoredArchetype{}, nil, nil)

	if !strings.HasPrefix(got, "UNIVERSAL BASE") {
		t.Errorf("expected universal when no top archetype; got prefix %q", got[:min(40, len(got))])
	}
}

// TestPerArchetypeRouting_AllArchetypesSupported sweeps every
// canonical archetype to make sure the lookup name is built
// correctly for each. Catches accidental mutations to the name
// format (e.g. dropping the dot, lowercasing wrong).
func TestPerArchetypeRouting_AllArchetypesSupported(t *testing.T) {
	t.Cleanup(func() {
		SetPromptTemplateProvider(nil)
		SetPerArchetypeRoutingEnabled(false)
	})
	SetPerArchetypeRoutingEnabled(true)

	for arche := range archetype.Profiles {
		t.Run(arche, func(t *testing.T) {
			expected := "TEMPLATE FOR " + arche
			SetPromptTemplateProvider(fakeProvider{
				"outfit_system_base":          "UNIVERSAL BASE",
				"outfit_system_base." + arche: expected,
			})
			got := buildSystemPrompt("", Weather{}, nil, []archetype.ScoredArchetype{
				{Name: arche, Score: 0.9},
			}, nil, nil)
			if !strings.HasPrefix(got, expected) {
				t.Errorf("archetype=%s: expected prefix %q, got %q", arche, expected, got[:min(60, len(got))])
			}
		})
	}
}
