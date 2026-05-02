// Package budget enforces per-user LLM-spend caps (P4-02 /
// mootd-admin#30).
//
// Two collaborators:
//
//	SpendTracker — reads/writes today's spend in a fast cache
//	               (Redis) and persists 24h auto-suspend state.
//	BudgetReader — reads the per-user cap (Mongo `user_budgets`).
//
// Glued together by Enforcer.Check, which returns a Decision the
// outfit service inspects before kicking off a generation.
//
// Scope split with adjacent issues:
//
//   - Daily / monthly cap data model, GET/PUT endpoints + audit:
//     mootd-admin#29 / P4-01 (closed). This package is the
//     enforcement-half — those caps only mean something once we
//     stop honouring requests that breach them.
//
//   - Auto-downgrade to Haiku/Ollama at the cap is deferred. The
//     Decision enum has a Downgrade variant for the future hookup;
//     today, breaching the cap returns Deny instead. Reasoning:
//     v1's primary purpose is preventing runaway spend; staged
//     fallback is a refinement once the gate is in place.
//
//   - Email at 80% threshold is deferred — needs SES/SMTP wiring
//     (mootd-admin#99). Tracked on this issue's closing comment.
package budget

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Decision is the gate's verdict. Callers check this first.
type Decision int

const (
	// Allow lets the call through unmodified. Most common path.
	Allow Decision = iota
	// Downgrade is a future state — the caller should retry the
	// LLM call against a cheaper provider. Today's enforcement
	// returns Deny instead; Decision is forward-compatible so
	// adding the cascade-tier-skip later doesn't change this
	// public surface.
	Downgrade
	// Deny refuses the call. The caller surfaces a 429 with the
	// Reason populated.
	Deny
)

// Cap mirrors the relevant fields from admin.UserBudget. Defined
// here so the budget package doesn't import admin (one-way
// dependency: the admin handler reads the BudgetReader the budget
// package supplies).
type Cap struct {
	UserID     string
	DailyUSD   float64
	MonthlyUSD float64
	IsDefault  bool
}

// BudgetReader fetches the per-user cap. The admin package's
// UserBudgetsMongoRepository satisfies this with a thin adapter in
// app/.
type BudgetReader interface {
	BudgetForUser(ctx context.Context, userID string) (Cap, error)
}

// SpendTracker is the fast-path daily-spend cache. Implementations:
//
//   - RedisSpendTracker (production): keyed by user+date with 48h
//     TTL. Cheap reads, atomic writes.
//   - NoopSpendTracker (tests / Redis-down): always reports 0.
//
// Spend is denominated in USD as a float64. We keep the precision
// (rather than rounding to cents) because per-call LLM cost
// resolution is naturally fractional and we want the gate to fire
// when a $0.20 call would push a user $0.10 past a $1.00 cap.
type SpendTracker interface {
	// TodaySpend returns the user's USD spend so far today (UTC).
	TodaySpend(ctx context.Context, userID string) (float64, error)

	// Increment adds a delta. Concurrent-safe.
	Increment(ctx context.Context, userID string, costUSD float64) error

	// IsSuspended reports whether the user is currently in a 24h
	// auto-suspend window from a previous 200% breach.
	IsSuspended(ctx context.Context, userID string) (bool, error)

	// Suspend marks the user as auto-suspended until `until`.
	Suspend(ctx context.Context, userID string, until time.Time) error
}

// Reason carries the why behind a Deny/Downgrade decision. The
// outfit handler embeds it in the 429 body so the frontend can
// show "Daily $2.00 cap reached at $2.13. Try again tomorrow."
type Reason struct {
	Code           string  // "over_daily_cap", "auto_suspended", "over_200pct"
	Message        string  // human-readable, safe to surface to the user
	DailyCapUSD    float64 // 0 when not relevant (suspended state)
	TodaySpendUSD  float64
	EstimatedUSD   float64
	SuspendedUntil *time.Time // populated only when Code = "auto_suspended"
}

// Enforcer is the public gate.
type Enforcer struct {
	reader  BudgetReader
	tracker SpendTracker
}

