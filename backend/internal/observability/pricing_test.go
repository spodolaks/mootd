package observability

import (
	"context"
	"io"
	"log"
	"math"
	"testing"
	"time"
)

// memPriceRepo is an in-memory PriceRepository for unit tests.
type memPriceRepo struct {
	rows []ModelPrice
}

func (m *memPriceRepo) ListEffective(ctx context.Context, at time.Time) ([]ModelPrice, error) {
	out := make([]ModelPrice, 0, len(m.rows))
	for _, r := range m.rows {
		if r.EffectiveFrom.After(at) {
			continue
		}
		if r.EffectiveUntil != nil && !r.EffectiveUntil.After(at) {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

func (m *memPriceRepo) Upsert(ctx context.Context, p ModelPrice) error {
	for i := range m.rows {
		if m.rows[i].ID == p.ID {
			m.rows[i] = p
			return nil
		}
	}
	m.rows = append(m.rows, p)
	return nil
}

func newTable(t *testing.T, rows ...ModelPrice) *PriceTable {
	t.Helper()
	repo := &memPriceRepo{rows: rows}
	pt, err := NewPriceTable(context.Background(), repo, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewPriceTable: %v", err)
	}
	return pt
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestComputeCost_AnthropicPricing(t *testing.T) {
	// Sonnet rates from the seeded defaults.
	pt := newTable(t, ModelPrice{
		ID: "sonnet|2026-04-01", Model: "claude-sonnet-4-20250514", Provider: "anthropic",
		InputUsdPerMTok: 3, OutputUsdPerMTok: 15,
		CacheWriteUsdPerMTok: 3.75, CacheReadUsdPerMTok: 0.30,
		EffectiveFrom: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	})

	// 5000 input (Anthropic semantics: input EXCLUDES cache_read +
	// cache_write), 1000 output, no cache:
	// = 5000 * 3 / 1e6 + 1000 * 15 / 1e6 = 0.015 + 0.015 = 0.030
	cost, err := pt.ComputeCost("claude-sonnet-4-20250514", 5000, 1000, 0, 0)
	if err != nil {
		t.Fatalf("ComputeCost: %v", err)
	}
	if !almostEqual(cost, 0.030) {
		t.Errorf("simple sonnet: got %.6f, want 0.030000", cost)
	}

	// Cached prompt (worst case — first call writes the cache):
	// 5000 fresh input + 2000 cache_write + 0 cache_read, 1000 output
	// = 5000 * 3/1e6 + 1000 * 15/1e6 + 2000 * 3.75/1e6 = 0.015 + 0.015 + 0.0075 = 0.0375
	cost, err = pt.ComputeCost("claude-sonnet-4-20250514", 5000, 1000, 0, 2000)
	if err != nil {
		t.Fatalf("ComputeCost: %v", err)
	}
	if !almostEqual(cost, 0.0375) {
		t.Errorf("cache write: got %.6f, want 0.037500", cost)
	}

	// Cached prompt (re-read at 10% of input price):
	// 5000 fresh input + 0 cache_write + 2000 cache_read, 1000 output
	// = 5000 * 3/1e6 + 1000 * 15/1e6 + 2000 * 0.30/1e6 = 0.015 + 0.015 + 0.0006 = 0.0306
	cost, err = pt.ComputeCost("claude-sonnet-4-20250514", 5000, 1000, 2000, 0)
	if err != nil {
		t.Fatalf("ComputeCost: %v", err)
	}
	if !almostEqual(cost, 0.0306) {
		t.Errorf("cache read: got %.6f, want 0.030600", cost)
	}
}

func TestComputeCost_OpenAIPricing(t *testing.T) {
	// gpt-4o, with prompt-caching: prompt_tokens INCLUDES cached_tokens
	// per OpenAI's API. The generator hands us:
	//   InputTokens     = prompt_tokens  (incl. cached)
	//   CacheReadTokens = cached_tokens
	//   CacheWriteTokens = 0  (OpenAI doesn't surface a write-side rate)
	//
	// ComputeCost subtracts cache_read from input before pricing the
	// uncached portion at full rate, then prices cache_read separately
	// at the cached rate. Net:
	//   uncached = prompt_tokens - cached
	pt := newTable(t, ModelPrice{
		ID: "gpt-4o|2026-04-01", Model: "gpt-4o", Provider: "openai",
		InputUsdPerMTok: 2.50, OutputUsdPerMTok: 10.00,
		CacheReadUsdPerMTok: 1.25,
		EffectiveFrom:       time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	})

	// The OpenAI generator normalises before stamping Usage:
	//   PromptTokens=7000, cached_tokens=2000 → InputTokens=5000, CacheReadTokens=2000.
	// ComputeCost then applies the rates uniformly:
	//   = 5000 * 2.50/1e6 + 1500 * 10/1e6 + 2000 * 1.25/1e6
	//   = 0.0125 + 0.015 + 0.0025 = 0.030
	cost, err := pt.ComputeCost("gpt-4o", 5000, 1500, 2000, 0)
	if err != nil {
		t.Fatalf("ComputeCost: %v", err)
	}
	if !almostEqual(cost, 0.030) {
		t.Errorf("gpt-4o cached: got %.6f, want 0.030000", cost)
	}
}

func TestComputeCost_OllamaIsZero(t *testing.T) {
	pt := newTable(t, ModelPrice{
		ID:            "qwen3|2026-04-01",
		Model:         "qwen3:14b",
		Provider:      "ollama",
		EffectiveFrom: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	})
	cost, err := pt.ComputeCost("qwen3:14b", 999_999, 999_999, 0, 0)
	if err != nil {
		t.Fatalf("ComputeCost: %v", err)
	}
	if cost != 0 {
		t.Errorf("ollama: got %.6f, want 0", cost)
	}
}

func TestComputeCost_UnknownModelReturnsErr(t *testing.T) {
	// Table is non-empty (an empty table now fails construction), but
	// the queried model is absent.
	pt := newTable(t, ModelPrice{
		ID: "sonnet|2026-04-01", Model: "claude-sonnet-4-20250514", Provider: "anthropic",
		InputUsdPerMTok: 3, OutputUsdPerMTok: 15,
		EffectiveFrom: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	})
	cost, err := pt.ComputeCost("not-a-real-model", 100, 200, 0, 0)
	if err != ErrUnpricedModel {
		t.Errorf("err: got %v, want ErrUnpricedModel", err)
	}
	if cost != 0 {
		t.Errorf("cost: got %.6f, want 0 on err", cost)
	}
}

func TestPriceTable_RefreshPicksLatestEffectiveFrom(t *testing.T) {
	older := ModelPrice{
		ID:                   "sonnet|early",
		Model:                "claude-sonnet-4-20250514",
		InputUsdPerMTok:      3,
		OutputUsdPerMTok:     15,
		CacheWriteUsdPerMTok: 3.75,
		CacheReadUsdPerMTok:  0.30,
		EffectiveFrom:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	newer := ModelPrice{
		ID:                   "sonnet|drop",
		Model:                "claude-sonnet-4-20250514",
		InputUsdPerMTok:      2.50, // hypothetical price drop
		OutputUsdPerMTok:     12.50,
		CacheWriteUsdPerMTok: 3.125,
		CacheReadUsdPerMTok:  0.25,
		EffectiveFrom:        time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}
	pt := newTable(t, older, newer)

	// 1M tokens of input only — easy to read in the assertion.
	cost, _ := pt.ComputeCost("claude-sonnet-4-20250514", 1_000_000, 0, 0, 0)
	if !almostEqual(cost, 2.50) {
		t.Errorf("expected the newer (lower) rate to win: got $%.4f, want $2.50", cost)
	}
}

func TestPriceTable_RefreshKeepsTableOnEmptyResult(t *testing.T) {
	sonnet := ModelPrice{
		ID: "sonnet|2026-04-01", Model: "claude-sonnet-4-20250514", Provider: "anthropic",
		InputUsdPerMTok: 3, OutputUsdPerMTok: 15,
		EffectiveFrom: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
	}
	repo := &memPriceRepo{rows: []ModelPrice{sonnet}}
	pt, err := NewPriceTable(context.Background(), repo, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewPriceTable: %v", err)
	}

	// Simulate a transient zero-row read (e.g. a misconfigured
	// effectiveUntil window or partial read).
	repo.rows = nil
	if err := pt.Refresh(context.Background()); err == nil {
		t.Fatal("expected Refresh to error on a 0-row result, got nil")
	}

	// The last-known-good price must survive — NOT be blanked to a
	// $0 ErrUnpricedModel, which would silently zero all spend.
	cost, err := pt.ComputeCost("claude-sonnet-4-20250514", 1_000_000, 0, 0, 0)
	if err != nil {
		t.Fatalf("ComputeCost after empty refresh: %v (table was wrongly blanked)", err)
	}
	if !almostEqual(cost, 3.0) {
		t.Errorf("kept-table cost: got $%.4f, want $3.00", cost)
	}
}

func TestNewPriceTable_FailsLoudWhenEmpty(t *testing.T) {
	// A cold start with no seeded rows must fail at construction rather
	// than booting with an empty (all-$0) price table.
	repo := &memPriceRepo{}
	if _, err := NewPriceTable(context.Background(), repo, log.New(io.Discard, "", 0)); err == nil {
		t.Fatal("expected NewPriceTable to fail on an empty repo, got nil")
	}
}

func TestSeedDefaults_Idempotent(t *testing.T) {
	repo := &memPriceRepo{}
	logger := log.New(io.Discard, "", 0)

	if err := SeedDefaults(context.Background(), repo, logger); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	first := len(repo.rows)
	if first == 0 {
		t.Fatal("seed produced zero rows")
	}

	if err := SeedDefaults(context.Background(), repo, logger); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if len(repo.rows) != first {
		t.Errorf("re-seed grew the table: got %d, want %d", len(repo.rows), first)
	}
}
