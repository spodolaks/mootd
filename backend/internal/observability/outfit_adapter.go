package observability

import (
	"context"

	"mootd/backend/internal/outfit"
)

// OutfitRecorderAdapter implements outfit.llmRecorder by translating
// the outfit-package's mirror types into observability's native types.
// Lives here (not in the outfit package) so the dependency direction
// stays one-way — outfit doesn't know observability exists at compile
// time, only that *something* satisfies its narrow interface.
type OutfitRecorderAdapter struct {
	r *LLMRecorder
}

// NewOutfitRecorderAdapter wraps an LLMRecorder so it can be passed as
// outfit.ServiceConfig.LLMRecorder.
func NewOutfitRecorderAdapter(r *LLMRecorder) *OutfitRecorderAdapter {
	return &OutfitRecorderAdapter{r: r}
}

// Record satisfies the outfit.llmRecorder interface.
func (a *OutfitRecorderAdapter) Record(ctx context.Context, cc outfit.LLMRecorderContext, obs outfit.LLMRecorderObservation) {
	a.r.Record(ctx, CallContext{
		UserID:     cc.UserID,
		Feature:    cc.Feature,
		TraceID:    cc.TraceID,
		PromptText: cc.PromptText,
	}, CallObservation{
		Provider:         obs.Provider,
		Model:            obs.Model,
		InputTokens:      obs.InputTokens,
		OutputTokens:     obs.OutputTokens,
		CacheReadTokens:  obs.CacheReadTokens,
		CacheWriteTokens: obs.CacheWriteTokens,
		PromptVersion:    obs.PromptVersion,
		StartedAt:        obs.StartedAt,
		EndedAt:          obs.EndedAt,
		Err:              obs.Err,
	})
}
