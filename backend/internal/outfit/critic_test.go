package outfit

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"testing"

	"mootd/backend/internal/archetype"
	"mootd/backend/internal/wardrobe"
)

// (sync.Mutex on the fake generator + sync import retained for
// the few tests that exercise concurrent Generate/Critique
// scheduling in future. Keep the import live with a no-op
// reference in case a refactor strips the lock by accident.)
var _ = sync.Mutex{}

// TestAnyBelowThreshold_Boundaries pins the threshold semantics:
// strictly below LowScoreThreshold triggers regenerate, equal-to
// does NOT. The critic's prompt rubric shares this boundary, so
// any drift here would desynchronise model + service behaviour.
func TestAnyBelowThreshold_Boundaries(t *testing.T) {
	tests := []struct {
		name   string
		scores []CritiqueScore
		want   bool
	}{
		{"empty list passes (no fail signal)", nil, false},
		{"all 7s pass", []CritiqueScore{{Score: 7}, {Score: 7}, {Score: 7}}, false},
		{"all 5s pass — 5 is borderline, NOT below", []CritiqueScore{{Score: 5}, {Score: 5}}, false},
		{"one 4 trips the gate", []CritiqueScore{{Score: 9}, {Score: 4}, {Score: 8}}, true},
		{"single 1 trips", []CritiqueScore{{Score: 1}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AnyBelowThreshold(tt.scores); got != tt.want {
				t.Errorf("AnyBelowThreshold(%v) = %v, want %v", tt.scores, got, tt.want)
			}
		})
	}
}

// TestFormatScores covers the log-line renderer: it stays
// readable when scores are present, doesn't panic on empty, and
// produces a single-line output suitable for log aggregation.
func TestFormatScores(t *testing.T) {
	got := FormatScores([]CritiqueScore{
		{OutfitName: "Edge", Score: 8},
		{OutfitName: "Soft", Score: 3},
	})
	want := "[Edge: 8 · Soft: 3]"
	if got != want {
		t.Errorf("FormatScores = %q, want %q", got, want)
	}

	if got := FormatScores(nil); got != "[no scores]" {
		t.Errorf("empty list rendering = %q, want %q", got, "[no scores]")
	}

	// Defends against accidental newline injection from a buggy
	// critic implementation — the line should stay on one line so
	// log scrapers don't lose context.
	if strings.Contains(FormatScores([]CritiqueScore{{OutfitName: "X", Score: 9}}), "\n") {
		t.Error("FormatScores should not include newlines")
	}
}

// fakeCriticGenerator stubs both Generator AND Critic so we can
// drive criticGate end-to-end without hitting the network. Note:
// criticGate only calls Generate from its own scope when it
// regenerates after a low score — the original Generate is
// upstream of the gate. So Generate-call accounting here measures
// the regen path specifically.
type fakeCriticGenerator struct {
	mu sync.Mutex

	generateCalls  []GeneratorRequest
	regenProposed  []Outfit
	regenError     error
	critiqueScores []CritiqueScore
	critiqueErr    error

	critiqueCalls int
}

func (f *fakeCriticGenerator) Name() string { return "fake-critic" }

func (f *fakeCriticGenerator) Generate(_ context.Context, req GeneratorRequest) ([]Outfit, *Usage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.generateCalls = append(f.generateCalls, req)
	if f.regenError != nil {
		return nil, nil, f.regenError
	}
	return f.regenProposed, &Usage{Provider: "fake"}, nil
}

func (f *fakeCriticGenerator) Critique(_ context.Context, _ CritiqueRequest) (CritiqueResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.critiqueCalls++
	if f.critiqueErr != nil {
		return CritiqueResult{}, f.critiqueErr
	}
	return CritiqueResult{Scores: f.critiqueScores, Usage: &Usage{Provider: "fake"}}, nil
}