// NewEnforcer wires the dependencies. Both must be non-nil; the
// caller is expected to substitute Noop implementations rather
// than passing nil.
func NewEnforcer(reader BudgetReader, tracker SpendTracker) *Enforcer {
	return &Enforcer{reader: reader, tracker: tracker}
}

// ErrNotConfigured is returned when the enforcer is asked to
// check before init completed. Caller should treat as Allow —
// failing closed on a config glitch is worse for users than
// over-running the cap by a few cents.
var ErrNotConfigured = errors.New("budget: enforcer not configured")

// Check applies the gate. `estimatedUSD` is a conservative upper
// bound the caller computes — e.g. for outfit generation, the
// largest plausible token cost.
//
// Lifecycle:
//
//  1. Suspended? → Deny with code "auto_suspended" and the
//     suspended-until timestamp on the Reason.
//  2. Over 200% (current + estimate ≥ 2× daily cap)? → mark
//     suspended for 24h, then Deny with "over_200pct".
//  3. Over 100% (current + estimate > daily cap)? → Deny with
//     "over_daily_cap". (Future: Downgrade.)
//  4. Otherwise → Allow.
func (e *Enforcer) Check(ctx context.Context, userID string, estimatedUSD float64) (Decision, Reason, error) {
	if e == nil || e.reader == nil || e.tracker == nil {
		return Allow, Reason{}, ErrNotConfigured
	}
	if userID == "" {
		return Allow, Reason{}, errors.New("budget: userID required")
	}

	// 1. Suspended?
	suspended, err := e.tracker.IsSuspended(ctx, userID)
	if err != nil {
		// Best-effort: a Redis blip shouldn't deny service. Log
		// upstream — we have no logger here on purpose, callers
		// surface the error.
		return Allow, Reason{}, err
	}
	if suspended {
		until := time.Now().UTC().Add(24 * time.Hour) // upper bound; actual stored value isn't read here
		return Deny, Reason{
			Code:           "auto_suspended",
			Message:        "Account temporarily suspended after exceeding 200% of daily LLM budget. Try again in 24 hours.",
			SuspendedUntil: &until,
		}, nil
	}

	// 2 & 3. Spend check.
	cap, err := e.reader.BudgetForUser(ctx, userID)
	if err != nil {
		return Allow, Reason{}, fmt.Errorf("read cap: %w", err)
	}
	today, err := e.tracker.TodaySpend(ctx, userID)
	if err != nil {
		return Allow, Reason{}, fmt.Errorf("read today spend: %w", err)
	}

	projected := today + estimatedUSD
	if cap.DailyUSD <= 0 {
		// A cap of 0 means "no cap configured." We treat that as
		// allow rather than "everything is over budget."
		return Allow, Reason{TodaySpendUSD: today, EstimatedUSD: estimatedUSD}, nil
	}

	if projected >= 2*cap.DailyUSD {
		// Auto-suspend for 24h.
		until := time.Now().UTC().Add(24 * time.Hour)
		if serr := e.tracker.Suspend(ctx, userID, until); serr != nil {
			// Suspend write failed — still deny this call but log
			// upstream. The next call will re-evaluate and
			// either re-suspend or allow.
			return Deny, Reason{
				Code:          "over_200pct",
				Message:       "200% of daily LLM budget reached. Generation blocked for 24 hours.",
				DailyCapUSD:   cap.DailyUSD,
				TodaySpendUSD: today,
				EstimatedUSD:  estimatedUSD,
			}, fmt.Errorf("suspend: %w", serr)
		}
		return Deny, Reason{
			Code:           "over_200pct",
			Message:        "200% of daily LLM budget reached. Generation blocked for 24 hours.",
			DailyCapUSD:    cap.DailyUSD,
			TodaySpendUSD:  today,
			EstimatedUSD:   estimatedUSD,
			SuspendedUntil: &until,
		}, nil
	}

	if projected > cap.DailyUSD {
		return Deny, Reason{
			Code:          "over_daily_cap",
			Message:       fmt.Sprintf("Daily LLM budget of $%.2f reached. Try again tomorrow.", cap.DailyUSD),
			DailyCapUSD:   cap.DailyUSD,
			TodaySpendUSD: today,
			EstimatedUSD:  estimatedUSD,
		}, nil
	}

	return Allow, Reason{
		DailyCapUSD:   cap.DailyUSD,
		TodaySpendUSD: today,
		EstimatedUSD:  estimatedUSD,
	}, nil
}

