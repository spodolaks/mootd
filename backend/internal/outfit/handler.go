package outfit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/response"
)

// Handler handles outfit HTTP endpoints. It delegates all business logic to
// the Service and is responsible only for HTTP concerns: parsing requests,
// managing async jobs, and writing responses.
type Handler struct {
	logger    *log.Logger
	service   *Service
	jobStore  *JobStore
	workerCtx context.Context
}

// HandlerConfig wires a Handler.
type HandlerConfig struct {
	Service   *Service
	JobStore  *JobStore
	WorkerCtx context.Context
}

// NewHandler creates an outfit Handler.
func NewHandler(logger *log.Logger, cfg HandlerConfig) *Handler {
	return &Handler{
		logger:    logger,
		service:   cfg.Service,
		jobStore:  cfg.JobStore,
		workerCtx: cfg.WorkerCtx,
	}
}

func parseWeatherParams(r *http.Request) Weather {
	unit := r.URL.Query().Get("unit")
	if unit == "" {
		unit = "C"
	}
	return Weather{
		Temperature: r.URL.Query().Get("temperature"),
		Condition:   r.URL.Query().Get("condition"),
		Unit:        unit,
	}
}

// Generate handles GET /v1/outfits.
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	weather := parseWeatherParams(r)

	outfits, err := h.service.GenerateOutfits(r.Context(), userID, weather)
	if err != nil {
		// Budget gate (P4-02 / mootd-admin#30): map BudgetError to
		// 429 with the reason in the body so the FE can render
		// "Daily $2.00 cap reached at $2.13" rather than a generic
		// "failed to generate outfits."
		var budgetErr *BudgetError
		if errors.As(err, &budgetErr) {
			h.logger.Printf("outfit: budget-denied generate for user %s", userID)
			body := map[string]any{"error": budgetErr.Error()}
			if budgetErr.Reason != nil {
				body["reason"] = budgetErr.Reason
			}
			response.WriteJSON(w, http.StatusTooManyRequests, body)
			return
		}
		h.logger.Printf("outfit: generate for user %s: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate outfits"})
		return
	}

	response.WriteJSON(w, http.StatusOK, GenerateResponse{Outfits: outfits})
}

// SubmitGenerate handles POST /v1/outfits/generate.
//
// Two transport modes (mootd#62):
//
//   - Default JSON path (`Accept: application/json` or absent):
//     creates an async job via Redis, returns 202 + jobId, the
//     generation runs in a goroutine, the client polls
//     /v1/outfits/jobs/{id}. Backwards-compatible with every
//     existing caller.
//
//   - SSE path (`Accept: text/event-stream`): keeps the HTTP
//     connection open through the entire generation and emits
//     `event: progress` messages with the current
//     GenerateProgress shape. Final event is `event: done`. Used
//     by the RN client to render perceived-latency feedback while
//     the LLM call is in flight.
//
// Idempotency (mootd#42): when the caller supplies an
// `Idempotency-Key` header, a (userId, key) → jobId mapping is
// stored in Redis with a 60s TTL. Applied only on the JSON path
// today — SSE streams aren't replayable in a useful way.
func (h *Handler) SubmitGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	// SSE branch (mootd#62). Detect Accept header up-front and
	// dispatch — the rest of this handler stays JSON-only so
	// idempotency + jobStore wiring don't drift across the two
	// transports.
	if wantsSSE(r) {
		h.handleSSEStream(w, r, userID)
		return
	}

	if h.jobStore == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "async generation unavailable (Redis not configured)"})
		return
	}

	// Idempotency check — capped length so a malicious client
	// can't poison Redis keys with megabyte values. UUIDs are
	// 36 chars; 128 leaves headroom for any reasonable scheme
	// without inviting abuse.
	idemKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if len(idemKey) > 128 {
		response.WriteJSONErr(w, http.StatusBadRequest, "Idempotency-Key must be ≤ 128 chars", nil)
		return
	}
	if idemKey != "" {
		if existing, err := h.jobStore.LookupIdempotency(r.Context(), userID, idemKey); err != nil {
			h.logger.Printf("outfit: idempotency lookup for %s/%s: %v (continuing without)", userID, idemKey, err)
		} else if existing != "" {
			// Replay the prior response. 202 again — the FE
			// poll loop is fine receiving the same jobID.
			response.WriteJSON(w, http.StatusAccepted, map[string]string{"jobId": existing})
			return
		}
	}

	weather := parseWeatherParams(r)

	jobID := fmt.Sprintf("%s-%d-%d", userID[:minLen(8, len(userID))], time.Now().UnixMilli(), rand.Intn(10000))
	job := &Job{
		ID:        jobID,
		UserID:    userID,
		Status:    JobPending,
		CreatedAt: time.Now().UTC(),
	}
	if err := h.jobStore.Save(r.Context(), job); err != nil {
		h.logger.Printf("outfit: failed to save job %s: %v", jobID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create job"})
		return
	}

	// Bind the idempotency key to this jobID before launching
	// the goroutine — a retry that lands while we're still in
	// this handler will see the mapping and replay. Failure to
	// save the mapping logs but doesn't fail the request: the
	// generation is durable, idempotency is best-effort safety.
	if idemKey != "" {
		if err := h.jobStore.SaveIdempotency(r.Context(), userID, idemKey, jobID); err != nil {
			h.logger.Printf("outfit: idempotency save for %s/%s → %s: %v (continuing)", userID, idemKey, jobID, err)
		}
	}

	// Launch async generation in a goroutine.
	go h.runAsyncGeneration(jobID, userID, weather)

	response.WriteJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
}