// helper: build the minimal Service + items + prefIDs the gate
// reads, so each test only customises what matters for its
// assertion.
func buildGateFixture() (*fakeCriticGenerator, *Service, []wardrobe.ClothingItem, map[string]bool, []Outfit) {
	gen := &fakeCriticGenerator{}
	s := &Service{
		generator: gen,
		logger:    log.New(os.Stderr, "", 0),
	}
	items := []wardrobe.ClothingItem{
		{ID: "wi_a", Category: "tops", Label: "Top", Traits: map[string]string{"color": "red"}},
		{ID: "wi_b", Category: "bottoms", Label: "Bottom", Traits: map[string]string{"color": "blue"}},
		{ID: "wi_c", Category: "footwear", Label: "Shoes", Traits: map[string]string{"color": "white"}},
		{ID: "wi_d", Category: "accessories", Label: "Belt", Traits: map[string]string{"color": "black"}},
	}
	prefIDs := map[string]bool{"wi_a": true, "wi_b": true, "wi_c": true, "wi_d": true}
	good := []Outfit{{
		Name:        "Good A",
		Description: "passes QA",
		Items:       []string{"wi_a", "wi_b", "wi_c", "wi_d"},
	}}
	return gen, s, items, prefIDs, good
}

// TestCriticGate_AllAboveThresholdReturnsOriginal covers the
// happy path: scores all >=5 → no regenerate → original outfits
// passed through.
func TestCriticGate_AllAboveThresholdReturnsOriginal(t *testing.T) {
	gen, s, items, prefIDs, good := buildGateFixture()
	gen.critiqueScores = []CritiqueScore{{OutfitName: "Good A", Score: 8}}

	got := s.criticGate(context.Background(), "user_x", good, items, prefIDs,
		[]archetype.ScoredArchetype{{Name: "creator", Score: 0.9}}, Weather{}, nil, nil, nil)

	if len(got) != 1 || got[0].Name != "Good A" {
		t.Errorf("expected pass-through of original outfit, got %+v", got)
	}
	if gen.critiqueCalls != 1 {
		t.Errorf("expected 1 critique call, got %d", gen.critiqueCalls)
	}
	// Generate should NOT have been called for regen — only the
	// original Generate (which lives outside the gate, so 0 here).
	if len(gen.generateCalls) != 0 {
		t.Errorf("expected 0 regenerate calls when all scores pass; got %d", len(gen.generateCalls))
	}
}

// TestCriticGate_LowScoreTriggersRegenerate proves a score of 3
// fires the regen branch and the second batch wins when its
// validated count is at least the original's.
func TestCriticGate_LowScoreTriggersRegenerate(t *testing.T) {
	gen, s, items, prefIDs, good := buildGateFixture()
	gen.critiqueScores = []CritiqueScore{{OutfitName: "Good A", Score: 3, Reason: "off-archetype"}}
	// Regen returns a fresh outfit with the same items — passes
	// ValidateOutfits unchanged (top + bottom + footwear present).
	gen.regenProposed = []Outfit{{
		Name:        "Regenerated A",
		Description: "second pass",
		Items:       []string{"wi_a", "wi_b", "wi_c", "wi_d"},
	}}

	got := s.criticGate(context.Background(), "user_x", good, items, prefIDs,
		[]archetype.ScoredArchetype{{Name: "creator", Score: 0.9}}, Weather{}, nil, nil, nil)

	if len(got) != 1 {
		t.Fatalf("expected 1 regenerated outfit, got %d (%+v)", len(got), got)
	}
	if got[0].Name != "Regenerated A" {
		t.Errorf("expected regenerated outfit to win; got name=%q", got[0].Name)
	}
	if gen.critiqueCalls != 1 {
		t.Errorf("expected exactly 1 critique call, got %d", gen.critiqueCalls)
	}
	if len(gen.generateCalls) != 1 {
		t.Errorf("expected exactly 1 regen Generate call, got %d", len(gen.generateCalls))
	}
}

