package outfit

import (
	"context"
	"errors"
	"testing"
)

// (`fakeGen` is shared from cascade_test.go.)
//
// Helpers for tier-routing tests:
func okGen(name string) *fakeGen {
	return &fakeGen{
		name: name,
		responses: []fakeResp{{
			outfits: []Outfit{{Name: "ok-from-" + name}},
			usage:   &Usage{Provider: name},
		}},
	}
}

func failingGen(name string, err error) *fakeGen {
	return &fakeGen{
		name:      name,
		responses: []fakeResp{{err: err}},
	}
}

type fakeRouting struct {
	tier2Provider map[string]string
	err           error
}

func (f *fakeRouting) ProviderForTier(_ context.Context, tier string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.tier2Provider[tier], nil
}

type fakeTierResolver struct {
	t   string
	err error
}

func (f fakeTierResolver) TierForUser(_ context.Context, _ string) (string, error) {
	return f.t, f.err
}

func TestTierRouting_PicksConfiguredProvider(t *testing.T) {
	anth := okGen("anthropic")
	ollama := okGen("ollama")
	fb := okGen("cascade")

	r := NewTierRoutingGenerator(nil,
		map[string]Generator{"anthropic": anth, "ollama": ollama},
		&fakeRouting{tier2Provider: map[string]string{"free": "ollama", "paid": "anthropic"}},
		fakeTierResolver{t: "paid"},
		fb,
	)
	outfits, _, err := r.Generate(context.Background(), GeneratorRequest{UserID: "u1"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if anth.calls != 1 || ollama.calls != 0 || fb.calls != 0 {
		t.Fatalf("expected anthropic=1 (paid), ollama=0, fb=0, got %d/%d/%d", anth.calls, ollama.calls, fb.calls)
	}
	if len(outfits) != 1 || outfits[0].Name != "ok-from-anthropic" {
		t.Fatalf("expected paid → anthropic outfit, got %+v", outfits)
	}
}

func TestTierRouting_FallsBackOnError(t *testing.T) {
	anth := failingGen("anthropic", errors.New("boom"))
	fb := okGen("cascade")
	r := NewTierRoutingGenerator(nil,
		map[string]Generator{"anthropic": anth},
		&fakeRouting{tier2Provider: map[string]string{"free": "anthropic"}},
		fakeTierResolver{t: "free"},
		fb,
	)
	outfits, _, err := r.Generate(context.Background(), GeneratorRequest{UserID: "u1"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if fb.calls != 1 || anth.calls != 1 {
		t.Fatalf("expected primary attempt + fallback (anth=1, fb=1), got %d/%d", anth.calls, fb.calls)
	}
	if outfits[0].Name != "ok-from-cascade" {
		t.Fatalf("expected fallback's outfit, got %+v", outfits)
	}
}

func TestTierRouting_FallbackPreservesBilledPrimaryUsage(t *testing.T) {
	// Primary billed tokens then failed; the fallback is a free
	// provider (nil Usage). The failed primary's billed Usage must be
	// surfaced so the cost still reaches the ledger.
	billed := &Usage{Provider: "anthropic", Model: "claude-x", InputTokens: 800, OutputTokens: 40}
	anth := &fakeGen{name: "anthropic", responses: []fakeResp{{usage: billed, err: errors.New("503")}}}
	// Free fallback: outfits but no Usage.
	freeFb := &fakeGen{name: "ollama", responses: []fakeResp{{outfits: []Outfit{{Name: "free-pick"}}}}}

	r := NewTierRoutingGenerator(nil,
		map[string]Generator{"anthropic": anth},
		&fakeRouting{tier2Provider: map[string]string{"free": "anthropic"}},
		fakeTierResolver{t: "free"},
		freeFb,
	)
	outfits, usage, err := r.Generate(context.Background(), GeneratorRequest{UserID: "u1"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(outfits) != 1 || outfits[0].Name != "free-pick" {
		t.Fatalf("expected fallback's outfit, got %+v", outfits)
	}
	if usage == nil || usage.Provider != "anthropic" || usage.InputTokens != 800 {
		t.Fatalf("expected the failed primary's billed usage to be preserved, got %+v", usage)
	}
}

func TestTierRouting_FallsBackWhenProviderMissing(t *testing.T) {
	fb := okGen("cascade")
	r := NewTierRoutingGenerator(nil,
		map[string]Generator{}, // no providers built
		&fakeRouting{tier2Provider: map[string]string{"free": "anthropic"}},
		fakeTierResolver{t: "free"},
		fb,
	)
	_, _, err := r.Generate(context.Background(), GeneratorRequest{UserID: "u1"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if fb.calls != 1 {
		t.Fatalf("expected fallback to fire when configured provider isn't built, got %d", fb.calls)
	}
}

func TestTierRouting_FallsBackOnRoutingError(t *testing.T) {
	fb := okGen("cascade")
	r := NewTierRoutingGenerator(nil,
		map[string]Generator{"anthropic": okGen("anthropic")},
		&fakeRouting{err: errors.New("mongo down")},
		fakeTierResolver{t: "free"},
		fb,
	)
	_, _, err := r.Generate(context.Background(), GeneratorRequest{UserID: "u1"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if fb.calls != 1 {
		t.Fatalf("expected fallback when routing errors, got %d", fb.calls)
	}
}

func TestTierRouting_FreeTierResolver_AllUsersAreFree(t *testing.T) {
	tier, err := FreeTierResolver{}.TierForUser(context.Background(), "any-user")
	if err != nil {
		t.Fatal(err)
	}
	if tier != "free" {
		t.Fatalf("expected free, got %q", tier)
	}
}
