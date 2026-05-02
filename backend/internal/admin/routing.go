package admin

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ────────────────────────────────────────────────────────────────────
// Model routing (P4-05 / mootd-admin#33).
//
// Per-tier mapping of which LLM provider serves outfit generation.
// One Mongo doc, four tier keys (free, paid, founder, beta). Read
// once per call (with a tiny in-process cache so a normal request
// burst doesn't fan out into DB reads).
//
// Architecture trade-off worth noting up front: this issue ships
// the *config + admin UI + reader interface*. Hooking it up to
// the actual generator selection in outfit/ requires holding all
// configured providers as a map at boot (instead of collapsing
// them into a single CascadeGenerator) — that wiring lives in
// app/ alongside the existing cascade builder.
//
// The tier-source side of the equation (where does a user's tier
// come from?) is the obvious next-step. Today nothing populates
// `users.tier`, so every user resolves to "free" — the routing
// table is read but the same provider is selected for everyone
// until a future ticket adds tier authoring. That's still
// valuable because:
//   1. It makes the routing surface real (admins can edit)
//   2. The "free" tier provider is now configurable without redeploy
//   3. When tier authoring lands, this surface immediately
//      becomes per-user-effective.
// ────────────────────────────────────────────────────────────────────

const modelRoutingCollection = "model_routing"

// modelRoutingDocID is the singleton key under which the routing
// doc lives. One row, edited in place — same pattern as the
// "global feature flags" doc we'd reach for if we had one.
const modelRoutingDocID = "model_routing_v1"

// Valid tiers. Hard-coded rather than dynamic because the issue's
// scope explicitly enumerates them, and the routing UI's dropdown
// is built from this list.
var validTiers = []string{"free", "paid", "founder", "beta"}

// ModelRoutingTier mirrors the wire shape.
type ModelRoutingTier struct {
	Tier     string `json:"tier"             bson:"tier"`
	Provider string `json:"provider"         bson:"provider"`
	Notes    string `json:"notes,omitempty"  bson:"notes,omitempty"`
}

// ModelRouting is the per-tier routing config.
type ModelRouting struct {
	Tiers     []ModelRoutingTier `json:"tiers"               bson:"tiers"`
	Providers []string           `json:"providers"           bson:"-"` // boot-time list, never persisted
	UpdatedBy string             `json:"updatedBy,omitempty" bson:"updatedBy,omitempty"`
	UpdatedAt *time.Time         `json:"updatedAt,omitempty" bson:"updatedAt,omitempty"`
	Notes     string             `json:"notes,omitempty"     bson:"notes,omitempty"`
}

// RoutingRepository owns the model_routing doc.
type RoutingRepository interface {
	Get(ctx context.Context) (*ModelRouting, error)
	Replace(ctx context.Context, r ModelRouting) error
}

