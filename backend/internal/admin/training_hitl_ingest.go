package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ────────────────────────────────────────────────────────────────────
// HITL → training-data ingest (mootd-admin#126, Phase 3).
//
// Every HITL attribute correction is a free training pair: the model's
// original structuredDescription (rejected) vs the admin-corrected one
// (chosen). The orchestrator doesn't persist the pre-edit values in a
// structured form, so we snapshot them at PATCH time (GET-before-PATCH
// in HitlPatchAttributes) and record the pair here.
//
// The pair is stored in the SAME training_trials collection as manual
// trials, shaped so the Phase 2 export reconstruction works with no
// special-casing: GemmaDescription = the original (rejected),
// ClaudeDescription = the corrected (chosen), and every patched path is
// a "claude" pick. reconstructGold then yields gemma-base + claude-picks
// = the corrected description, with changed=true wherever an edit moved
// a value — exactly the (chosen, rejected) signal a DPO export wants.
//
// source="hitl" keeps these out of the manual review list (they're
// already reviewed) while still flowing to exports.
// ────────────────────────────────────────────────────────────────────

// hitlItemSnapshot is the slim slice of a HITL item we capture before a
// patch: the pre-edit structured description (the rejected half) plus
// the source image URI.
type hitlItemSnapshot struct {
	StructuredDescription map[string]any
	ImageURL              string
}

// fetchHitlItemSnapshot GETs the current HITL item from the orchestrator
// and pulls out its structuredDescription + source image URL. Used only
// for ingest, so all failures are returned to the caller to log-and-skip
// — they must never affect the PATCH the admin actually requested.
func (h *Handler) fetchHitlItemSnapshot(ctx context.Context, id string) (*hitlItemSnapshot, error) {
	if h.hitlProxy == nil {
		return nil, fmt.Errorf("hitl proxy not wired")
	}
	upstream := fmt.Sprintf("%s/v1/admin/items/%s", h.hitlProxy.BaseURL, urlPathEscape(id))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream, nil)
	if err != nil {
		return nil, err
	}
	if h.hitlProxy.APIKey != "" {
		req.Header.Set("X-API-Key", h.hitlProxy.APIKey)
	}
	resp, err := h.hitlProxy.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("item GET returned %d", resp.StatusCode)
	}
	var parsed struct {
		Item struct {
			StructuredDescription map[string]any `json:"structuredDescription"`
			ImageURL              string         `json:"imageUrl"`
		} `json:"item"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if parsed.Item.StructuredDescription == nil {
		return nil, fmt.Errorf("item has no structuredDescription")
	}
	return &hitlItemSnapshot{
		StructuredDescription: parsed.Item.StructuredDescription,
		ImageURL:              parsed.Item.ImageURL,
	}, nil
}

// ingestHitlCorrection records the (rejected, chosen) pair from a
// successful HITL patch. Best-effort — a write failure is logged, never
// surfaced (the patch already succeeded). See the file header for why
// the record is shaped as a Gemma=rejected / Claude=chosen "trial".
func (h *Handler) ingestHitlCorrection(ctx context.Context, requestID string, snap *hitlItemSnapshot, patches map[string]any, adminID string) {
	if h.trainingTrials == nil || snap == nil || len(patches) == 0 {
		return
	}
	orig := normalizeDesc(snap.StructuredDescription)
	chosenFlat := flattenDesc(orig)
	picks := make(map[string]string, len(patches))
	for path, val := range patches {
		chosenFlat[path] = val
		picks[path] = "claude" // claude = the corrected (chosen) side
	}
	chosen := unflattenDesc(chosenFlat)

	id := fmt.Sprintf("hitl-%s-%s", requestID, generateAuditID())
	if _, err := h.trainingTrials.SubmitTrainingTrial(ctx, id, adminID, TrainingSubmitInput{
		Picks:             picks,
		AttrCount:         len(picks),
		ClaudeDescription: chosen,
		GemmaDescription:  orig,
		SourceImageURL:    snap.ImageURL,
		Source:            TrainingSourceHITL,
	}, time.Now().UTC()); err != nil {
		h.logger.Printf("admin training: hitl ingest %s: %v", id, err)
	}
}
