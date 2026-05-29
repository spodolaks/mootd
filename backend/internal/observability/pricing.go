// Package observability owns the LLM-call ledger + per-call cost
// computation. It is consumed by the outfit service to wrap every
// LLM round-trip and by the admin panel to surface costs per user /
// model / feature.
//
// Cost is computed at ingest using the model_prices collection, then
// stored on the llm_calls row. This freezes yesterday's USD figure
// at yesterday's prices — when Anthropic drops Sonnet from $3 to $2
// next quarter, last month's reports stay accurate.
package observability

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ModelPrice is one row in the model_prices collection. Versioned by
// EffectiveFrom so dropping a price tomorrow doesn't rewrite history.
type ModelPrice struct {
	ID                   string     `bson:"_id"`
	Model                string     `bson:"model"`
	Provider             string     `bson:"provider"`
	InputUsdPerMTok      float64    `bson:"inputUsdPerMTok"`
	OutputUsdPerMTok     float64    `bson:"outputUsdPerMTok"`
	CacheWriteUsdPerMTok float64    `bson:"cacheWriteUsdPerMTok"`
	CacheReadUsdPerMTok  float64    `bson:"cacheReadUsdPerMTok"`
	EffectiveFrom        time.Time  `bson:"effectiveFrom"`
	EffectiveUntil       *time.Time `bson:"effectiveUntil,omitempty"`
	Notes                string     `bson:"notes,omitempty"`
}

// PriceTable is the in-memory cache of effective prices. Loaded from
// Mongo at startup + refreshed periodically; per-call lookups never
// hit the DB, so the hot path stays µs-fast.
type PriceTable struct {
	mu     sync.RWMutex
	prices map[string]ModelPrice // key = model id; only the currently-effective row
	logger *log.Logger
	repo   PriceRepository
}

// PriceRepository is the persistence contract for the price table.
type PriceRepository interface {
	ListEffective(ctx context.Context, at time.Time) ([]ModelPrice, error)
	Upsert(ctx context.Context, p ModelPrice) error
}

// NewPriceTable constructs a PriceTable, loads the current effective
// rows synchronously, and starts a background refresher.
func NewPriceTable(ctx context.Context, repo PriceRepository, logger *log.Logger) (*PriceTable, error) {
	pt := &PriceTable{
		prices: map[string]ModelPrice{},
		logger: logger,
		repo:   repo,
	}
	if err := pt.Refresh(ctx); err != nil {
		return nil, err
	}
	return pt, nil
}

// Refresh re-reads the effective price rows. Cheap (≤20 rows) so we
// can run it on every minute via a goroutine without ceremony.
func (pt *PriceTable) Refresh(ctx context.Context) error {
	rows, err := pt.repo.ListEffective(ctx, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("price table refresh: %w", err)
	}
	next := make(map[string]ModelPrice, len(rows))
	for _, r := range rows {
		// In case two rows match (overlapping effective windows for
		// the same model), the row with the later EffectiveFrom wins.
		if existing, ok := next[r.Model]; ok && existing.EffectiveFrom.After(r.EffectiveFrom) {
			continue
		}
		next[r.Model] = r
	}
	// An empty result is never legitimate: SeedDefaults always runs
	// before the first Refresh, so there are always ≥8 effective rows.
	// A zero-row read therefore means a transient failure (partial
	// read, clock skew, a misconfigured effectiveUntil window) — never
	// "prices were intentionally deleted". Swapping in an empty map
	// would make ComputeCost return ErrUnpricedModel for every model,
	// silently logging every LLM call at $0 AND disabling the budget
	// gate. Refuse the swap and keep the last-known-good table; at
	// startup this surfaces as a hard error so we fail loud instead of
	// booting with no prices.
	if len(next) == 0 {
		return fmt.Errorf("price table refresh: ListEffective returned 0 rows; keeping previous table")
	}
	pt.mu.Lock()
	pt.prices = next
	pt.mu.Unlock()
	return nil
}

