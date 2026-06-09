package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"time"
)

// LLMRecorder is the cross-cutting helper the outfit service uses to
// log every LLM call to the ledger. Wrapping at this layer (instead
// of inside each generator) keeps generators focused on protocol
// translation; the recorder owns timing, cost computation, and the
// Mongo write.
//
// Single instance, shared across requests — safe for concurrent use
// because the underlying repo + price table are.
type LLMRecorder struct {
	repo    LLMCallRepository
	prices  *PriceTable
	tracker SpendIncrementer // optional — bumps the budget tracker on every billed call (cost > 0), success or failure
	logger  *log.Logger
}

// SpendIncrementer is the slice of the budget package's
// SpendTracker that the recorder uses. Defined here so observability
// doesn't import the budget package — same one-way dep convention
// as the rest of the codebase.
type SpendIncrementer interface {
	Increment(ctx context.Context, userID string, costUSD float64) error
}

// NewLLMRecorder constructs a recorder. All three deps are required.
func NewLLMRecorder(repo LLMCallRepository, prices *PriceTable, logger *log.Logger) *LLMRecorder {
	return &LLMRecorder{repo: repo, prices: prices, logger: logger}
}

// WithSpendTracker wires the budget tracker. Optional — when
// unset, every Record call still writes to llm_calls but doesn't
// update the per-user daily total. Production app.go always wires
// it; tests can leave it nil.
func (r *LLMRecorder) WithSpendTracker(t SpendIncrementer) *LLMRecorder {
	r.tracker = t
	return r
}

// CallContext carries the per-request metadata needed to attribute a
// row in the ledger. Built by the caller (outfit service) once per
// request and passed into Record.
//
// SystemPrompt + UserMessage + WardrobeItemIDs are P1-11 archival
// fields — see LLMCall for the storage rationale. PromptText is the
// concatenated prompt used to compute the dedupe hash; pass it
// alongside SystemPrompt/UserMessage so the recorder can stamp both
// the hash AND the inline body in one pass.
type CallContext struct {
	UserID          string
	Feature         string // "outfit_generate" today; "detection" / "search" later
	TraceID         string // optional — empty until P1-03 lands
	PromptText      string // optional — used to compute promptHash for dedupe
	SystemPrompt    string // P1-11 archival: rendered system prompt
	UserMessage     string // P1-11 archival: rendered user message
	WardrobeItemIDs []string
	// DetectionRunID links detection_* rows to their parent run.
	// Empty for outfit-generation; populated by the wardrobe
	// adapter when the detection handler creates a run id upfront.
	DetectionRunID string

	// Granular cost-tagging dimensions (mootd#63). Optional;
	// callers populate what they have. Recorder copies them
	// onto the row.
	WardrobeItemCount int
	ImageCount        int
	RecentBoardCount  int
}

// CallObservation is the result of calling the LLM, packaged so we
// can write the row regardless of whether the call succeeded or
// failed. Provider + Model + token counts are populated by the
// generator's *Usage; the wrapper handles cost + timing + status.
type CallObservation struct {
	Provider         string
	Model            string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	PromptVersion    string
	RawResponse      string // P1-11 archival: the LLM's text/tool-use payload
	StartedAt        time.Time
	EndedAt          time.Time
	Err              error // nil on success

	// Token-region split (mootd#63). Optional — callers
	// populate what their generator API exposes. For Anthropic
	// + OpenAI the prompt is split into system + user roles in
	// the API response. SystemTokens + UserTokens should sum to
	// InputTokens; ResponseTokens should equal OutputTokens
	// (kept separate so callers don't have to reach into the
	// totals when they have role-level numbers).
	SystemTokens   int
	UserTokens     int
	ResponseTokens int
}

// archivalFieldMaxBytes caps any single archival field. The actual
// payload sizes today are well under this (system prompt ~5KB, user
// message ~2KB, response ~10KB), so the cap is purely a safety net
// against pathological growth (e.g. a model that ignores max_tokens
// and floods us). Fields are truncated, not dropped — partial data
// beats no data when debugging.
const archivalFieldMaxBytes = 64 * 1024

