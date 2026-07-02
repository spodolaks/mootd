package admin

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mootd/backend/internal/shared/response"
)

// promptNameRe constrains template names: lowercase snake_case with
// optional dot-separated suffixes — the per-archetype convention
// "outfit_system_base.creator" (mootd#65). Names embed into the
// "pt_<name>_v<N>" document id and the /admin/v1/prompts/{name}
// route, so anything looser invites escaping bugs. Existing seeded
// names all match.
var promptNameRe = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_-]+)*$`)

// itoa is a tiny shorthand. fmt.Sprintf("%d") is the
// alternative; this keeps the call site readable.
func itoa(n int) string { return strconv.Itoa(n) }

// ────────────────────────────────────────────────────────────────────
// Prompt-template HTTP handlers (P3-01 / mootd-admin#24).
//
// Five endpoints, all behind admin auth:
//
//   GET    /admin/v1/prompts                            — list names
//   GET    /admin/v1/prompts/{name}                     — list versions of one template
//   GET    /admin/v1/prompts/{name}/{version}           — read one version
//   POST   /admin/v1/prompts/{name}                     — create a new version (requires prompts:write)
//   POST   /admin/v1/prompts/{name}/{version}/promote   — flip isProduction (requires prompts:write)
//
// The dispatcher is `PromptTemplatesRouter` — single mux entry,
// per-method dispatch inside.
// ────────────────────────────────────────────────────────────────────

// PromptTemplatesRouter is the prefix dispatcher.
func (h *Handler) PromptTemplatesRouter(w http.ResponseWriter, r *http.Request) {
	if h.templatesRepo == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "prompt templates not wired"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/admin/v1/prompts")
	rest = strings.TrimPrefix(rest, "/")

	switch {
	case rest == "":
		// /admin/v1/prompts → list names (GET only)
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.listPromptNames(w, r)

	default:
		// /admin/v1/prompts/{name}[/{version}[/promote]]
		parts := strings.Split(rest, "/")
		name := parts[0]
		switch len(parts) {
		case 1:
			switch r.Method {
			case http.MethodGet:
				h.listPromptVersions(w, r, name)
			case http.MethodPost:
				h.createPromptVersion(w, r, name)
			default:
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			}
		case 2:
			if parts[1] == "ab-tests" {
				switch r.Method {
				case http.MethodGet:
					h.listABTests(w, r, name)
				case http.MethodPost:
					h.startABTest(w, r, name)
				default:
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				}
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.getPromptVersion(w, r, name, parts[1])
		case 3:
			if parts[1] == "ab-tests" && parts[2] == "end" {
				if r.Method != http.MethodPost {
					http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
					return
				}
				h.endABTest(w, r, name)
				return
			}
			if parts[2] != "promote" {
				response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-resource"})
				return
			}
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			h.promotePromptVersion(w, r, name, parts[1])
		default:
			response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown sub-resource"})
		}
	}
}

func (h *Handler) listPromptNames(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	names, err := h.templatesRepo.ListNames(ctx)
	if err != nil {
		h.logger.Printf("admin /prompts: list names: %v", err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if names == nil {
		names = []string{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"names": names})
}

func (h *Handler) listPromptVersions(w http.ResponseWriter, r *http.Request, name string) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := h.templatesRepo.ListVersions(ctx, name)
	if err != nil {
		h.logger.Printf("admin /prompts/%s: list: %v", name, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if rows == nil {
		rows = []PromptTemplate{}
	}
	response.WriteJSON(w, http.StatusOK, map[string]any{"versions": rows})
}

func (h *Handler) getPromptVersion(w http.ResponseWriter, r *http.Request, name, versionStr string) {
	id := "pt_" + name + "_v" + versionStr
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	doc, err := h.templatesRepo.Get(ctx, id)
	if err != nil {
		h.logger.Printf("admin /prompts/%s/%s: %v", name, versionStr, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if doc == nil {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "version not found"})
		return
	}
	response.WriteJSON(w, http.StatusOK, doc)
}

func (h *Handler) createPromptVersion(w http.ResponseWriter, r *http.Request, name string) {
	if !HasPermissionFromContext(r, PermPromptsWrite) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermPromptsWrite,
		})
		return
	}
	var body struct {
		Body  string `json:"body"`
		Notes string `json:"notes"`
	}
	if err := response.DecodeJSONBody(w, r, &body); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "body is required"})
		return
	}
	notes := strings.TrimSpace(body.Notes)
	if notes == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "notes is required (audit log rationale)"})
		return
	}
	// New names are created implicitly by the first POST (that's how
	// per-archetype variants are curated from the UI), so validate
	// the shape here rather than 404ing unknown names.
	if len(name) > 80 || !promptNameRe.MatchString(name) {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid template name: lowercase letters, digits and underscores, optional dot-separated suffix (e.g. outfit_system_base.creator), max 80 chars"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Determine next version = max(existing) + 1.
	existing, err := h.templatesRepo.ListVersions(ctx, name)
	if err != nil {
		h.logger.Printf("admin /prompts/%s POST: list: %v", name, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	nextVersion := 1
	for _, v := range existing {
		if v.Version >= nextVersion {
			nextVersion = v.Version + 1
		}
	}

	adminID, _ := AdminIDFromContext(r.Context())
	t := PromptTemplate{
		ID:           "pt_" + name + "_v" + itoa(nextVersion),
		Name:         name,
		Version:      nextVersion,
		Body:         body.Body,
		Variables:    discoverVariables(body.Body),
		IsProduction: false, // new versions don't auto-promote — separate explicit step
		CreatedBy:    adminID,
		CreatedAt:    time.Now().UTC(),
		Notes:        notes,
	}
	if err := h.templatesRepo.CreateVersion(ctx, t); err != nil {
		h.logger.Printf("admin /prompts/%s POST: create: %v", name, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
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
			Action:     "prompt.create",
			At:         time.Now().UTC(),
			IP:         clientIP(r),
			UserAgent:  r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"name":    name,
				"version": nextVersion,
				"notes":   notes,
				"chars":   len(body.Body),
			},
		})
	}

	response.WriteJSON(w, http.StatusCreated, t)
}

func (h *Handler) promotePromptVersion(w http.ResponseWriter, r *http.Request, name, versionStr string) {
	if !HasPermissionFromContext(r, PermPromptsWrite) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermPromptsWrite,
		})
		return
	}
	var version int
	for _, c := range versionStr {
		if c < '0' || c > '9' {
			response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "version must be numeric"})
			return
		}
		version = version*10 + int(c-'0')
	}
	if version == 0 {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "version > 0 required"})
		return
	}

	var body struct {
		Notes string `json:"notes"`
	}
	_ = response.DecodeJSONBody(w, r, &body) // notes optional on promote
	notes := strings.TrimSpace(body.Notes)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.templatesRepo.Promote(ctx, name, version); err != nil {
		// "version not found" → 400; everything else 500.
		if strings.Contains(err.Error(), "not found") {
			response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		h.logger.Printf("admin /prompts/%s/%d/promote: %v", name, version, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	// Invalidate the cache so the next outfit-gen call sees
	// the new production template immediately.
	if h.templatesCache != nil {
		h.templatesCache.Invalidate()
	}

	// Audit.
	if h.repo != nil {
		adminID, _ := AdminIDFromContext(r.Context())
		var adminEmail string
		if a, _ := h.repo.FindByID(ctx, adminID); a != nil {
			adminEmail = a.Email
		}
		Audit(ctx, h.repo, h.logger, AuditEntry{
			ID:         generateAuditID(),
			AdminID:    adminID,
			AdminEmail: adminEmail,
			Action:     "prompt.promote",
			At:         time.Now().UTC(),
			IP:         clientIP(r),
			UserAgent:  r.Header.Get("User-Agent"),
			Metadata: map[string]any{
				"name":    name,
				"version": version,
				"notes":   notes,
			},
		})
	}

	// Echo the now-current production version so the FE can
	// reflect immediately without a follow-up GET.
	doc, _ := h.templatesRepo.GetProduction(ctx, name)
	response.WriteJSON(w, http.StatusOK, doc)
}
