package admin

import (
	"context"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/shared/response"
)

// ────────────────────────────────────────────────────────────────────
// A/B test HTTP handlers (P3-05 / mootd-admin#28).
//
// Three endpoints, all gated on prompts:write:
//
//   GET  /admin/v1/prompts/{name}/ab-tests       — list (active + history)
//   POST /admin/v1/prompts/{name}/ab-tests       — start (one active per name)
//   POST /admin/v1/prompts/{name}/ab-tests/end   — end the active test
//
// The route patterns are dispatched from PromptTemplatesRouter.
// ────────────────────────────────────────────────────────────────────

func (h *Handler) listABTests(w http.ResponseWriter, r *http.Request, name string) {
	if h.abTestsRepo == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "A/B testing not wired"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := h.abTestsRepo.List(ctx, name)
	if err != nil {
		h.logger.Printf("admin /prompts/%s/ab-tests GET: %v", name, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if rows == nil {
		rows = []ABTest{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"tests": rows})
}

func (h *Handler) startABTest(w http.ResponseWriter, r *http.Request, name string) {
	if !HasPermissionFromContext(r, PermPromptsWrite) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermPromptsWrite,
		})
		return
	}
	if h.abTestsRepo == nil || h.templatesRepo == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "A/B testing not wired"})
		return
	}

	var body struct {
		CandidateVersion int    `json:"candidateVersion"`
		TrafficPct       int    `json:"trafficPct"`
		Notes            string `json:"notes"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.CandidateVersion <= 0 {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "candidateVersion > 0 required"})
		return
	}
	if body.TrafficPct < 1 || body.TrafficPct > 99 {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "trafficPct must be 1-99"})
		return
	}
	notes := strings.TrimSpace(body.Notes)
	if notes == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "notes is required (audit log rationale)"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Validate: candidate version must exist + must NOT be the
	// current production version (running an A/B against the
	// same body twice is a footgun we'd rather catch up front).
	candidate, err := h.templatesRepo.Get(ctx, "pt_"+name+"_v"+itoa(body.CandidateVersion))
	if err != nil {
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if candidate == nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "candidateVersion not found"})
		return
	}
	if candidate.IsProduction {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "candidateVersion is the current production version; pick a different one"})
		return
	}
	prod, err := h.templatesRepo.GetProduction(ctx, name)
	if err != nil || prod == nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "no production version on this template; promote one first"})
		return
	}

	adminID, _ := AdminIDFromContext(r.Context())
	test := ABTest{
		TemplateName:      name,
		ProductionVersion: prod.Version,
		CandidateVersion:  body.CandidateVersion,
		TrafficPct:        body.TrafficPct,
		StartedBy:         adminID,
		StartedAt:         time.Now().UTC(),
		Notes:             notes,
	}
	if err := h.abTestsRepo.Start(ctx, test); err != nil {
		// "already active on this template" → 409.
		if strings.Contains(err.Error(), "already active") {
			response.WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		h.logger.Printf("admin /prompts/%s/ab-tests POST: %v", name, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if h.abTestsCache != nil {
		h.abTestsCache.Invalidate()
	}

	// Audit.
	if h.repo != nil {
		var adminEmail string
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(ctx, h.repo, h.logger, AuditEntry{
			ID:         generateAuditID(),
			AdminID:    adminID,
			AdminEmail: adminEmail,
			Action:     "prompt.ab.start",
			At:         time.Now().UTC(),
			IP:         clientIP(r),
			UserAgent:  r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"templateName":      name,
				"productionVersion": prod.Version,
				"candidateVersion":  body.CandidateVersion,
				"trafficPct":        body.TrafficPct,
				"notes":             notes,
			},
		})
	}

	// Re-fetch to echo the persisted (with assigned id + status).
	echo, _ := h.abTestsRepo.Active(ctx, name)
	response.WriteJSON(w, http.StatusCreated, echo)
}

func (h *Handler) endABTest(w http.ResponseWriter, r *http.Request, name string) {
	if !HasPermissionFromContext(r, PermPromptsWrite) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermPromptsWrite,
		})
		return
	}
	if h.abTestsRepo == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "A/B testing not wired"})
		return
	}

	var body struct {
		Notes string `json:"notes"`
	}
	_ = response.DecodeJSONBody(w, r, &body)
	notes := strings.TrimSpace(body.Notes)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	active, err := h.abTestsRepo.Active(ctx, name)
	if err != nil {
		h.logger.Printf("admin /prompts/%s/ab-tests/end: %v", name, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if active == nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "no active test on this template"})
		return
	}

	adminID, _ := AdminIDFromContext(r.Context())
	if err := h.abTestsRepo.End(ctx, active.ID, adminID, notes); err != nil {
		h.logger.Printf("admin /prompts/%s/ab-tests/end: %v", name, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if h.abTestsCache != nil {
		h.abTestsCache.Invalidate()
	}

	if h.repo != nil {
		var adminEmail string
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(ctx, h.repo, h.logger, AuditEntry{
			ID:         generateAuditID(),
			AdminID:    adminID,
			AdminEmail: adminEmail,
			Action:     "prompt.ab.end",
			At:         time.Now().UTC(),
			IP:         clientIP(r),
			UserAgent:  r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"templateName": name,
				"testId":       active.ID,
				"notes":        notes,
			},
		})
	}

	w.WriteHeader(http.StatusNoContent)
}
