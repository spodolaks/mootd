package outfit

import (
	"context"
	"log"
)

// TierRoutingGenerator picks a provider per call based on the
// caller's user tier (P4-05 / mootd-admin#33).
//
// Architecture:
//
//   - `byProvider` maps provider names ("anthropic" / "openai" /
//     "ollama") to the constructed Generator instances. Built once
//     at boot from the same env-driven chain that powers
//     CascadeGenerator.
//   - `routing` returns the configured provider name for a given
//     tier. Backed by admin.CachedRoutingReader in production —
//     30s in-process cache so per-call DB reads are zero in steady
//     state; admin PUT explicitly invalidates.
//   - `tier` resolves a user's tier. v1 returns "free" for everyone
//     because users.tier isn't authored anywhere yet. The interface
//     is in place so the next ticket (tier authoring) can plug in
//     without changing this struct's surface.
//   - `fallback` runs when the configured provider is missing from
//     the map (deployment misconfiguration), unknown, or fails
//     transiently. Production wires the existing CascadeGenerator
//     here so the routing layer degrades to the same behaviour
//     the codebase had before this issue landed.
//
// The intentional non-goal: this struct does NOT do health
// tracking or per-provider failure classification. Those concerns
// live on the cascade fallback. TierRouting is a single decision
// point at the top of the chain.
type TierRoutingGenerator struct {
	byProvider map[string]Generator
	routing    RoutingReader
	tier       TierResolver
	fallback   Generator
	logger     *log.Logger
}

// RoutingReader is the slice of admin.CachedRoutingReader the
// outfit package needs. Defined here so outfit/ doesn't import
// admin/ — same one-way-dep convention as the rest of the
// codebase. Returns "" when the tier isn't in the routing table
// (caller falls back to the cascade).
type RoutingReader interface {
	ProviderForTier(ctx context.Context, tier string) (string, error)
}

// TierResolver maps a userID to a tier. v1 production wiring
// returns "free" for everyone (since users.tier isn't yet
// authored). Tests substitute fakes.
type TierResolver interface {
	TierForUser(ctx context.Context, userID string) (string, error)
}

// FreeTierResolver always returns "free". Use this until the
// tier-authoring ticket lands (deferred from #33 close comment).
type FreeTierResolver struct{}

// TierForUser implements TierResolver.
func (FreeTierResolver) TierForUser(_ context.Context, _ string) (string, error) {
	return "free", nil
}

// NewTierRoutingGenerator wires the dependencies. `byProvider`
// must be non-empty (a routing without options to pick from is
// unusable); the caller is expected to populate it from the same
// chain that builds `fallback`.
func NewTierRoutingGenerator(
	logger *log.Logger,
	byProvider map[string]Generator,
	routing RoutingReader,
	tier TierResolver,
	fallback Generator,
) *TierRoutingGenerator {
	if logger == nil {
		logger = log.Default()
	}
	if tier == nil {
		tier = FreeTierResolver{}
	}
	return &TierRoutingGenerator{
		byProvider: byProvider,
		routing:    routing,
		tier:       tier,
		fallback:   fallback,
		logger:     logger,
	}
}

// Name reports the wrapper for logging. Per-call provider lands
// on Usage.Provider via the underlying generator, so admin pages
// can still slice by real provider.
func (t *TierRoutingGenerator) Name() string {
	if t.fallback == nil {
		return "tier-routing"
	}
	return "tier-routing(fallback=" + t.fallback.Name() + ")"
}

// Generate picks a provider per the routing config + user tier,
// falling back to the configured fallback cascade on any error
// (including missing config). Errors are logged at the routing
// layer so admins can see which decisions were made; fallback
// invocations are also logged so a misconfigured routing entry
// is visible without enabling debug logs.
func (t *TierRoutingGenerator) Generate(ctx context.Context, req GeneratorRequest) ([]Outfit, *Usage, error) {
	chosen := t.pickProvider(ctx, req.UserID)
	if chosen == "" {
		// No tier-specific provider configured. Use the fallback
		// without logging — this is the "graceful degradation"
		// path when routing is unwired entirely.
		return t.fallback.Generate(ctx, req)
	}

	gen, ok := t.byProvider[chosen]
	if !ok {
		t.logger.Printf("outfit: tier routing chose %q but no such provider built; falling back to %s", chosen, t.fallback.Name())
		return t.fallback.Generate(ctx, req)
	}

	outfits, usage, err := gen.Generate(ctx, req)
	if err != nil {
		t.logger.Printf("outfit: tier routing %q failed (%v); falling back to %s", chosen, err, t.fallback.Name())
		return t.fallback.Generate(ctx, req)
	}
	return outfits, usage, nil
}

// pickProvider runs the resolve-tier-then-look-up step. Errors
// here (Mongo blip on reading the routing config, tier resolver
// failure) translate to "" — caller uses the fallback.
func (t *TierRoutingGenerator) pickProvider(ctx context.Context, userID string) string {
	tierStr, err := t.tier.TierForUser(ctx, userID)
	if err != nil || tierStr == "" {
		return ""
	}
	provider, err := t.routing.ProviderForTier(ctx, tierStr)
	if err != nil {
		return ""
	}
	return provider
}
