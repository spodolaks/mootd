package outfit

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// CascadeGenerator wraps an ordered chain of providers and falls
// through on transient failures. Closes mootd#58.
//
// Design philosophy:
//   - Try the chain in order. First success wins.
//   - On error, classify it: 4xx-like client errors short-circuit
//     (re-trying on a different provider won't help). 5xx / timeout /
//     network errors fall through to the next provider.
//   - Track per-provider health in a rolling window. After three
//     consecutive failures, mark the provider unhealthy for 60s
//     (skip it entirely until cooldown expires).
//   - When *all* providers are unhealthy, attempt them anyway —
//     better to try and possibly fail than refuse to serve.
//
// Health stats are exposed for the admin Provider Health Board
// (mootd-admin issue forthcoming) — see HealthSnapshot().
type CascadeGenerator struct {
	chain  []Generator
	health *HealthTracker
	logger *log.Logger
}

// NewCascadeGenerator builds a cascade from the given chain. Order
// matters — earlier providers are preferred. Pass ascending cost
// (best/cheapest first) for the typical "Claude → OpenAI → Ollama"
// production setup, or descending cost when running primarily on
// Ollama with paid providers as fallback.
func NewCascadeGenerator(logger *log.Logger, chain ...Generator) *CascadeGenerator {
	if logger == nil {
		logger = log.Default()
	}
	return &CascadeGenerator{
		chain:  chain,
		health: NewHealthTracker(),
		logger: logger,
	}
}

// Name returns "cascade" + the underlying chain. Used in logs and
// in the llm_calls row's metadata; the actual serving provider is
// stamped on Usage.Provider by the underlying Generator, so admin
// pages can still slice by real provider.
func (c *CascadeGenerator) Name() string {
	if len(c.chain) == 0 {
		return "cascade(empty)"
	}
	names := "cascade("
	for i, g := range c.chain {
		if i > 0 {
			names += " > "
		}
		names += g.Name()
	}
	return names + ")"
}

// Generate iterates the chain. Returns the first successful result;
// on exhaustion returns ErrAllProvidersFailed wrapping the last
// non-shortcircuit error.
func (c *CascadeGenerator) Generate(ctx context.Context, req GeneratorRequest) ([]Outfit, *Usage, error) {
	if len(c.chain) == 0 {
		return nil, nil, errors.New("cascade: empty chain")
	}

	// First pass: only try healthy providers. Fast path for the
	// typical case where the head of the chain is healthy.
	//
	// lastUsage holds the most recent non-nil Usage from a *failed*
	// attempt. Providers (notably Anthropic) bill tokens before
	// returning a 5xx/parse error, so a billed-but-failed attempt
	// must still surface its Usage to the caller for the ledger —
	// otherwise the cost vanishes when the whole chain exhausts.
	any := false
	var lastErr error
	var lastUsage *Usage
	for _, gen := range c.chain {
		if !c.health.IsHealthy(gen.Name()) {
			c.logger.Printf("cascade: skipping unhealthy provider %q", gen.Name())
			continue
		}
		any = true
		outfits, usage, err := gen.Generate(ctx, req)
		if err == nil {
			c.health.RecordSuccess(gen.Name())
			return outfits, usage, nil
		}
		if usage != nil {
			lastUsage = usage
		}
		c.health.RecordFailure(gen.Name(), err)
		lastErr = err
		if shouldNotFallback(err) {
			// Re-trying on another provider won't help (auth bug,
			// malformed wardrobe, ctx cancelled). Bail.
			return nil, usage, err
		}
		c.logger.Printf("cascade: %s failed (%v), falling through", gen.Name(), err)
	}

	// Nothing healthy in the chain. Last-resort: try every provider
	// regardless of health state. Better to try and possibly serve
	// than refuse outright.
	if !any {
		c.logger.Printf("cascade: all providers unhealthy, attempting anyway")
		for _, gen := range c.chain {
			outfits, usage, err := gen.Generate(ctx, req)
			if err == nil {
				c.health.RecordSuccess(gen.Name())
				return outfits, usage, nil
			}
			if usage != nil {
				lastUsage = usage
			}
			c.health.RecordFailure(gen.Name(), err)
			lastErr = err
			if shouldNotFallback(err) {
				return nil, usage, err
			}
		}
	}

	if lastErr != nil {
		// Return lastUsage (may be nil) so a billed-but-failed attempt
		// is still recorded in the LLM-call ledger.
		return nil, lastUsage, ErrAllProvidersFailed(lastErr)
	}
	return nil, nil, errors.New("cascade: no provider attempted")
}

