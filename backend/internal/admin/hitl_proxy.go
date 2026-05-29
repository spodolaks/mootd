package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/shared/response"
)

// ────────────────────────────────────────────────────────────────────
// HITL queue admin proxy (singleItemDetection #34, #35).
//
// The singleItemDetection orchestrator owns the SingleItemDetectionItem
// rows + their HITL state. Admins read + mutate them through mootd
// backend so the existing admin auth, audit log, and IP allowlist
// apply uniformly. mootd backend forwards each request body to the
// orchestrator and streams the response back; the orchestrator's
// response shape is canonical (defined in admin-api.yaml schemas).
//
// Why proxy instead of letting the admin frontend call the
// orchestrator directly:
//
//   - One auth model. The orchestrator authenticates via a static
//     service token; admin auth goes through mootd's JWT + RBAC +
//     MFA flow. Joining them at the FE would mean a CORS surface
//     on the orchestrator and duplicated auth logic.
//   - Audit log on every mutation lands in mootd's admin_audit
//     collection alongside every other admin action — single
//     source of truth for "who did what".
//   - Permission gating in one place — admin#34's RBAC tables.
// ────────────────────────────────────────────────────────────────────

// HitlProxy carries the orchestrator base URL + service token
// the proxy handlers use. nil means HITL is disabled (the admin
// endpoints return 503 — same shape as other optional admin deps).
type HitlProxy struct {
	BaseURL  string // e.g. http://orchestrator:8080
	APIKey   string // service-to-service token; sent as X-API-Key
	Client   *http.Client
}

// NewHitlProxy constructs a HitlProxy. baseURL = "" → returns nil so
// app.go's wiring is uniform (`if proxy != nil` everywhere).
func NewHitlProxy(baseURL, apiKey string) *HitlProxy {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	return &HitlProxy{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		// 30s for read endpoints (queue list + detail); the
		// regenerate POST is the slowest case but still well
		// under this since it's an enqueue, not a sync run.
		Client: &http.Client{Timeout: 30 * time.Second},
	}
}

// WithHitlProxy wires the orchestrator client (singleItemDetection
// #34, #35). Optional — when nil, the /admin/v1/hitl-queue +
// /admin/v1/items/{id}/* endpoints return 503.
func (h *Handler) WithHitlProxy(p *HitlProxy) *Handler {
	h.hitlProxy = p
	return h
}

// WithTrainingTrials wires the training-review record store
// (singleItemDetection #36, multi-item). Optional — when nil, the
// /admin/v1/training/submissions/* endpoints return 503 while the
// orchestrator-proxied /training/{process,trials,blob} keep working.
func (h *Handler) WithTrainingTrials(r TrainingTrialsRepository) *Handler {
	h.trainingTrials = r
	return h
}

// HitlQueue handles GET /admin/v1/hitl-queue.
//
// Query params are forwarded verbatim — the orchestrator owns the
// filter set (status / category / hitl_reason / hitl_only /
// confidence_min / confidence_max / sort / cursor / limit). Adding a
// new filter on the orchestrator side doesn't require a mootd-side
// change; the schema gate is the OpenAPI spec the FE generator reads.
//
// Permission: traces:read.
func (h *Handler) HitlQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.hitlReady(w) {
		return
	}
	upstream := h.hitlProxy.BaseURL + "/v1/admin/hitl-queue"
	if q := r.URL.RawQuery; q != "" {
		upstream += "?" + q
	}
	h.proxyForward(w, r, http.MethodGet, upstream, nil)
}

// HitlItem handles GET /admin/v1/items/{id}.
//
// {id} is the orchestrator's item id; it's a server-generated
// opaque string. We don't crack it open — pass through.
func (h *Handler) HitlItem(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.hitlReady(w) {
		return
	}
	upstream := fmt.Sprintf("%s/v1/admin/items/%s", h.hitlProxy.BaseURL, urlPathEscape(id))
	h.proxyForward(w, r, http.MethodGet, upstream, nil)
}

// HitlApprove handles POST /admin/v1/items/{id}/approve.
//
// Mutating action — gated on traces:rerun (same shape as the
// detection-run replay surface). Audited.
func (h *Handler) HitlApprove(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !HasPermissionFromContext(r, PermTracesRerun) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermTracesRerun,
		})
		return
	}
	if !h.hitlReady(w) {
		return
	}
	body, err := readBody(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	upstream := fmt.Sprintf("%s/v1/admin/items/%s/approve", h.hitlProxy.BaseURL, urlPathEscape(id))
	h.proxyForward(w, r, http.MethodPost, upstream, body)
	h.auditHitlAction(r, "hitl.approve", id, body)
}

