package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"mootd/backend/internal/shared/response"
)

// ────────────────────────────────────────────────────────────────────
// Training-data export (mootd-admin#125, Phase 2).
//
// Turns submitted training trials into trainer-consumable JSONL. Two
// formats, both streamed (one JSON object per line, never materialised
// into a slice — a large corpus can't OOM the box):
//
//	sft → one record per submitted trial: the reconstructed gold
//	      structured description (the model-input photo is referenced
//	      by sourceImageUrl). Gold = Gemma's output with each human
//	      pick applied on top (claude/custom overrides; gemma picks are
//	      no-ops). Unreviewed attributes keep Gemma's value — we never
//	      fabricate a preference the reviewer didn't express.
//
//	dpo → one record per trial WHERE the human changed something:
//	      chosen = the same reconstructed gold, rejected = Gemma's full
//	      original. Trials where Gemma won every pick (or all picks were
//	      gemma / agreed) carry no preference signal, so they're skipped
//	      — matching the Phase 2 acceptance criterion.
//
// Reconstruction reads ONLY the trial record's snapshot (Phase 1,
// mootd-admin#124) — it never touches the orchestrator. Trials
// submitted before the snapshot landed have no GemmaDescription and are
// skipped (counted in the audit row).
//
// Gated on training:export (distinct from traces:rerun) and audited —
// this ships data off the box (docs/SECURITY.md export-exfil risk).
// ────────────────────────────────────────────────────────────────────

// maxTrainingExportRows bounds a single export. The corpus is built by
// hand today (one trial per manual review) so this is generous; it's a
// backstop, not an expected limit.
const maxTrainingExportRows = 100_000

// trainingSFTRecord is one JSONL line of an sft export.
type trainingSFTRecord struct {
	TrialID        string         `json:"trialId"`
	SourceImageURL string         `json:"sourceImageUrl,omitempty"`
	Gold           map[string]any `json:"gold"`
	AttrCount      int            `json:"attrCount"`
	SubmittedBy    string         `json:"submittedBy,omitempty"`
	SubmittedAt    string         `json:"submittedAt,omitempty"`
}

// trainingDPORecord is one JSONL line of a dpo export.
type trainingDPORecord struct {
	TrialID        string         `json:"trialId"`
	SourceImageURL string         `json:"sourceImageUrl,omitempty"`
	Chosen         map[string]any `json:"chosen"`
	Rejected       map[string]any `json:"rejected"`
	SubmittedBy    string         `json:"submittedBy,omitempty"`
	SubmittedAt    string         `json:"submittedAt,omitempty"`
}

// TrainingExport handles GET /admin/v1/training/export?format=sft|dpo&since=RFC3339.
// Streams JSONL. Permission: training:export (gated inline — the route
// floor is traces:read, which readonly has but must not export with).
func (h *Handler) TrainingExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !HasPermissionFromContext(r, PermTrainingExport) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermTrainingExport,
		})
		return
	}
	if !h.trainingTrialsReady(w) {
		return
	}

	format := r.URL.Query().Get("format")
	if format != "sft" && format != "dpo" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "format must be 'sft' or 'dpo'",
		})
		return
	}
	var since time.Time
	if s := r.URL.Query().Get("since"); s != "" {
		parsed, err := time.Parse(time.RFC3339, s)
		if err != nil {
			response.WriteJSON(w, http.StatusBadRequest, map[string]string{
				"error": "since must be an RFC3339 timestamp",
			})
			return
		}
		since = parsed
	}
	// minAgreement (Phase 4, mootd-admin#127): when > 0, emit only
	// trials that were dual-reviewed AND met the agreement bar — a way
	// to keep noisy single-reviewer labels out of a training set.
	var minAgreement float64
	if s := r.URL.Query().Get("minAgreement"); s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v < 0 || v > 1 {
			response.WriteJSON(w, http.StatusBadRequest, map[string]string{
				"error": "minAgreement must be a number between 0 and 1",
			})
			return
		}
		minAgreement = v
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	filename := fmt.Sprintf("training-%s-%s.jsonl", format, time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	emitted, skipped := 0, 0

	total, streamErr := h.trainingTrials.StreamSubmittedTrainingTrials(ctx, since, maxTrainingExportRows, func(t TrainingTrial) error {
		// No snapshot → pre-Phase-1 trial; can't reconstruct a pair.
		if t.GemmaDescription == nil {
			skipped++
			return nil
		}
		// Label-quality gate: below the bar, or not dual-reviewed at all.
		if minAgreement > 0 && (t.Agreement == nil || *t.Agreement < minAgreement) {
			skipped++
			return nil
		}
		gold, changed := reconstructGold(t)
		submittedAt := ""
		if t.SubmittedAt != nil {
			submittedAt = t.SubmittedAt.UTC().Format(time.RFC3339)
		}
		if format == "sft" {
			if err := enc.Encode(trainingSFTRecord{
				TrialID:        t.ID,
				SourceImageURL: t.SourceImageURL,
				Gold:           gold,
				AttrCount:      t.AttrCount,
				SubmittedBy:    t.SubmittedBy,
				SubmittedAt:    submittedAt,
			}); err != nil {
				return err
			}
			emitted++
		} else { // dpo
			if !changed {
				// Gemma won every pick / all agreed → no preference signal.
				skipped++
				return nil
			}
			if err := enc.Encode(trainingDPORecord{
				TrialID:        t.ID,
				SourceImageURL: t.SourceImageURL,
				Chosen:         gold,
				Rejected:       normalizeDesc(t.GemmaDescription),
				SubmittedBy:    t.SubmittedBy,
				SubmittedAt:    submittedAt,
			}); err != nil {
				return err
			}
			emitted++
		}
		if flusher != nil {
			flusher.Flush()
		}
		return nil
	})
	if streamErr != nil {
		// Headers + partial body already on the wire — can't switch to
		// a JSON error. Log it; the JSONL consumer sees a clean EOF.
		h.logger.Printf("admin training export (%s): stream failed after %d rows: %v", format, total, streamErr)
	}

	h.auditTrainingExport(r, format, since, minAgreement, emitted, skipped, total)
}