// StartAutoRefresh kicks off a background loop that calls Refresh on
// the given interval. Returns immediately; cancellation via ctx.
func (pt *PriceTable) StartAutoRefresh(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := pt.Refresh(ctx); err != nil {
					pt.logger.Printf("price table refresh failed: %v", err)
				}
			}
		}
	}()
}

// ComputeCost returns the USD cost of one LLM call given its token
// counts. Uses the cached effective price for the model. When no
// price row exists for the model, returns (0, ErrUnpricedModel) so
// the caller can decide to log + skip vs fail loud.
//
// Contract: inputTok is the UNCACHED prompt portion only — already
// excluding both cacheReadTok and cacheWriteTok. The four buckets are
// disjoint and the total billable token count is their sum.
//
// Generators are responsible for normalising provider-native shapes:
//   - Anthropic returns input_tokens already-uncached + the two cache
//     buckets separately, so the Claude generator stamps Usage
//     directly from the response.
//   - OpenAI returns prompt_tokens INCLUDING cached_tokens, so the
//     OpenAI generator subtracts cached_tokens from prompt_tokens
//     before stamping Usage.InputTokens.
//
// This keeps the cost formula uniform — one function, no provider
// branches.
//
//	cost = inputTok        * inputPerMTok       / 1e6
//	     + outputTok       * outputPerMTok      / 1e6
//	     + cacheWriteTok   * cacheWritePerMTok  / 1e6
//	     + cacheReadTok    * cacheReadPerMTok   / 1e6
func (pt *PriceTable) ComputeCost(model string, inputTok, outputTok, cacheReadTok, cacheWriteTok int) (float64, error) {
	pt.mu.RLock()
	p, ok := pt.prices[model]
	pt.mu.RUnlock()
	if !ok {
		return 0, ErrUnpricedModel
	}

	const perMTok = 1_000_000.0
	cost := float64(inputTok)*p.InputUsdPerMTok/perMTok +
		float64(outputTok)*p.OutputUsdPerMTok/perMTok +
		float64(cacheWriteTok)*p.CacheWriteUsdPerMTok/perMTok +
		float64(cacheReadTok)*p.CacheReadUsdPerMTok/perMTok
	return cost, nil
}

// ErrUnpricedModel is returned when ComputeCost is asked about a
// model that isn't in the table. Callers in dev should log + treat
// as zero cost; production wiring fails loud at startup so we never
// silently drop dollars on the floor.
var ErrUnpricedModel = errors.New("observability: model not in price table")

// ── Mongo-backed PriceRepository ────────────────────────────────────

// MongoPriceRepository implements PriceRepository against the same
// Mongo cluster the rest of the app uses.
type MongoPriceRepository struct {
	client *mongo.Client
	dbName string
}

// NewMongoPriceRepository constructs the repo and ensures the indexes
// the queries below rely on.
func NewMongoPriceRepository(ctx context.Context, client *mongo.Client, dbName string) (*MongoPriceRepository, error) {
	r := &MongoPriceRepository{client: client, dbName: dbName}
	if _, err := r.col().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "model", Value: 1}, {Key: "effectiveFrom", Value: -1}},
	}); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *MongoPriceRepository) col() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("model_prices")
}

