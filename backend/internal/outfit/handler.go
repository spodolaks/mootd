package outfit

import (
	"context"
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
		h.logger.Printf("outfit: generate for user %s: %v", userID, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate outfits"})
		return
	}

	response.WriteJSON(w, http.StatusOK, GenerateResponse{Outfits: outfits})
}

// SubmitGenerate handles POST /v1/outfits/generate — creates an async generation
// job and returns 202 with the job ID. The actual generation runs in a goroutine.
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
		job.Error = "outfit generation failed"
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
