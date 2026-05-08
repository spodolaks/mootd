package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"mootd/backend/internal/shared/response"
)

// WithArchetypeDefaults wires the curated-defaults repo so the
// admin endpoints become available. Optional — when nil, the
// /archetype-defaults surface returns 503.
func (h *Handler) WithArchetypeDefaults(repo ArchetypeDefaultsRepository) *Handler {
	h.archetypeDefaults = repo
	return h
}

// ArchetypeDefaultsRouter dispatches requests against
// /admin/v1/archetype-defaults[/{id}].
//
// Permissions:
//   - GET (list / detail) → prompts:read (anyone curating
//     content is allowed to read).
//   - POST / PATCH / DELETE → prompts:write (curating defaults
//     is content authoring; we reuse the prompts:write gate
//     rather than minting a new permission for one feature).
func (h *Handler) ArchetypeDefaultsRouter(w http.ResponseWriter, r *http.Request) {
	if h.archetypeDefaults == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "archetype defaults not wired"})
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/admin/v1/archetype-defaults")
	rest = strings.TrimPrefix(rest, "/")

	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			h.listArchetypeDefaults(w, r)
		case http.MethodPost:
			h.createArchetypeDefault(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	// Sub-paths come before id-based dispatch. "detect" is the
	// upload-and-autodescribe endpoint — keep adding new ones here
	// as the surface grows.
	if rest == "detect" {
		h.detectArchetypeDefault(w, r)
		return
	}

	id := rest
	switch r.Method {
	case http.MethodGet:
		h.getArchetypeDefault(w, r, id)
	case http.MethodPatch:
		h.updateArchetypeDefault(w, r, id)
	case http.MethodDelete:
		h.deleteArchetypeDefault(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) listArchetypeDefaults(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	arche := r.URL.Query().Get("archetype")
	rows, err := h.archetypeDefaults.List(ctx, arche)
	if err != nil {
		h.logger.Printf("admin /archetype-defaults list (archetype=%q): %v", arche, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if rows == nil {
		rows = []ArchetypeDefaultItem{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"items": rows})
}

func (h *Handler) getArchetypeDefault(w http.ResponseWriter, r *http.Request, id string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	item, err := h.archetypeDefaults.Get(ctx, id)
	if err != nil {
		h.logger.Printf("admin /archetype-defaults/%s: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if item == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	response.WriteJSON(w, http.StatusOK, item)
}

func (h *Handler) createArchetypeDefault(w http.ResponseWriter, r *http.Request) {
	if !HasPermissionFromContext(r, PermPromptsWrite) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermPromptsWrite,
		})
		return
	}

	var body ArchetypeDefaultItem
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	adminID, _ := AdminIDFromContext(r.Context())
	body.CreatedBy = adminID

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	created, err := h.archetypeDefaults.Create(ctx, body)
	if err != nil {
		// Friendlier mapping for known classes of error.
		msg := err.Error()
		switch {
		case strings.Contains(msg, "unknown archetype"),
			strings.Contains(msg, "requires category"):
			response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
		case mongo.IsDuplicateKeyError(err):
			response.WriteJSON(w, http.StatusConflict, map[string]string{
				"error": "an archetype default already exists with this archetype + category + label",
			})
		default:
			h.logger.Printf("admin /archetype-defaults create: %v", err)
			response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		}
		return
	}

	if h.repo != nil {
		var adminEmail string
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(ctx, h.repo, h.logger, AuditEntry{
			ID:           generateAuditID(),
			AdminID:      adminID,
			AdminEmail:   adminEmail,
			Action:       "archetype_default.create",
			TargetEntity: "archetype_default_items/" + created.ID,
			Metadata: map[string]any{
				"archetype": created.Archetype,
				"category":  created.Category,
				"label":     created.Label,
			},
			At:        time.Now().UTC(),
			IP:        clientIP(r),
			UserAgent: r.Header.Get("User-Agent"),
		})
	}

	response.WriteJSON(w, http.StatusCreated, created)
}

func (h *Handler) updateArchetypeDefault(w http.ResponseWriter, r *http.Request, id string) {
	if !HasPermissionFromContext(r, PermPromptsWrite) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermPromptsWrite,
		})
		return
	}

	var patch ArchetypeDefaultItemPatch
	if err := response.DecodeJSONBody(w, r, &patch); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	updated, err := h.archetypeDefaults.Update(ctx, id, patch)
	if errors.Is(err, mongo.ErrNoDocuments) {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		h.logger.Printf("admin /archetype-defaults/%s patch: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	response.WriteJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteArchetypeDefault(w http.ResponseWriter, r *http.Request, id string) {
	if !HasPermissionFromContext(r, PermPromptsWrite) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermPromptsWrite,
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	err := h.archetypeDefaults.Delete(ctx, id)
	if errors.Is(err, mongo.ErrNoDocuments) {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		h.logger.Printf("admin /archetype-defaults/%s delete: %v", id, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	if h.repo != nil {
		adminID, _ := AdminIDFromContext(r.Context())
		var adminEmail string
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(ctx, h.repo, h.logger, AuditEntry{
			ID:           generateAuditID(),
			AdminID:      adminID,
			AdminEmail:   adminEmail,
			Action:       "archetype_default.delete",
			TargetEntity: "archetype_default_items/" + id,
			At:           time.Now().UTC(),
			IP:           clientIP(r),
			UserAgent:    r.Header.Get("User-Agent"),
		})
	}

	w.WriteHeader(http.StatusNoContent)
}