// HitlReject handles POST /admin/v1/items/{id}/reject.
func (h *Handler) HitlReject(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !HasPermissionFromContext(r, PermTracesRerun) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermTracesRerun,
		})
		return
	}
	if !h.hitlReady(w) {
		return
	}
	body, err := readBody(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	upstream := fmt.Sprintf("%s/v1/admin/items/%s/reject", h.hitlProxy.BaseURL, urlPathEscape(id))
	h.proxyForward(w, r, http.MethodPost, upstream, body)
	h.auditHitlAction(r, "hitl.reject", id, body)
}

// HitlPatchAttributes handles PATCH /admin/v1/items/{id}/attributes.
func (h *Handler) HitlPatchAttributes(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPatch {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !HasPermissionFromContext(r, PermTracesRerun) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermTracesRerun,
		})
		return
	}
	if !h.hitlReady(w) {
		return
	}
	body, err := readBody(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Training ingest (#126): when the training store is wired, snapshot
	// the pre-edit structuredDescription BEFORE forwarding the patch, so
	// we can record (rejected = original, chosen = original+patches) once
	// the patch succeeds. Entirely best-effort — any failure here leaves
	// the patch itself untouched.
	var snap *hitlItemSnapshot
	var patches map[string]any
	if h.trainingTrials != nil {
		var pb struct {
			Patches map[string]any `json:"patches"`
		}
		if json.Unmarshal(body, &pb) == nil && len(pb.Patches) > 0 {
			patches = pb.Patches
			if s, gErr := h.fetchHitlItemSnapshot(r.Context(), id); gErr == nil {
				snap = s
			} else {
				h.logger.Printf("admin training: hitl pre-snapshot %s: %v", id, gErr)
			}
		}
	}

	upstream := fmt.Sprintf("%s/v1/admin/items/%s/attributes", h.hitlProxy.BaseURL, urlPathEscape(id))
	status := h.proxyForward(w, r, http.MethodPatch, upstream, body)
	h.auditHitlAction(r, "hitl.patch_attributes", id, body)

	if snap != nil && status >= 200 && status < 300 {
		adminID, _ := AdminIDFromContext(r.Context())
		h.ingestHitlCorrection(r.Context(), id, snap, patches, adminID)
	}
}

// HitlRegenerate handles POST /admin/v1/items/{id}/regenerate.
func (h *Handler) HitlRegenerate(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !HasPermissionFromContext(r, PermTracesRerun) {
		response.WriteJSON(w, http.StatusForbidden, map[string]any{
			"error":             "permission denied",
			"missingPermission": PermTracesRerun,
		})
		return
	}
	if !h.hitlReady(w) {
		return
	}
	body, err := readBody(r)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	upstream := fmt.Sprintf("%s/v1/admin/items/%s/regenerate", h.hitlProxy.BaseURL, urlPathEscape(id))
	h.proxyForward(w, r, http.MethodPost, upstream, body)
	h.auditHitlAction(r, "hitl.regenerate", id, body)
}

// hitlReady returns true when the proxy is wired. When false it
// writes 503 with a stable error shape and the caller should
// abort.
func (h *Handler) hitlReady(w http.ResponseWriter) bool {
	if h.hitlProxy == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "HITL queue not wired (set DETECTION_BACKEND=singleitem and SINGLEITEM_BASE_URL)",
		})
		return false
	}
	return true
}

