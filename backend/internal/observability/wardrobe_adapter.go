package observability

import (
	"context"

	"mootd/backend/internal/wardrobe"
)

// WardrobeRecorderAdapter implements wardrobe.LLMRecorder by
// translating the wardrobe-package's mirror types into the
// observability-native types.
//
// Lives here (not in the wardrobe package) so the dependency
// direction stays one-way — wardrobe doesn't import observability
// at compile time, only that *something* satisfies its narrow
// LLMRecorder interface. Identical pattern to OutfitRecorderAdapter.
type WardrobeRecorderAdapter struct {
	r *LLMRecorder
}

// NewWardrobeRecorderAdapter wraps an LLMRecorder so it can be passed
// as a wardrobe.LLMRecorder to Detector.WithRecorder.
func NewWardrobeRecorderAdapter(r *LLMRecorder) *WardrobeRecorderAdapter {
	return &WardrobeRecorderAdapter{r: r}
}

// Record satisfies the wardrobe.LLMRecorder interface. Detection
// calls don't go through prompt archival (the prompt lives inside
// the detection service itself, opaque to us), so the archival
// fields stay zero-valued. They'll show up empty in the admin trace
// viewer with a "no archive — call from outside the prompt-archival
// boundary" note (mootd@ef7461e for the original archival ship).
func (a *WardrobeRecorderAdapter) Record(ctx context.Context, cc wardrobe.DetectorRecorderContext, obs wardrobe.DetectorRecorderObservation) {
	a.r.Record(ctx, CallContext{
		UserID:  cc.UserID,
		Feature: cc.Feature,
	}, CallObservation{
		Provider:     obs.Provider,
		Model:        obs.Model,
		InputTokens:  obs.InputTokens,
		OutputTokens: obs.OutputTokens,
		StartedAt:    obs.StartedAt,
		EndedAt:      obs.EndedAt,
		Err:          obs.Err,
	})
}