// reconstructGold rebuilds the human-approved structured description
// from a trial's snapshot: Gemma's output is the base, and each pick is
// applied on top (claude → Claude's value at that path, custom → the
// typed value, gemma → left as-is). Returns the nested gold plus a
// `changed` flag — true when at least one pick moved a value off
// Gemma's original, i.e. there's a genuine (chosen != rejected) signal.
func reconstructGold(t TrainingTrial) (gold map[string]any, changed bool) {
	gemmaFlat := flattenDesc(normalizeDesc(t.GemmaDescription))
	claudeFlat := flattenDesc(normalizeDesc(t.ClaudeDescription))

	chosenFlat := make(map[string]any, len(gemmaFlat))
	for k, v := range gemmaFlat {
		chosenFlat[k] = v
	}
	for path, kind := range t.Picks {
		switch kind {
		case "claude":
			if v, ok := claudeFlat[path]; ok {
				chosenFlat[path] = v
			}
		case "custom":
			chosenFlat[path] = t.CustomValues[path]
		case "gemma":
			// no-op: Gemma's value is already the base.
		}
	}
	return unflattenDesc(chosenFlat), flatDiffers(chosenFlat, gemmaFlat)
}

// normalizeDesc JSON-round-trips a description so nested values are
// plain map[string]any / []any / scalars regardless of how they were
// decoded (the Mongo driver yields bson.M for nested docs, which the
// flatten type-switch wouldn't otherwise descend into).
func normalizeDesc(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out map[string]any
	if json.Unmarshal(b, &out) != nil {
		return m
	}
	return out
}

// flattenDesc maps a (normalized) nested description to dotted-path →
// leaf value, descending into objects only — arrays and scalars are
// leaves. Mirrors the admin UI's attribute flattening so paths line up
// with the picks recorded against them.
func flattenDesc(obj map[string]any) map[string]any {
	out := map[string]any{}
	var rec func(prefix string, m map[string]any)
	rec = func(prefix string, m map[string]any) {
		for k, v := range m {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			if sub, ok := v.(map[string]any); ok {
				rec(p, sub)
			} else {
				out[p] = v
			}
		}
	}
	if obj != nil {
		rec("", obj)
	}
	return out
}

// unflattenDesc rebuilds a nested object from dotted-path leaves. When a
// path segment collides with an existing non-object, the deeper path
// wins (the scalar is replaced by the object) — a rare cross-describer
// shape mismatch; documented rather than errored.
func unflattenDesc(flat map[string]any) map[string]any {
	root := map[string]any{}
	for path, v := range flat {
		segs := strings.Split(path, ".")
		cur := root
		for i, s := range segs {
			if i == len(segs)-1 {
				cur[s] = v
				continue
			}
			nxt, ok := cur[s].(map[string]any)
			if !ok {
				nxt = map[string]any{}
				cur[s] = nxt
			}
			cur = nxt
		}
	}
	return root
}

// flatDiffers reports whether two flattened descriptions differ in any
// key or value (value equality via JSON encoding, so nested arrays /
// objects compare structurally).
func flatDiffers(a, b map[string]any) bool {
	if len(a) != len(b) {
		return true
	}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			return true
		}
		ab, _ := json.Marshal(av)
		bb, _ := json.Marshal(bv)
		if !bytes.Equal(ab, bb) {
			return true
		}
	}
	return false
}

// auditTrainingExport writes one admin_audit row per export. Best-effort
// (same contract as the traces export audit) — we don't deny a served
// export over an audit hiccup.
func (h *Handler) auditTrainingExport(r *http.Request, format string, since time.Time, minAgreement float64, emitted, skipped, total int) {
	if h.repo == nil {
		return
	}
	adminID, _ := AdminIDFromContext(r.Context())
	var adminEmail string
	if a, _ := h.repo.FindByID(r.Context(), adminID); a != nil {
		adminEmail = a.Email
	}
	meta := map[string]any{
		"format":  format,
		"emitted": emitted,
		"skipped": skipped,
		"scanned": total,
	}
	if !since.IsZero() {
		meta["since"] = since.UTC().Format(time.RFC3339)
	}
	if minAgreement > 0 {
		meta["minAgreement"] = minAgreement
	}
	Audit(r.Context(), h.repo, h.logger, AuditEntry{
		ID:           generateAuditID(),
		AdminID:      adminID,
		AdminEmail:   adminEmail,
		Action:       "training.export",
		TargetEntity: "training/export",
		Metadata:     meta,
		At:           time.Now().UTC(),
		IP:           clientIP(r),
		UserAgent:    r.Header.Get("User-Agent"),
	})
}