// proxyForward streams the request to the upstream URL and writes
// the response back verbatim. Body is the pre-read request body
// (nil for GETs). Errors at the transport layer surface as 502;
// orchestrator-level 4xx/5xx pass through.
// Returns the HTTP status written to the client (the orchestrator's
// status on success, or 502 on a transport/build failure) so callers
// that need to act only on a successful upstream call — e.g. HITL
// training ingest (#126) — can gate on it.
func (h *Handler) proxyForward(w http.ResponseWriter, r *http.Request, method, upstream string, body []byte) int {
	ctx, cancel := context.WithTimeout(r.Context(), h.hitlProxy.Client.Timeout)
	defer cancel()

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, upstream, bodyReader)
	if err != nil {
		h.logger.Printf("admin hitl proxy: build request %s %s: %v", method, upstream, err)
		response.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream request build failed"})
		return http.StatusBadGateway
	}
	if body != nil && r.Header.Get("Content-Type") != "" {
		req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	}
	if h.hitlProxy.APIKey != "" {
		req.Header.Set("X-API-Key", h.hitlProxy.APIKey)
	}
	// Forward the calling admin's identity so the orchestrator
	// can stamp it on its own audit rows. mootd's audit_log
	// captures the same; this lets the orchestrator's per-row
	// "lockedBy" field show the actual admin email rather than
	// a service-account placeholder.
	if adminID, ok := AdminIDFromContext(r.Context()); ok {
		req.Header.Set("X-Mootd-Admin-Id", adminID)
	}

	resp, err := h.hitlProxy.Client.Do(req)
	if err != nil {
		h.logger.Printf("admin hitl proxy: %s %s failed: %v", method, upstream, err)
		response.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "orchestrator unreachable"})
		return http.StatusBadGateway
	}
	defer resp.Body.Close()

	// Copy status + content-type + body. Other headers are
	// dropped — the orchestrator might emit framework-internal
	// things (server, x-debug-foo) that we don't want leaking
	// out through the admin surface.
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		h.logger.Printf("admin hitl proxy: copy response %s: %v", upstream, err)
	}
	return resp.StatusCode
}

// readBody pulls the request body into a byte slice (capped) so
// proxyForward can replay it. Cap is generous — HITL patches
// might carry several attribute paths but never approach
// megabytes.
func readBody(r *http.Request) ([]byte, error) {
	const maxBody = 256 * 1024
	if r.Body == nil {
		return nil, nil
	}
	limited := http.MaxBytesReader(nil, r.Body, maxBody)
	defer limited.Close()
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, errors.New("body read failed (max 256KB)")
	}
	return body, nil
}

// auditHitlAction writes one admin_audit row per mutation. Best-
// effort: failure logs but the proxy response is unchanged.
func (h *Handler) auditHitlAction(r *http.Request, action, itemID string, body []byte) {
	if h.repo == nil {
		return
	}
	adminID, _ := AdminIDFromContext(r.Context())
	var adminEmail string
	if a, _ := h.repo.FindByID(r.Context(), adminID); a != nil {
		adminEmail = a.Email
	}
	// Body is recorded raw (capped above) so the audit row
	// captures exactly what was forwarded. JSON parse failures
	// don't matter — the field is for human review.
	Audit(r.Context(), h.repo, h.logger, AuditEntry{
		ID:           generateAuditID(),
		AdminID:      adminID,
		AdminEmail:   adminEmail,
		Action:       action,
		TargetEntity: "items/" + itemID,
		Metadata: map[string]any{
			"itemId":  itemID,
			"bodyRaw": string(body),
		},
		At:        time.Now().UTC(),
		IP:        clientIP(r),
		UserAgent: r.Header.Get("User-Agent"),
	})
}

// HitlItemsRouter dispatches /admin/v1/items/{id}[/sub] requests
// to the right HITL handler based on the sub-resource. Until Go
// 1.22 path variables, this dispatch lives in the handler.
func (h *Handler) HitlItemsRouter(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/admin/v1/items/")
	if rest == "" {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "missing item id"})
		return
	}
	id, sub := rest, ""
	if idx := strings.Index(rest, "/"); idx > 0 {
		id, sub = rest[:idx], rest[idx+1:]
	}
	switch sub {
	case "":
		h.HitlItem(w, r, id)
	case "approve":
		h.HitlApprove(w, r, id)
	case "reject":
		h.HitlReject(w, r, id)
	case "attributes":
		h.HitlPatchAttributes(w, r, id)
	case "regenerate":
		h.HitlRegenerate(w, r, id)
	default:
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown item sub-resource"})
	}
}

// urlPathEscape is a tiny helper so the handlers don't pull in
// net/url for one call. Item IDs are server-minted and don't
// contain unsafe characters today, but escaping is cheap
// insurance against a future ID format change.
func urlPathEscape(s string) string {
	// Reuse net/http's standard encoding via Request.URL.Path
	// would require an extra request build. The orchestrator
	// generates IDs from `[a-zA-Z0-9_-]` so a no-op replace is
	// safe here; if that ever changes, switch to url.PathEscape.
	return strings.NewReplacer(
		"/", "%2F",
		"?", "%3F",
		"#", "%23",
		" ", "%20",
	).Replace(s)
}
