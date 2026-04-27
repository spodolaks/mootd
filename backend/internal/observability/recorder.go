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
	repo   LLMCallRepository
	prices *PriceTable
	logger *log.Logger
}

// NewLLMRecorder constructs a recorder. All three deps are required.
func NewLLMRecorder(repo LLMCallRepository, prices *PriceTable, logger *log.Logger) *LLMRecorder {
	return &LLMRecorder{repo: repo, prices: prices, logger: logger}
}

// CallContext carries the per-request metadata needed to attribute a
// row in the ledger. Built by the caller (outfit service) once per
// request and passed into Record.
type CallContext struct {
	UserID     string
	Feature    string // "outfit_generate" today; "detection" / "search" later
	TraceID    string // optional — empty until P1-03 lands
	PromptText string // optional — used to compute promptHash for dedupe
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
	StartedAt        time.Time
	EndedAt          time.Time
	Err              error // nil on success
}

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
	}
	if cc.PromptText != "" {
		row.PromptHash = HashPrompt(cc.PromptText)
	}

	if err := r.repo.AppendLLMCall(ctx, row); err != nil {
		// Log + swallow. The user-facing call already succeeded; we
		// don't want to retroactively fail it because the ledger
		// write blipped.
		r.logger.Printf("observability: append llm_calls failed: %v (user=%s, feature=%s, model=%s)", err, cc.UserID, cc.Feature, obs.Model)
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