// ────────────────────────────────────────────────────────────────────
// RedisSpendTracker — production implementation.
// ────────────────────────────────────────────────────────────────────

// RedisSpendTracker stores per-day per-user spend in
// `user:{id}:spend:{YYYY-MM-DD}` (TTL 48h) and 24h auto-suspend
// flags in `user:{id}:suspend` (TTL = the suspend window).
//
// 48h TTL on spend keys gives a buffer for the midnight-UTC
// boundary so a request firing at 23:59:59.999 can finish reading
// today's bucket; the next read picks up the new day's empty key.
// We rely on Redis's own clock for expiry — same trust model as
// the rate-limiter middleware.
type RedisSpendTracker struct {
	rdb       *redis.Client
	keyPrefix string
}

// NewRedisSpendTracker wires the client. `prefix` namespaces
// keys (defaults to "user" — the issue's spec).
func NewRedisSpendTracker(rdb *redis.Client, prefix string) *RedisSpendTracker {
	if prefix == "" {
		prefix = "user"
	}
	return &RedisSpendTracker{rdb: rdb, keyPrefix: prefix}
}

func (t *RedisSpendTracker) spendKey(userID string) string {
	return fmt.Sprintf("%s:%s:spend:%s", t.keyPrefix, userID, time.Now().UTC().Format("2006-01-02"))
}

func (t *RedisSpendTracker) suspendKey(userID string) string {
	return fmt.Sprintf("%s:%s:suspend", t.keyPrefix, userID)
}

func (t *RedisSpendTracker) TodaySpend(ctx context.Context, userID string) (float64, error) {
	if t == nil || t.rdb == nil {
		return 0, nil
	}
	val, err := t.rdb.Get(ctx, t.spendKey(userID)).Float64()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, nil
		}
		return 0, err
	}
	return val, nil
}

func (t *RedisSpendTracker) Increment(ctx context.Context, userID string, costUSD float64) error {
	if t == nil || t.rdb == nil {
		return nil
	}
	if costUSD <= 0 {
		return nil
	}
	key := t.spendKey(userID)
	// Pipeline so the IncrByFloat + Expire are one round-trip.
	pipe := t.rdb.TxPipeline()
	pipe.IncrByFloat(ctx, key, costUSD)
	pipe.Expire(ctx, key, 48*time.Hour)
	_, err := pipe.Exec(ctx)
	return err
}

func (t *RedisSpendTracker) IsSuspended(ctx context.Context, userID string) (bool, error) {
	if t == nil || t.rdb == nil {
		return false, nil
	}
	res, err := t.rdb.Exists(ctx, t.suspendKey(userID)).Result()
	if err != nil {
		return false, err
	}
	return res > 0, nil
}

func (t *RedisSpendTracker) Suspend(ctx context.Context, userID string, until time.Time) error {
	if t == nil || t.rdb == nil {
		return nil
	}
	ttl := time.Until(until)
	if ttl <= 0 {
		// Refuse to suspend in the past; treat as no-op.
		return nil
	}
	return t.rdb.Set(ctx, t.suspendKey(userID), until.Format(time.RFC3339), ttl).Err()
}

// ────────────────────────────────────────────────────────────────────
// NoopSpendTracker — tests and Redis-down fallback.
// ────────────────────────────────────────────────────────────────────

// NoopSpendTracker reports zero spend, never suspends, accepts
// every Increment. Use it to disable enforcement entirely (e.g.
// when Redis is unavailable at boot — the alternative is failing
// every outfit generation closed, which is worse).
type NoopSpendTracker struct{}

func (NoopSpendTracker) TodaySpend(context.Context, string) (float64, error)             { return 0, nil }
func (NoopSpendTracker) Increment(context.Context, string, float64) error                { return nil }
func (NoopSpendTracker) IsSuspended(context.Context, string) (bool, error)               { return false, nil }
func (NoopSpendTracker) Suspend(context.Context, string, time.Time) error                { return nil }
