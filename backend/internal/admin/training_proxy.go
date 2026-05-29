package admin

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/shared/response"
)

// ────────────────────────────────────────────────────────────────────
// Training-trial admin proxy (singleItemDetection #36).
//
// The training UI runs the same garment photo through two describers
// (Claude vs Gemma), then lets an admin pick the winning value per
// attribute — the picks become DPO preference data. Like the HITL
// queue (see hitl_proxy.go), the orchestrator owns the data; mootd
// backend proxies the requests so admin auth, RBAC, and the audit
// log apply uniformly and the orchestrator's service token never
// reaches the browser.
//
// This reuses the already-wired HitlProxy (SINGLEITEM_BASE_URL +
// SINGLEITEM_API_KEY → X-API-Key). The endpoints proxied:
//
//	GET   /admin/v1/training/trials/{trialId}
//	      → orchestrator GET /v1/admin/training/trials/{trialId}
//	GET   /admin/v1/training/blob/{bucket}/{id}
//	      → orchestrator GET /v1/single-item/blob/{bucket}/{id}
//	POST  /admin/v1/training/process
//	      → orchestrator POST /v1/single-item/process
//
// The /admin/v1/training/submissions/* endpoints are NOT proxies —
// they read/write mootd-admin's own training_trials collection (the
// trial list + Submit-review action); see training_trials.go.
//
// The per-attribute "pick" PATCH is NOT here — it lands on the same
// orchestrator endpoint the HITL queue already exposes
// (/admin/v1/items/{id}/attributes), so the FE reuses that.
// ────────────────────────────────────────────────────────────────────

// maxTrainingImageBytes caps the multipart upload forwarded to the
// orchestrator's process endpoint. Garment photos are a few MB; 25MB
// is generous headroom while still bounding memory per request.
const maxTrainingImageBytes = 25 << 20

// TrainingRouter dispatches /admin/v1/training/{sub...} to the right
// handler. Manual path parsing mirrors HitlItemsRouter — the project
// predates Go 1.22 path variables in the admin mux.
func (h *Handler) TrainingRouter(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/admin/v1/training/")
	head, tail := rest, ""
	if idx := strings.Index(rest, "/"); idx >= 0 {
		head, tail = rest[:idx], rest[idx+1:]
	}
	switch head {
	case "process":
		h.TrainingProcess(w, r)
	case "trials":
		h.TrainingTrial(w, r, tail)
	case "blob":
		h.TrainingBlob(w, r, tail)
	case "submissions":
		h.TrainingSubmissionsRouter(w, r, tail)
	default:
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "unknown training sub-resource"})
	}
}

// TrainingTrial handles GET /admin/v1/training/trials/{trialId}.
// Read-only — the page polls this until both describers finish.
// Permission: traces:read (gated at the route).
func (h *Handler) TrainingTrial(w http.ResponseWriter, r *http.Request, trialID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if trialID == "" {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "missing trial id"})
		return
	}
	if !h.hitlReady(w) {
		return
	}
	upstream := fmt.Sprintf("%s/v1/admin/training/trials/%s", h.hitlProxy.BaseURL, urlPathEscape(trialID))
	h.proxyForward(w, r, http.MethodGet, upstream, nil)
}

// TrainingBlob handles GET /admin/v1/training/blob/{bucket}/{id}.
//
// The orchestrator returns gridfs://sid_<bucket>/<id> URIs for the
// source / mask / generation images; the FE resolves them through
// this endpoint. Read-only, gated on traces:read at the route — the
// browser fetches the bytes with the admin bearer token and renders
// them via an object URL (img tags can't carry an Authorization
// header, so this is fetched, not used as a bare <img src>).
func (h *Handler) TrainingBlob(w http.ResponseWriter, r *http.Request, rest string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.hitlReady(w) {
		return
	}
	// rest = "{bucket}/{id}". Both segments are orchestrator-minted
	// ([a-z]+ bucket, 24-hex id); reject anything else so we don't
	// proxy arbitrary upstream paths.
	bucket, id, ok := strings.Cut(rest, "/")
	if !ok || bucket == "" || id == "" || strings.Contains(id, "/") {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "blob path must be {bucket}/{id}"})
		return
	}
	upstream := fmt.Sprintf("%s/v1/single-item/blob/%s/%s", h.hitlProxy.BaseURL, urlPathEscape(bucket), urlPathEscape(id))
	h.proxyForward(w, r, http.MethodGet, upstream, nil)
}

// TrainingProcess handles POST /admin/v1/training/process.
//
// Forwards the multipart garment photo (plus the X-Trial-Id /
// X-Describer / X-Request-Id routing headers) to the orchestrator's
// SSE process endpoint. The orchestrator streams progress events
// until the pipeline finishes (10-60s+); the training UI never reads
// those events — it polls the trial endpoint instead. So we wait only
// for the orchestrator to ACCEPT the job (response headers), return
// 202 to the admin immediately, and drain the SSE stream in the
// background on a detached context so the pipeline runs to completion
// even after the admin's request returns.
//
// Mutating + expensive → gated inline on traces:rerun (same as the
// HITL approve/regenerate actions). Audited.
func (h *Handler) TrainingProcess(w http.ResponseWriter, r *http.Request) {
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

	// Read the multipart body up front so we can (a) cap it and (b)
	// replay it on a detached context. Bounded read — see cap above.
	r.Body = http.MaxBytesReader(w, r.Body, maxTrainingImageBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("image read failed (max %dMB)", maxTrainingImageBytes>>20),
		})
		return
	}

	// Detached context: the orchestrator's pipeline must outlive this
	// HTTP request. r.Context() is cancelled the moment we return 202,
	// which would kill the upstream stream mid-pipeline.
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)

	upstream := h.hitlProxy.BaseURL + "/v1/single-item/process"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream, bytes.NewReader(body))
	if err != nil {
		cancel()
		h.logger.Printf("admin training proxy: build request: %v", err)
		response.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream request build failed"})
		return
	}
	// multipart Content-Type (carries the boundary) + the orchestrator's
	// trial-routing headers must pass through verbatim.
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	for _, hdr := range []string{"X-Trial-Id", "X-Describer", "X-Request-Id"} {
		if v := r.Header.Get(hdr); v != "" {
			req.Header.Set(hdr, v)
		}
	}
	if h.hitlProxy.APIKey != "" {
		req.Header.Set("X-API-Key", h.hitlProxy.APIKey)
	}
	if adminID, ok := AdminIDFromContext(r.Context()); ok {
		req.Header.Set("X-Mootd-Admin-Id", adminID)
	}

	// No client timeout — ctx bounds the whole exchange. Do() returns
	// once the orchestrator has sent response headers (job accepted),
	// before the SSE body is consumed.
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		cancel()
		h.logger.Printf("admin training proxy: %s failed: %v", upstream, err)
		response.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "orchestrator unreachable"})
		return
	}

	// Non-2xx: the orchestrator rejected the job. Surface its status +
	// body to the admin (so a 401/422 is visible) and stop here.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		resp.Body.Close()
		cancel()
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(errBody)
		return
	}

	// Job accepted. Drain the SSE stream in the background so the
	// orchestrator pipeline runs to completion, then return 202.
	go func() {
		defer cancel()
		defer resp.Body.Close()
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			h.logger.Printf("admin training proxy: drain %s: %v", upstream, err)
		}
	}()

	requestID := r.Header.Get("X-Request-Id")
	response.WriteJSON(w, http.StatusAccepted, map[string]string{
		"status":    "accepted",
		"requestId": requestID,
	})
	h.auditHitlAction(r, "training.process", requestID, nil)
}