// HealthSnapshot returns a copy of the current per-provider health
// stats. Used by the future Provider Health Board admin page.
func (c *CascadeGenerator) HealthSnapshot() map[string]ProviderHealth {
	return c.health.Snapshot()
}

// ErrAllProvidersFailed wraps the final error after the entire
// cascade exhausted. Callers can errors.Is against the wrapped
// inner error if they care about the original cause.
type ErrAllProvidersFailed error

// shouldNotFallback returns true when an error is the kind that
// retrying on a different provider can't fix.
//
// Examples:
//   - context.Canceled / context.DeadlineExceeded — caller gave up
//   - errors flagged as ErrFatal (auth-style failures)
//
// 5xx, network timeouts, rate limits all fall through.
func shouldNotFallback(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var fatal ErrFatal
	return errors.As(err, &fatal)
}

// ErrFatal marks an error as cascade-fatal — re-trying on another
// provider won't change the outcome. Generators wrap their own
// errors with this when they detect 4xx-style problems (bad request,
// auth failure, content policy violation).
type ErrFatal struct {
	Inner error
}

func (e ErrFatal) Error() string { return "fatal: " + e.Inner.Error() }
func (e ErrFatal) Unwrap() error { return e.Inner }

// HealthTracker keeps per-provider rolling health stats. Goroutine-safe.
type HealthTracker struct {
	mu              sync.Mutex
	stats           map[string]*ProviderHealth
	cooldown        time.Duration
	failsToCooldown int
}

// NewHealthTracker constructs a tracker with default thresholds:
// 3 consecutive failures → 60s cooldown.
func NewHealthTracker() *HealthTracker {
	return &HealthTracker{
		stats:           make(map[string]*ProviderHealth),
		cooldown:        60 * time.Second,
		failsToCooldown: 3,
	}
}

// ProviderHealth is the per-provider rolling state.
type ProviderHealth struct {
	Name              string    `json:"name"`
	Successes         int64     `json:"successes"`
	Failures          int64     `json:"failures"`
	ConsecutiveFails  int       `json:"consecutiveFails"`
	LastSuccess       time.Time `json:"lastSuccess,omitempty"`
	LastFailure       time.Time `json:"lastFailure,omitempty"`
	UnhealthyUntil    time.Time `json:"unhealthyUntil,omitempty"`
	LastFailureReason string    `json:"lastFailureReason,omitempty"`
}

// IsHealthy returns true unless the provider is in cooldown.
func (h *HealthTracker) IsHealthy(name string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.get(name)
	if s.UnhealthyUntil.IsZero() {
		return true
	}
	return time.Now().After(s.UnhealthyUntil)
}

// RecordSuccess clears any cooldown and increments the success counter.
func (h *HealthTracker) RecordSuccess(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.get(name)
	s.Successes++
	s.ConsecutiveFails = 0
	s.LastSuccess = time.Now()
	s.UnhealthyUntil = time.Time{}
	s.LastFailureReason = ""
}

// RecordFailure increments the failure counter; if consecutive
// failures cross the threshold, marks the provider unhealthy.
func (h *HealthTracker) RecordFailure(name string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.get(name)
	s.Failures++
	s.ConsecutiveFails++
	s.LastFailure = time.Now()
	if err != nil {
		// Truncate to avoid pathological log-line growth.
		msg := err.Error()
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		s.LastFailureReason = msg
	}
	if s.ConsecutiveFails >= h.failsToCooldown {
		s.UnhealthyUntil = time.Now().Add(h.cooldown)
	}
}

// Snapshot returns a copy of the current stats. Safe to mutate.
func (h *HealthTracker) Snapshot() map[string]ProviderHealth {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]ProviderHealth, len(h.stats))
	for k, v := range h.stats {
		out[k] = *v
	}
	return out
}

func (h *HealthTracker) get(name string) *ProviderHealth {
	if s, ok := h.stats[name]; ok {
		return s
	}
	s := &ProviderHealth{Name: name}
	h.stats[name] = s
	return s
}
