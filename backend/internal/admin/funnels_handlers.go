package admin

import (
	"context"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/shared/response"
)

// FunnelsRouter handles GET + POST /admin/v1/funnels and
// GET /admin/v1/funnels/{id}/stats (P2-04 / mootd-admin#21).
//
// Permission: traces:read for read; uses prompts:write as a
// proxy for "can configure analyses" since we don't have a
// dedicated funnels:write permission yet (catalog at P5-01
// already big enough). Configure-without-redeploy is the
// admin-tooling intent.
func (h *Handler) FunnelsRouter(w http.ResponseWriter, r *http.Request) {
	if h.funnelsRepo == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "funnels not wired"})
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/admin/v1/funnels")
	rest = strings.TrimPrefix(rest, "/")

	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			h.listFunnels(w, r)
		case http.MethodPost:
			h.createFunnel(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	parts := strings.Split(rest, "/")
	switch len(parts) {
	case 1:
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getFunnel(w, r, parts[0])
	case 2:
		if parts[1] != "stats" {
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-resource"})
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.getFunnelStats(w, r, parts[0])
	default:
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-resource"})
	}
}

func (h *Handler) listFunnels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := h.funnelsRepo.List(ctx)
	if err != nil {
		h.logger.Printf("admin /funnels list: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if rows == nil {
		rows = []Funnel{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"funnels": rows})
}

func (h *Handler) getFunnel(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	f, err := h.funnelsRepo.Get(ctx, id)
	if err != nil {
		h.logger.Printf("admin /funnels/%s: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if f == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "funnel not found"})
		return
	}
	response.WriteJSON(w, http.StatusOK, f)
}

func (h *Handler) createFunnel(w http.ResponseWriter, r *http.Request) {
	if !HasPermissionFromContext(r, PermPromptsWrite) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermPromptsWrite,
		})
		return
	}

	var body struct {
		Name         string       `json:"name"`
		Steps        []FunnelStep `json:"steps"`
		WindowDays   int          `json:"windowDays"`
		AnalysisDays int          `json:"analysisDays"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if body.WindowDays <= 0 {
		body.WindowDays = 7
	}
	if body.AnalysisDays <= 0 {
		body.AnalysisDays = 30
	}
	// Validate step event names against the catalog. We can't
	// reach the events package directly without an import
	// cycle (admin → events → admin via shared/middleware
	// → ...); inline a small allowlist that mirrors the
	// catalog for v1. Drift here is caught by the catalog
	// guard test in #19.
	for _, s := range body.Steps {
		if !isCatalogEvent(s.EventName) {
			response.WriteJSON(w, http.StatusBadRequest, map[string]string{
				"error": "unknown event name in step: " + s.EventName,
			})
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	adminID, _ := AdminIDFromContext(r.Context())
	f := Funnel{
		Name:         strings.TrimSpace(body.Name),
		Steps:        body.Steps,
		WindowDays:   body.WindowDays,
		AnalysisDays: body.AnalysisDays,
		CreatedBy:    adminID,
		CreatedAt:    time.Now().UTC(),
	}
	created, err := h.funnelsRepo.Create(ctx, f)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Audit the mutation (#111 F1) — every other admin write does, and
	// CLAUDE.md requires non-read handlers to write an admin_audit row.
	if h.repo != nil {
		var adminEmail string
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(ctx, h.repo, h.logger, AuditEntry{
			ID:           generateAuditID(),
			AdminID:      adminID,
			AdminEmail:   adminEmail,
			Action:       "funnel.create",
			TargetEntity: created.ID,
			At:           time.Now().UTC(),
			IP:           clientIP(r),
			UserAgent:    r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"name":         created.Name,
				"steps":        len(created.Steps),
				"windowDays":   created.WindowDays,
				"analysisDays": created.AnalysisDays,
			},
		})
	}

	// Echo the persisted row directly — Create returns it with its
	// generated ID, so no name-scan re-list (which returned the wrong
	// row for duplicate names) (#111 F7).
	response.WriteJSON(w, http.StatusCreated, created)
}

func (h *Handler) getFunnelStats(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	stats, err := h.funnelsRepo.Stats(ctx, id)
	if err != nil {
		h.logger.Printf("admin /funnels/%s/stats: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if stats == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "funnel not found"})
		return
	}
	response.WriteJSON(w, http.StatusOK, stats)
}

// isCatalogEvent mirrors the events.CatalogNames allowlist
// without the cross-package import. Same set as
// mootd/backend/internal/events/domain.go (19 events). Kept
// in sync via the catalog guard test in events package.
func isCatalogEvent(name string) bool {
	switch name {
	case "app_opened", "screen_view",
		"session_start", "session_heartbeat", "session_end",
		"photo_uploaded", "items_detected", "item_confirmed", "item_rejected",
		"generate_outfit_requested", "generated_outfit", "viewed_outfit",
		"rated_outfit", "swapped_item",
		"saved_moodboard", "viewed_calendar_date", "shared_moodboard",
		"signed_up", "signed_in", "signed_out":
		return true
	}
	return false
}