// wantsSSE reports whether the caller asked for SSE via Accept
// header. Only an explicit `text/event-stream` triggers the
// streaming path — `*/*` keeps the JSON default for the
// existing wave of clients that don't care.
func wantsSSE(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}
	for _, part := range strings.Split(accept, ",") {
		mt := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if mt == "text/event-stream" {
			return true
		}
	}
	return false
}

// handleSSEStream drives the synchronous SSE path of
// SubmitGenerate (mootd#62). The caller's Accept header opted
// in; we keep the connection open through the whole LLM call
// and emit one `event: progress` per stage milestone, ending
// with `event: done` (success) or `event: error` (failure).
func (h *Handler) handleSSEStream(w http.ResponseWriter, r *http.Request, userID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Server doesn't support streaming — fall back gracefully
		// so the client at least gets a JSON response instead of
		// a half-open stream.
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unavailable"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable Caddy/nginx buffering
	w.WriteHeader(http.StatusOK)

	// SSE write helper. Errors silently abort — usually means
	// the client closed the connection. Service is still
	// running; it'll log an error and exit at its own context.
	emit := func(event string, payload any) error {
		body, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	weather := parseWeatherParams(r)

	// Generation timeout matches the JSON path. Use the request
	// context as the parent so client disconnects abort the LLM
	// call (the client doesn't need the result anymore).
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	// Bridge the OnProgress callback to SSE wire events.
	onProgress := func(p GenerateProgress) error {
		switch p.Stage {
		case StageDone:
			return emit("done", p)
		case StageError:
			return emit("error", p)
		default:
			return emit("progress", p)
		}
	}

	if _, err := h.service.GenerateOutfitsWithProgress(ctx, userID, weather, onProgress); err != nil {
		// Most error categories already emitted via onProgress.
		// Stage budget-denial through the SSE error event so the
		// client can show a decent message.
		_ = emit("error", GenerateProgress{Stage: StageError, Description: err.Error()})
		h.logger.Printf("outfit: SSE generation failed for user %s: %v", userID, err)
	}
}

// runAsyncGeneration performs the full outfit generation pipeline in the background
// and updates the job in Redis with the result.
func (h *Handler) runAsyncGeneration(jobID, userID string, weather Weather) {
	ctx, cancel := context.WithTimeout(h.workerCtx, 2*time.Minute)
	defer cancel()

	defer func() {
		if r := recover(); r != nil {
			h.logger.Printf("outfit: async job %s — panic: %v", jobID, r)
			failed := &Job{ID: jobID, UserID: userID, Status: JobFailed, Error: "outfit generation failed", CreatedAt: time.Now().UTC()}
			if err := h.jobStore.Save(ctx, failed); err != nil {
				h.logger.Printf("outfit: async job %s — failed to save panic state: %v", jobID, err)
			}
		}
	}()

	// Mark as processing.
	job := &Job{ID: jobID, UserID: userID, Status: JobProcessing, CreatedAt: time.Now().UTC()}
	if err := h.jobStore.Save(ctx, job); err != nil {
		h.logger.Printf("outfit: async job %s — failed to save job state: %v", jobID, err)
	}

	outfits, err := h.service.GenerateOutfits(ctx, userID, weather)
	if err != nil {
		h.logger.Printf("outfit: async job %s — generation failed: %v", jobID, err)
		job.Status = JobFailed
		// Surface budget denial as a user-friendly message in the
		// job state — the polling client renders this directly.
		var budgetErr *BudgetError
		if errors.As(err, &budgetErr) {
			job.Error = budgetErr.Error()
		} else {
			job.Error = "outfit generation failed"
		}
		if err := h.jobStore.Save(ctx, job); err != nil {
			h.logger.Printf("outfit: async job %s — failed to save job state: %v", jobID, err)
		}
		return
	}

	job.Status = JobCompleted
	job.Outfits = outfits
	if err := h.jobStore.Save(ctx, job); err != nil {
		h.logger.Printf("outfit: async job %s — failed to save job state: %v", jobID, err)
	}
	h.logger.Printf("outfit: async job %s completed — %d outfits", jobID, len(outfits))
}

// PollJob handles GET /v1/outfits/jobs/{id} — returns the current state of an
// async outfit generation job. The caller must own the job; mismatches are
// reported as 404 to avoid leaking job existence across users.
func (h *Handler) PollJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.jobStore == nil {
		response.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "async generation unavailable (Redis not configured)"})
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	jobID := strings.TrimPrefix(r.URL.Path, "/v1/outfits/jobs/")
	if jobID == "" {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "missing job ID"})
		return
	}

	job, err := h.jobStore.Get(r.Context(), jobID)
	if err != nil || job.UserID != userID {
		response.WriteJSON(w, http.StatusNotFound, map[string]string{"error": "job not found"})
		return
	}

	response.WriteJSON(w, http.StatusOK, job)
}

// minLen returns the smaller of a and b.
func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