// Record writes one llm_calls row for the given call. Cost is computed
// from the price table at write time (so historical rows stay accurate
// when prices change later). Errors writing the row are logged but
// never surfaced — losing an audit row is bad, but failing the user's
// outfit generation because Mongo blipped is worse.
func (r *LLMRecorder) Record(ctx context.Context, cc CallContext, obs CallObservation) {
	cost, costErr := r.prices.ComputeCost(obs.Model, obs.InputTokens, obs.OutputTokens, obs.CacheReadTokens, obs.CacheWriteTokens)
	if costErr != nil {
		// Unpriced model — don't drop the row. Cost stays at 0 and a
		// log line surfaces the missing price so operators can fix
		// the table.
		r.logger.Printf("observability: unpriced model %q (provider=%s); writing row with cost_usd=0", obs.Model, obs.Provider)
	}

	status := "success"
	errorMsg := ""
	if obs.Err != nil {
		status = "error"
		errorMsg = truncateErr(obs.Err.Error(), 1024)
	}

	// CacheHitRatio = cacheRead / (cacheRead + cacheWrite + input)
	// (mootd#63). Stored on the row so analytics can $group on
	// the ratio directly. Zero denominator → 0; we don't
	// divide-by-zero or stash a special sentinel.
	cacheTotal := obs.CacheReadTokens + obs.CacheWriteTokens + obs.InputTokens
	cacheHitRatio := 0.0
	if cacheTotal > 0 {
		cacheHitRatio = float64(obs.CacheReadTokens) / float64(cacheTotal)
	}

	row := LLMCall{
		ID:               generateLLMCallID(),
		TraceID:          cc.TraceID,
		UserID:           cc.UserID,
		Provider:         obs.Provider,
		Model:            obs.Model,
		Feature:          cc.Feature,
		InputTokens:      obs.InputTokens,
		OutputTokens:     obs.OutputTokens,
		CacheReadTokens:  obs.CacheReadTokens,
		CacheWriteTokens: obs.CacheWriteTokens,
		DurationMs:       obs.EndedAt.Sub(obs.StartedAt).Milliseconds(),
		Status:           status,
		CostUSD:          cost,
		PromptVersion:    obs.PromptVersion,
		ErrorMsg:         errorMsg,
		CreatedAt:        obs.StartedAt.UTC(),
		// P1-11 archival — truncated for safety; sane payloads are
		// orders of magnitude under the cap.
		SystemPrompt:    truncateField(cc.SystemPrompt),
		UserMessage:     truncateField(cc.UserMessage),
		ResponseRaw:     truncateField(obs.RawResponse),
		WardrobeItemIDs: cc.WardrobeItemIDs,
		DetectionRunID:  cc.DetectionRunID,
		// Granular cost-tagging (mootd#63).
		WardrobeItemCount: cc.WardrobeItemCount,
		ImageCount:        cc.ImageCount,
		RecentBoardCount:  cc.RecentBoardCount,
		SystemTokens:      obs.SystemTokens,
		UserTokens:        obs.UserTokens,
		ResponseTokens:    obs.ResponseTokens,
		CacheHitRatio:     cacheHitRatio,
	}
	// PromptHash dedupe key. Prefer PromptText (caller's pre-built
	// concat) over re-stitching, but fall back to system+user when
	// only the archival fields were supplied — gives callers one
	// thing to populate.
	hashSrc := cc.PromptText
	if hashSrc == "" && (cc.SystemPrompt != "" || cc.UserMessage != "") {
		hashSrc = cc.SystemPrompt + "\n" + cc.UserMessage
	}
	if hashSrc != "" {
		row.PromptHash = HashPrompt(hashSrc)
	}

	if err := r.repo.AppendLLMCall(ctx, row); err != nil {
		// Log + swallow. The user-facing call already succeeded; we
		// don't want to retroactively fail it because the ledger
		// write blipped.
		r.logger.Printf("observability: append llm_calls failed: %v (user=%s, feature=%s, model=%s)", err, cc.UserID, cc.Feature, obs.Model)
	}

	// Budget tracker (P4-02 / mootd-admin#30): bump the per-user
	// daily counter so the next request's enforcement gate sees
	// today's spend including this call. Best-effort — Redis
	// blips are logged but never fail the user-facing request,
	// same pattern as the ledger write above.
	//
	// Increment whenever a cost was actually billed (cost > 0),
	// REGARDLESS of status. A failed attempt that still consumed
	// tokens — a provider erroring mid-stream, or a cascade hop that
	// billed before failing over — costs real money and must count
	// against the daily cap. Gating on status=="success" (the old
	// behaviour) let failure storms undercount spend, so the
	// enforcement gate (Redis) drifted below the ledger (#153, a
	// follow-up to the #106 billed-but-failed accounting). Skipped
	// only when no cost was incurred (e.g. Ollama: free, or an error
	// before any tokens) or no userID was attached.
	if r.tracker != nil && cost > 0 && cc.UserID != "" {
		if terr := r.tracker.Increment(ctx, cc.UserID, cost); terr != nil {
			r.logger.Printf("observability: budget tracker increment failed: %v (user=%s, cost=$%.4f, status=%s)", terr, cc.UserID, cost, status)
		}
	}
}

// truncateErr keeps the error message bounded so a verbose underlying
// error (e.g. dump of a 200kb LLM response in the OpenAI generator)
// doesn't bloat the ledger row.
func truncateErr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + " …(truncated)"
}

// truncateField caps an archival field at archivalFieldMaxBytes.
// Inserts a sentinel suffix so debug readers can tell the field was
// truncated instead of legitimately ending mid-word. Byte-based
// rather than rune-based — these fields contain JSON / English
// prose, never multi-byte literals where the cut would matter.
func truncateField(s string) string {
	if len(s) <= archivalFieldMaxBytes {
		return s
	}
	return s[:archivalFieldMaxBytes] + "…(truncated)"
}

// generateLLMCallID returns a unique row id. Mongo doesn't require
// strings, but using "llm_<hex>" keeps the IDs greppable in logs
// alongside the user-side IDs.
func generateLLMCallID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic("observability: crypto/rand unavailable: " + err.Error())
	}
	return "llm_" + hex.EncodeToString(b)
}