// RoutingMongoRepository implements RoutingRepository.
type RoutingMongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewRoutingMongoRepository ensures the seeded defaults exist on
// first boot. Defaults: free→ollama, everyone else→anthropic.
// The presence of the doc isn't checked first — InsertOne with
// duplicate-key errors swallowed is cheaper than a Find probe.
func NewRoutingMongoRepository(ctx context.Context, client *mongo.Client, dbName string) (*RoutingMongoRepository, error) {
	r := &RoutingMongoRepository{client: client, dbName: dbName}

	// Seed defaults idempotently. ReplaceOne with upsert and a
	// $setOnInsert payload is the cleanest way: existing rows
	// untouched, missing rows seeded.
	now := time.Now().UTC()
	defaults := ModelRouting{
		Tiers: []ModelRoutingTier{
			{Tier: "free", Provider: "ollama", Notes: "Default — free tier ships on local model"},
			{Tier: "paid", Provider: "anthropic", Notes: "Default — paid tier on Anthropic Sonnet"},
			{Tier: "founder", Provider: "anthropic", Notes: "Default — founder tier on Anthropic Sonnet (Opus when available)"},
			{Tier: "beta", Provider: "anthropic", Notes: "Default — beta tier mirrors paid"},
		},
		UpdatedAt: &now,
		Notes:     "Seeded defaults at first boot",
	}
	doc := bson.M{
		"_id":   modelRoutingDocID,
		"tiers": defaults.Tiers,
	}
	_, err := r.col().UpdateOne(ctx,
		bson.M{"_id": modelRoutingDocID},
		bson.M{
			"$setOnInsert": doc,
			"$set":         bson.M{"updatedAtSeed": now},
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return nil, fmt.Errorf("seed model_routing: %w", err)
	}
	return r, nil
}

func (r *RoutingMongoRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection(modelRoutingCollection)
}

// Get reads the routing doc. Returns the seeded defaults if
// somehow the doc is missing (defensive — shouldn't happen post
// NewRoutingMongoRepository).
func (r *RoutingMongoRepository) Get(ctx context.Context) (*ModelRouting, error) {
	var doc ModelRouting
	err := r.col().FindOne(ctx, bson.M{"_id": modelRoutingDocID}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Defensive fallback. Shouldn't normally fire because
			// NewRoutingMongoRepository seeds on init.
			return &ModelRouting{
				Tiers: []ModelRoutingTier{
					{Tier: "free", Provider: "ollama"},
					{Tier: "paid", Provider: "anthropic"},
					{Tier: "founder", Provider: "anthropic"},
					{Tier: "beta", Provider: "anthropic"},
				},
			}, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (r *RoutingMongoRepository) Replace(ctx context.Context, r2 ModelRouting) error {
	if len(r2.Tiers) == 0 {
		return errors.New("admin: routing tiers required")
	}
	now := time.Now().UTC()
	r2.UpdatedAt = &now
	_, err := r.col().UpdateOne(ctx,
		bson.M{"_id": modelRoutingDocID},
		bson.M{
			"$set": bson.M{
				"tiers":     r2.Tiers,
				"updatedBy": r2.UpdatedBy,
				"updatedAt": r2.UpdatedAt,
				"notes":     r2.Notes,
			},
		},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

// ────────────────────────────────────────────────────────────────────
// Cached reader for the outfit-generation hot path.
// ────────────────────────────────────────────────────────────────────

// CachedRoutingReader wraps a RoutingRepository with a 30-second
// in-process cache. Outfit generation runs at human cadence (a few
// per second at most) — caching for 30s keeps the per-call DB
// hits zero in steady state without making admin edits feel
// "stuck" (the next call after the TTL picks up the new mapping).
type CachedRoutingReader struct {
	repo RoutingRepository
	mu   sync.RWMutex
	tier map[string]string
	exp  time.Time
	ttl  time.Duration
}

// NewCachedRoutingReader constructs the cache wrapper. ttl=0 uses
// the default 30s.
func NewCachedRoutingReader(repo RoutingRepository, ttl time.Duration) *CachedRoutingReader {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &CachedRoutingReader{repo: repo, ttl: ttl}
}

// ProviderForTier returns the configured provider name for a
// tier, or "" when the tier isn't in the table (caller should
// then fall back to the default cascade).
func (c *CachedRoutingReader) ProviderForTier(ctx context.Context, tier string) (string, error) {
	if c == nil || c.repo == nil {
		return "", nil
	}
	c.mu.RLock()
	if c.tier != nil && time.Now().Before(c.exp) {
		v := c.tier[tier]
		c.mu.RUnlock()
		return v, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	// Re-check after the upgrade — another goroutine may have
	// refreshed while we were waiting for the write lock.
	if c.tier != nil && time.Now().Before(c.exp) {
		return c.tier[tier], nil
	}
	doc, err := c.repo.Get(ctx)
	if err != nil {
		return "", err
	}
	m := make(map[string]string, len(doc.Tiers))
	for _, t := range doc.Tiers {
		m[t.Tier] = t.Provider
	}
	c.tier = m
	c.exp = time.Now().Add(c.ttl)
	return m[tier], nil
}

// Invalidate clears the cache. Called by the PUT handler so the
// admin sees their edit reflected on the next request without
// waiting for the TTL.
func (c *CachedRoutingReader) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tier = nil
	c.exp = time.Time{}
}

// ────────────────────────────────────────────────────────────────────
// Validation helpers used by the PUT handler.
// ────────────────────────────────────────────────────────────────────

// ValidateRoutingUpdate checks that the proposed tiers cover all
// known tiers exactly once and that every provider name is in the
// allow-list. Returns a user-facing error string on failure.
func ValidateRoutingUpdate(tiers []ModelRoutingTier, allowedProviders []string) error {
	if len(tiers) != len(validTiers) {
		return fmt.Errorf("must include all %d tiers (got %d)", len(validTiers), len(tiers))
	}
	seen := map[string]bool{}
	for _, vt := range validTiers {
		seen[vt] = false
	}
	allowed := map[string]bool{}
	for _, p := range allowedProviders {
		allowed[p] = true
	}
	for _, t := range tiers {
		if _, ok := seen[t.Tier]; !ok {
			return fmt.Errorf("unknown tier %q", t.Tier)
		}
		if seen[t.Tier] {
			return fmt.Errorf("duplicate tier %q", t.Tier)
		}
		seen[t.Tier] = true
		if t.Provider == "" {
			return fmt.Errorf("tier %q has empty provider", t.Tier)
		}
		if !allowed[t.Provider] {
			return fmt.Errorf("tier %q: unknown provider %q (available: %v)", t.Tier, t.Provider, allowedProviders)
		}
	}
	return nil
}

// ValidTiers returns the canonical list. Exposed for the handler
// (provider list) and tests.
func ValidTiers() []string { return append([]string{}, validTiers...) }
