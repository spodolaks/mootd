package outfit

import (
	"context"
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

// SubmitGenerate handles POST /v1/outfits/generate — creates an async generation
// job and returns 202 with the job ID. The actual generation runs in a goroutine.
//
// Idempotency (mootd#42): when the caller supplies an
// `Idempotency-Key` header, a (userId, key) → jobId mapping is
// stored in Redis with a 60s TTL. A second submit with the same
// key inside the window returns the original jobID and does NOT
// start a new job. The RN client is expected to mint a UUID
// per Generate-button press and retain it across retries — that
// way a fat-fingered double-tap or a network retry don't pay
// twice for the same generation.
func (h *Handler) SubmitGenerate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
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