// TestCriticGate_RegenWithFewerOutfitsKeepsOriginal proves we
// don't accidentally degrade — if the regen Generate returns
// fewer valid outfits than the first batch, we keep the
// originals (graceful degradation, no worse than no-critic).
func TestCriticGate_RegenWithFewerOutfitsKeepsOriginal(t *testing.T) {
	gen, s, items, prefIDs, _ := buildGateFixture()
	// Original has TWO outfits.
	good := []Outfit{
		{Name: "A", Description: "ok", Items: []string{"wi_a", "wi_b", "wi_c", "wi_d"}},
		{Name: "B", Description: "ok", Items: []string{"wi_a", "wi_b", "wi_c", "wi_d"}},
	}
	gen.critiqueScores = []CritiqueScore{{OutfitName: "A", Score: 3}, {OutfitName: "B", Score: 9}}
	// Regen returns ONE outfit — fewer than original.
	gen.regenProposed = []Outfit{
		{Name: "Regen", Description: "just one", Items: []string{"wi_a", "wi_b", "wi_c", "wi_d"}},
	}

	got := s.criticGate(context.Background(), "user_x", good, items, prefIDs,
		[]archetype.ScoredArchetype{}, Weather{}, nil, nil, nil)

	if len(got) != 2 || got[0].Name != "A" || got[1].Name != "B" {
		t.Errorf("expected original 2-outfit batch when regen has fewer; got %+v", got)
	}
}

// TestCriticGate_CritiqueErrorReturnsOriginal covers the soft-
// degrade path: critic API down → don't fail the user, just keep
// the first-batch outfits unchanged.
func TestCriticGate_CritiqueErrorReturnsOriginal(t *testing.T) {
	gen, s, items, prefIDs, good := buildGateFixture()
	gen.critiqueErr = errors.New("anthropic 503")

	got := s.criticGate(context.Background(), "user_x", good, items, prefIDs,
		[]archetype.ScoredArchetype{}, Weather{}, nil, nil, nil)

	if len(got) != 1 || got[0].Name != "Good A" {
		t.Errorf("expected pass-through on critic error; got %+v", got)
	}
	if len(gen.generateCalls) != 0 {
		t.Errorf("regen should NOT fire on critic error; got %d generate calls", len(gen.generateCalls))
	}
}

// TestCriticGate_RegenErrorReturnsOriginal — critic fired,
// caught a low score, kicked off a regenerate, but the regen
// itself errored. We keep the first batch (still better than
// nothing).
func TestCriticGate_RegenErrorReturnsOriginal(t *testing.T) {
	gen, s, items, prefIDs, good := buildGateFixture()
	gen.critiqueScores = []CritiqueScore{{OutfitName: "Good A", Score: 2}}
	gen.regenError = errors.New("anthropic 500")

	got := s.criticGate(context.Background(), "user_x", good, items, prefIDs,
		[]archetype.ScoredArchetype{}, Weather{}, nil, nil, nil)

	if len(got) != 1 || got[0].Name != "Good A" {
		t.Errorf("expected pass-through on regen error; got %+v", got)
	}
}

// TestCriticGate_NonCriticGeneratorIsNoop proves the type-assert
// gate: when the underlying generator doesn't implement Critic,
// the gate returns the original outfits without reaching for
// Critique at all (no panic, no log spam, no regen).
func TestCriticGate_NonCriticGeneratorIsNoop(t *testing.T) {
	gen := &generatorOnly{}
	s := &Service{generator: gen, logger: log.New(os.Stderr, "", 0)}
	good := []Outfit{{Name: "X", Items: []string{"wi_a"}}}

	got := s.criticGate(context.Background(), "user_x", good, nil, nil,
		[]archetype.ScoredArchetype{}, Weather{}, nil, nil, nil)

	if len(got) != 1 || got[0].Name != "X" {
		t.Errorf("expected pass-through when generator lacks Critic; got %+v", got)
	}
	if gen.calls != 0 {
		t.Errorf("non-critic generator should not see ANY calls in gate; got %d", gen.calls)
	}
}

// generatorOnly satisfies Generator but NOT Critic — a fresh
// plain struct so the type-assert in criticGate falls through.
type generatorOnly struct{ calls int }

func (g *generatorOnly) Name() string { return "plain" }
func (g *generatorOnly) Generate(_ context.Context, _ GeneratorRequest) ([]Outfit, *Usage, error) {
	g.calls++
	return nil, nil, nil
}
