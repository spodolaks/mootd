package feedback

import (
	"context"
	"log"
	"net/http"
	"time"

	"mootd/backend/internal/shared/id"
	"mootd/backend/internal/shared/middleware"
	"mootd/backend/internal/shared/response"
)

// Handler handles feedback endpoints.
type Handler struct {
	logger *log.Logger
	repo   Repository
}

// NewHandler creates a feedback Handler.
func NewHandler(logger *log.Logger, repo Repository) *Handler {
	return &Handler{logger: logger, repo: repo}
}

// Submit handles POST /v1/outfits/feedback.
//
// Accepts a SubmitRequest and appends it to the outfit_feedback collection.
// UserID is taken from the JWT, never the body, so a client can't forge
// events for another account. Action must be one of the known constants;
// unknown actions are rejected so the collection stays typed.
//
// Responses:
//
//	201 Created — { id }
//	400 — { error: "…" } on validation failure
//	401 — { error: "unauthorized" } when the JWT is missing or invalid
//	500 — { error: "failed to record feedback" } on persistence failure
func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		response.WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req SubmitRequest
	if err := response.DecodeJSONBody(w, r, &req); err != nil {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !req.Action.Valid() {
		response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid or missing action"})
		return
	}
	if req.Action == ActionRated {
		if req.Rating == nil || *req.Rating < 1 || *req.Rating > 5 {
			response.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "rating must be 1–5 for action=rated"})
			return
		}
	}

	event := Event{
		ID:               id.Generate(),
		UserID:           userID,
		JobID:            req.JobID,
		ChosenOutfitID:   req.ChosenOutfitID,
		Action:           req.Action,
		Rating:           req.Rating,
		GeneratedBatch:   req.GeneratedBatch,
		Context:          req.Context,
		PromptVersion:    req.PromptVersion,
		GeneratorVersion: req.GeneratorVersion,
		SchemaVersion:    CurrentSchemaVersion,
		CreatedAt:        time.Now().UTC(),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := h.repo.Insert(ctx, event); err != nil {
		h.logger.Printf("feedback: insert failed for user %s action %s: %v", userID, req.Action, err)
		response.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to record feedback"})
		return
	}

	response.WriteJSON(w, http.StatusCreated, SubmitResponse{ID: event.ID})
}