// ListEffective returns the price rows whose effectiveFrom ≤ at and
// (effectiveUntil missing OR > at). The caller (PriceTable.Refresh)
// dedupes when multiple rows match the same model.
func (r *MongoPriceRepository) ListEffective(ctx context.Context, at time.Time) ([]ModelPrice, error) {
	cur, err := r.col().Find(ctx, bson.M{
		"effectiveFrom": bson.M{"$lte": at},
		"$or": []bson.M{
			{"effectiveUntil": bson.M{"$exists": false}},
			{"effectiveUntil": bson.M{"$gt": at}},
			{"effectiveUntil": nil},
		},
	}, options.Find().SetSort(bson.D{{Key: "effectiveFrom", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var rows []ModelPrice
	if err := cur.All(ctx, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// Upsert is used by the seed routine + the (eventual) admin panel UI
// for editing prices. We never mutate an existing row — supersede
// with a new row whose effectiveFrom is later. Mongo's _id uniqueness
// is on the (model, effectiveFrom) tuple via the seed convention.
func (r *MongoPriceRepository) Upsert(ctx context.Context, p ModelPrice) error {
	if p.ID == "" {
		p.ID = fmt.Sprintf("%s|%d", p.Model, p.EffectiveFrom.Unix())
	}
	_, err := r.col().UpdateOne(ctx,
		bson.M{"_id": p.ID},
		bson.M{"$set": p},
		options.UpdateOne().SetUpsert(true),
	)
	return err
}

// SeedDefaults inserts the 2026-04 baseline price rows. Idempotent —
// Upsert by deterministic _id, so calling on every startup is safe
// and a hand-edited row in the DB won't be overwritten unless its
// _id collides with a default.
func SeedDefaults(ctx context.Context, repo PriceRepository, logger *log.Logger) error {
	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	defaults := []ModelPrice{
		{Model: "claude-sonnet-4-20250514", Provider: "anthropic", InputUsdPerMTok: 3.00, OutputUsdPerMTok: 15.00, CacheWriteUsdPerMTok: 3.75, CacheReadUsdPerMTok: 0.30, EffectiveFrom: from, Notes: "2026-04 baseline"},
		{Model: "claude-opus-4-20250514", Provider: "anthropic", InputUsdPerMTok: 5.00, OutputUsdPerMTok: 25.00, CacheWriteUsdPerMTok: 6.25, CacheReadUsdPerMTok: 0.50, EffectiveFrom: from, Notes: "2026-04 baseline"},
		{Model: "claude-haiku-4-20250514", Provider: "anthropic", InputUsdPerMTok: 1.00, OutputUsdPerMTok: 5.00, CacheWriteUsdPerMTok: 1.25, CacheReadUsdPerMTok: 0.10, EffectiveFrom: from, Notes: "2026-04 baseline"},
		{Model: "claude-sonnet-4-5", Provider: "anthropic", InputUsdPerMTok: 3.00, OutputUsdPerMTok: 15.00, CacheWriteUsdPerMTok: 3.75, CacheReadUsdPerMTok: 0.30, EffectiveFrom: from, Notes: "alias used by ANTHROPIC_MODEL default"},
		{Model: "gpt-4o", Provider: "openai", InputUsdPerMTok: 2.50, OutputUsdPerMTok: 10.00, CacheReadUsdPerMTok: 1.25, EffectiveFrom: from, Notes: "2026-04 baseline; cache_read uses prompt_tokens_details.cached_tokens"},
		{Model: "gpt-4o-mini", Provider: "openai", InputUsdPerMTok: 0.15, OutputUsdPerMTok: 0.60, CacheReadUsdPerMTok: 0.075, EffectiveFrom: from, Notes: "2026-04 baseline"},
		// gpt-image-1 — used by the detection service for per-item
		// image generation (returned in stats.openai_images). OpenAI
		// publishes a tiered model:
		//   text input  : $5/Mtok
		//   image input : $10/Mtok
		//   image output: $40/Mtok
		// Detection's input is mostly the original photo (image
		// tokens) so $10 is a fair approximation; output is always
		// image tokens at $40. A future refinement can split based
		// on prompt_tokens_details if the API exposes it.
		{Model: "gpt-image-1", Provider: "openai", InputUsdPerMTok: 10.00, OutputUsdPerMTok: 40.00, EffectiveFrom: from, Notes: "2026-04 baseline; approx — see seed comment"},
		// Ollama: free. Stored so the price table still has a row for
		// the active model when OUTFIT_PROVIDER=ollama; ComputeCost
		// returns 0 for it cleanly.
		{Model: "qwen3:14b", Provider: "ollama", EffectiveFrom: from, Notes: "local — free"},
	}
	for _, p := range defaults {
		if err := repo.Upsert(ctx, p); err != nil {
			return fmt.Errorf("seed price %q: %w", p.Model, err)
		}
	}
	logger.Printf("observability: seeded %d default model prices", len(defaults))
	return nil
}
